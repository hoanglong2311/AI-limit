// Package scheduler runs each provider in isolation and tracks row state.
package scheduler

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/hoanglong2311/AI-limit/internal/provider"
)

// Status of a row.
type Status int

const (
	StatusPending  Status = iota // never fetched successfully, no error yet
	StatusOK                     // last fetch succeeded and is fresh
	StatusError                  // last fetch failed
	StatusStale                  // last success is older than StaleAfter
	StatusNoKey                  // not configured; Err holds one line of instructions
	StatusReset                  // window reset with no newer data
	StatusNoSource               // service has no data source yet
)

// HighThreshold is the used % above which a row is highlighted.
const HighThreshold = 80.0

// State is a snapshot of one provider's row.
type State struct {
	ID, Service, Account string
	Status               Status
	Usage                provider.Usage // valid only if HasUsage
	HasUsage             bool
	LastSuccess          time.Time
	LastAttempt          time.Time
	Err                  string
}

// Fresh reports whether the numbers may be shown as current.
func (s State) Fresh() bool { return s.Status == StatusOK && s.HasUsage }

// High reports whether the row should be highlighted.
func (s State) High() bool {
	return s.Fresh() && s.Usage.Kind == provider.KindQuota && s.Usage.UsedPercent > HighThreshold
}

// Label is the short status text shown instead of numbers when not fresh.
func (s State) Label() string {
	switch s.Status {
	case StatusError:
		return "lỗi"
	case StatusNoKey:
		return "chưa có key"
	case StatusReset:
		return "đã reset, chưa dùng"
	case StatusNoSource:
		return "chưa có nguồn số liệu"
	case StatusPending, StatusStale:
		return "chưa cập nhật"
	}
	return ""
}

// Config for the scheduler.
type Config struct {
	Interval   time.Duration // default refresh interval
	Timeout    time.Duration // per fetch
	StaleAfter time.Duration // success older than this is stale; default 3*Interval
	// Skip, when set, reports providers that must not be fetched
	// (disabled in Settings).
	Skip func(id string) bool
	// PerProvider overrides Interval by provider ID.
	PerProvider map[string]time.Duration
	Now         func() time.Time
}

func (c *Config) defaults() {
	if c.Interval <= 0 {
		c.Interval = 5 * time.Minute
	}
	if c.Timeout <= 0 {
		c.Timeout = 30 * time.Second
	}
	if c.StaleAfter <= 0 {
		c.StaleAfter = 3 * c.Interval
	}
	if c.Now == nil {
		c.Now = time.Now
	}
}

// Scheduler owns the providers and their states.
type Scheduler struct {
	cfg       Config
	providers []provider.Provider
	mu        sync.Mutex
	states    map[string]*State
	onChange  func()
}

// New creates a scheduler. onChange is called (from any goroutine) after a state changes.
func New(cfg Config, ps []provider.Provider, onChange func()) *Scheduler {
	cfg.defaults()
	s := &Scheduler{cfg: cfg, providers: ps, states: map[string]*State{}, onChange: onChange}
	for _, p := range ps {
		s.states[p.ID()] = &State{ID: p.ID(), Service: p.Service(), Account: p.Account()}
	}
	return s
}

// IntervalFor returns the refresh interval for a provider.
func (s *Scheduler) IntervalFor(id string) time.Duration {
	if d, ok := s.cfg.PerProvider[id]; ok && d > 0 {
		return d
	}
	return s.cfg.Interval
}

// RefreshOne fetches one provider with timeout and panic recovery, then
// updates its state. Safe to call concurrently for different providers.
func (s *Scheduler) RefreshOne(ctx context.Context, p provider.Provider) {
	u, err := s.safeFetch(ctx, p)
	now := s.cfg.Now()
	s.mu.Lock()
	st := s.states[p.ID()]
	st.LastAttempt = now
	st.Account = p.Account() // may be detected only after a fetch
	if errors.Is(err, provider.ErrNoSource) {
		st.Status = StatusNoSource
		st.Err = ""
	} else if errors.Is(err, provider.ErrReset) {
		st.Status = StatusReset
		st.Err = err.Error()
	} else if errors.Is(err, provider.ErrNoKey) {
		st.Status = StatusNoKey
		st.Err = err.Error()
	} else if errors.Is(err, provider.ErrStale) {
		st.Status = StatusStale
		st.Err = err.Error()
	} else if err != nil {
		st.Status = StatusError
		st.Err = err.Error()
	} else {
		st.Status = StatusOK
		st.Err = ""
		st.Usage = u
		st.HasUsage = true
		st.LastSuccess = now
	}
	s.mu.Unlock()
	s.notify()
}

func (s *Scheduler) safeFetch(ctx context.Context, p provider.Provider) (u provider.Usage, err error) {
	ctx, cancel := context.WithTimeout(ctx, s.cfg.Timeout)
	defer cancel()
	type res struct {
		u   provider.Usage
		err error
	}
	ch := make(chan res, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				ch <- res{err: fmt.Errorf("panic: %v", r)}
			}
		}()
		u, err := p.Fetch(ctx)
		ch <- res{u, err}
	}()
	select {
	case r := <-ch:
		return r.u, r.err
	case <-ctx.Done():
		return provider.Usage{}, fmt.Errorf("timeout: %w", ctx.Err())
	}
}

func (s *Scheduler) notify() {
	if s.onChange != nil {
		s.onChange()
	}
}

// Run starts one goroutine per provider; returns when ctx is done.
func (s *Scheduler) Run(ctx context.Context) {
	var wg sync.WaitGroup
	for _, p := range s.providers {
		wg.Add(1)
		go func(p provider.Provider) {
			defer wg.Done()
			t := time.NewTicker(s.IntervalFor(p.ID()))
			defer t.Stop()
			for {
				if s.cfg.Skip == nil || !s.cfg.Skip(p.ID()) {
					s.RefreshOne(ctx, p)
				}
				select {
				case <-ctx.Done():
					return
				case <-t.C:
				}
			}
		}(p)
	}
	// Staleness watcher: re-evaluate periodically so old numbers turn stale.
	wg.Add(1)
	go func() {
		defer wg.Done()
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				s.notify()
			}
		}
	}()
	wg.Wait()
}

// Snapshot returns states in provider order, with staleness applied.
func (s *Scheduler) Snapshot() []State {
	now := s.cfg.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]State, 0, len(s.providers))
	for _, p := range s.providers {
		st := *s.states[p.ID()]
		st.Status = Effective(st, now, s.cfg.StaleAfter)
		out = append(out, st)
	}
	return out
}

// Effective is the status to display at now. Numbers whose window reset
// time has passed show "đã reset, chưa dùng" (there is no newer data);
// otherwise numbers older than staleAfter turn stale.
func Effective(st State, now time.Time, staleAfter time.Duration) Status {
	if st.Status != StatusOK {
		return st.Status
	}
	if st.HasUsage && st.Usage.Kind == provider.KindQuota &&
		!st.Usage.ResetAt.IsZero() && !now.Before(st.Usage.ResetAt) {
		return StatusReset
	}
	if now.Sub(st.LastSuccess) > staleAfter {
		return StatusStale
	}
	return StatusOK
}

// Providers returns the providers in order.
func (s *Scheduler) Providers() []provider.Provider { return s.providers }

// RefreshAll fetches the given providers concurrently and returns when
// all are done.
func (s *Scheduler) RefreshAll(ctx context.Context, ps []provider.Provider) {
	var wg sync.WaitGroup
	for _, p := range ps {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.RefreshOne(ctx, p)
		}()
	}
	wg.Wait()
}
