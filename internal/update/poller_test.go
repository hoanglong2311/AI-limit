package update

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestPollerDefaultInterval(t *testing.T) {
	if CheckInterval != 3*time.Hour {
		t.Fatal(CheckInterval)
	}
}

func TestPollerStartupPeriodicAndTrigger(t *testing.T) {
	var calls, running, overlap atomic.Int32
	release := make(chan struct{}, 10)
	p := NewPoller(func(ctx context.Context) {
		if running.Add(1) > 1 {
			overlap.Add(1)
		}
		calls.Add(1)
		select {
		case <-release:
		case <-ctx.Done():
		}
		running.Add(-1)
	})
	p.Interval = time.Hour // no tick during the test
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { p.Run(ctx); close(done) }()
	wait := func(n int32) {
		t.Helper()
		for i := 0; i < 200 && calls.Load() < n; i++ {
			time.Sleep(5 * time.Millisecond)
		}
		if calls.Load() != n {
			t.Fatalf("calls %d want %d", calls.Load(), n)
		}
	}
	wait(1) // startup check, still running
	// Many presses during a running check: no overlap, one follow-up.
	for range 5 {
		p.Trigger()
	}
	release <- struct{}{}
	wait(2)
	release <- struct{}{}
	time.Sleep(30 * time.Millisecond)
	if calls.Load() != 2 {
		t.Fatalf("triggers stacked: %d", calls.Load())
	}
	// A later press checks again immediately.
	p.Trigger()
	wait(3)
	release <- struct{}{}
	cancel()
	<-done
	if overlap.Load() != 0 {
		t.Fatal("concurrent checks")
	}
}

func TestPollerPeriodic(t *testing.T) {
	var calls atomic.Int32
	p := NewPoller(func(context.Context) { calls.Add(1) })
	p.Interval = 20 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), 110*time.Millisecond)
	defer cancel()
	p.Run(ctx)
	if n := calls.Load(); n < 3 {
		t.Fatalf("periodic calls %d", n)
	}
}
