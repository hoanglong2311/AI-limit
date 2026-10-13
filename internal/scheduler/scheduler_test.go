package scheduler

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hoanglong2311/AI-limit/internal/provider"
)

type fake struct {
	id string
	fn func(ctx context.Context) (provider.Usage, error)
	n  atomic.Int32
}

func (f *fake) ID() string      { return f.id }
func (f *fake) Service() string { return "Fake" }
func (f *fake) Account() string { return f.id }
func (f *fake) Fetch(ctx context.Context) (provider.Usage, error) {
	f.n.Add(1)
	return f.fn(ctx)
}

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) Now() time.Time      { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) Add(d time.Duration) { c.mu.Lock(); c.t = c.t.Add(d); c.mu.Unlock() }

func ok(p float64) func(context.Context) (provider.Usage, error) {
	return func(context.Context) (provider.Usage, error) {
		return provider.Usage{Kind: provider.KindQuota, UsedPercent: p}, nil
	}
}

func TestPendingBeforeFetch(t *testing.T) {
	s := New(Config{}, []provider.Provider{&fake{id: "a", fn: ok(10)}}, nil)
	st := s.Snapshot()[0]
	if st.Status != StatusPending || st.Fresh() || st.Label() != "chưa cập nhật" {
		t.Fatalf("got %+v", st)
	}
}

func TestErrorKeepsLastSuccessButNotFresh(t *testing.T) {
	ck := &clock{t: time.Unix(1000, 0)}
	f := &fake{id: "a", fn: ok(90)}
	s := New(Config{Now: ck.Now}, []provider.Provider{f}, nil)
	s.RefreshOne(context.Background(), f)
	st := s.Snapshot()[0]
	if !st.Fresh() || !st.High() {
		t.Fatalf("want fresh+high, got %+v", st)
	}
	first := st.LastSuccess
	ck.Add(time.Minute)
	f.fn = func(context.Context) (provider.Usage, error) { return provider.Usage{}, errors.New("boom") }
	s.RefreshOne(context.Background(), f)
	st = s.Snapshot()[0]
	if st.Status != StatusError || st.Fresh() || st.High() || st.Label() != "lỗi" {
		t.Fatalf("want error, got %+v", st)
	}
	if !st.LastSuccess.Equal(first) || st.Err != "boom" {
		t.Fatalf("last success lost: %+v", st)
	}
}

func TestStaleAfter(t *testing.T) {
	ck := &clock{t: time.Unix(1000, 0)}
	f := &fake{id: "a", fn: ok(10)}
	s := New(Config{Now: ck.Now, Interval: time.Minute}, []provider.Provider{f}, nil)
	s.RefreshOne(context.Background(), f)
	ck.Add(2 * time.Minute)
	if s.Snapshot()[0].Status != StatusOK {
		t.Fatal("should still be ok")
	}
	ck.Add(2 * time.Minute) // 4m > 3*1m
	st := s.Snapshot()[0]
	if st.Status != StatusStale || st.Fresh() || st.Label() != "chưa cập nhật" {
		t.Fatalf("want stale, got %+v", st)
	}
}

func TestPanicAndTimeoutIsolated(t *testing.T) {
	good := &fake{id: "good", fn: ok(50)}
	pan := &fake{id: "panic", fn: func(context.Context) (provider.Usage, error) { panic("x") }}
	slow := &fake{id: "slow", fn: func(ctx context.Context) (provider.Usage, error) {
		time.Sleep(time.Second) // ignores ctx on purpose
		return provider.Usage{}, nil
	}}
	s := New(Config{Timeout: 50 * time.Millisecond}, []provider.Provider{good, pan, slow}, nil)
	var wg sync.WaitGroup
	for _, p := range []provider.Provider{good, pan, slow} {
		wg.Add(1)
		go func() { defer wg.Done(); s.RefreshOne(context.Background(), p) }()
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("slow provider blocked others")
	}
	sn := s.Snapshot()
	if !sn[0].Fresh() {
		t.Errorf("good: %+v", sn[0])
	}
	if sn[1].Status != StatusError {
		t.Errorf("panic: %+v", sn[1])
	}
	if sn[2].Status != StatusError {
		t.Errorf("slow: %+v", sn[2])
	}
}

func TestRunRefreshesPeriodicallyPerProvider(t *testing.T) {
	fast := &fake{id: "fast", fn: ok(1)}
	slow := &fake{id: "slow", fn: ok(1)}
	s := New(Config{Interval: time.Hour, PerProvider: map[string]time.Duration{"fast": 20 * time.Millisecond}},
		[]provider.Provider{fast, slow}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	s.Run(ctx)
	if fast.n.Load() < 3 {
		t.Errorf("fast refreshed %d times", fast.n.Load())
	}
	if slow.n.Load() != 1 {
		t.Errorf("slow refreshed %d times", slow.n.Load())
	}
}

func TestBalanceNeverHigh(t *testing.T) {
	f := &fake{id: "a", fn: func(context.Context) (provider.Usage, error) {
		return provider.Usage{Kind: provider.KindBalance, Balance: 999}, nil
	}}
	s := New(Config{}, []provider.Provider{f}, nil)
	s.RefreshOne(context.Background(), f)
	if s.Snapshot()[0].High() {
		t.Fatal("balance should not be high")
	}
}

func TestStaleErrorShowsChuaCapNhat(t *testing.T) {
	f := &fake{id: "a", fn: ok(10)}
	s := New(Config{}, []provider.Provider{f}, nil)
	s.RefreshOne(context.Background(), f)
	f.fn = func(context.Context) (provider.Usage, error) {
		return provider.Usage{}, provider.Stale("token hết hạn")
	}
	s.RefreshOne(context.Background(), f)
	st := s.Snapshot()[0]
	if st.Status != StatusStale || st.Fresh() || st.Label() != "chưa cập nhật" || st.LastSuccess.IsZero() {
		t.Fatalf("got %+v", st)
	}
}

func TestNoKeyStatus(t *testing.T) {
	p := &fake{id: "k", fn: func(context.Context) (provider.Usage, error) { return provider.Usage{}, provider.NoKey("thêm key") }}
	s := New(Config{}, []provider.Provider{p}, nil)
	s.RefreshOne(context.Background(), p)
	st := s.Snapshot()[0]
	if st.Status != StatusNoKey || st.Label() != "chưa có key" || st.Err != "thêm key" {
		t.Fatalf("%+v", st)
	}
}
