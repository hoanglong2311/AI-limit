package update

import (
	"context"
	"time"
)

// CheckInterval is the time between periodic update checks.
const CheckInterval = 3 * time.Hour

// Poller runs update checks one at a time: once at start, every Interval,
// and on Trigger (the "Làm mới" button). Checks never overlap; triggers
// that arrive while a check is running or pending collapse into one.
type Poller struct {
	Check    func(ctx context.Context)
	Interval time.Duration // default CheckInterval
	kick     chan struct{}
}

// NewPoller returns a poller for check.
func NewPoller(check func(ctx context.Context)) *Poller {
	return &Poller{Check: check, kick: make(chan struct{}, 1)}
}

// Trigger asks for a check now without blocking; extra calls while one is
// already pending are dropped.
func (p *Poller) Trigger() {
	select {
	case p.kick <- struct{}{}:
	default:
	}
}

// Run checks until ctx is done.
func (p *Poller) Run(ctx context.Context) {
	d := p.Interval
	if d <= 0 {
		d = CheckInterval
	}
	t := time.NewTicker(d)
	defer t.Stop()
	for {
		p.Check(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-p.kick:
		}
		// A tick and a kick together still mean one check.
		select {
		case <-p.kick:
		default:
		}
	}
}
