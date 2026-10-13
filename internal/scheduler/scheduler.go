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
	StatusPending Status = iota // never fetched successfully, no error yet
	StatusOK                    // last fetch succeeded and is fresh
	StatusError                 // last fetch failed
	StatusStale                 // last success is older than StaleAfter
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
	if errors.Is(err, provider.ErrStale) {
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
				s.RefreshOne(ctx, p)
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
		if st.Status == StatusOK && now.Sub(st.LastSuccess) > s.cfg.StaleAfter {
			st.Status = StatusStale
		}
		out = append(out, st)
	}
	return out
}
