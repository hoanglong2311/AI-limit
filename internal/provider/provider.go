// Package provider defines the interface every usage source implements.
package provider

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Kind tells which fields of Usage are meaningful.
type Kind int

const (
	// KindQuota: UsedPercent and ResetAt are set.
	KindQuota Kind = iota
	// KindBalance: Balance and Currency are set.
	KindBalance
)

// Usage is one snapshot from a provider.
type Usage struct {
	Kind        Kind
	UsedPercent float64   // 0..100, KindQuota
	ResetAt     time.Time // zero if unknown, KindQuota
	Balance     float64   // KindBalance, amount in Currency
	Currency    string    // KindBalance, ISO code such as "USD" or "CNY"
}

// Provider fetches usage for one account.
type Provider interface {
	// ID is unique and stable, e.g. "claude:five_hour".
	ID() string
	// Service is the display name, e.g. "Claude".
	Service() string
	// Account is the display label, e.g. an email.
	Account() string
	// Fetch must honor ctx cancellation. It may be called from any goroutine.
	Fetch(ctx context.Context) (Usage, error)
}

// ErrStale marks a failure where no fresh data can be fetched yet (for
// example a missing or expired token). Wrap it; the row then shows
// "chưa cập nhật" instead of "lỗi".
var ErrStale = errors.New("chưa cập nhật")

// Stale wraps a reason as an ErrStale error.
func Stale(reason string) error { return fmt.Errorf("%s: %w", reason, ErrStale) }

// ErrNoKey marks a provider whose API key is not configured. The row
// shows "chưa có key" and the error text (one line of instructions) as a
// hint, distinct from "lỗi".
var ErrNoKey = errors.New("chưa có key")

// NoKey wraps a one-line instruction as an ErrNoKey error.
func NoKey(hint string) error { return noKey{hint} }

type noKey struct{ hint string }

func (e noKey) Error() string        { return e.hint }
func (e noKey) Is(target error) bool { return target == ErrNoKey }

// notImpl is returned by stub providers.
type notImpl struct{ name string }

func (e notImpl) Error() string { return e.name + ": chưa triển khai" }

// NotImplemented returns an error for a stub provider.
func NotImplemented(name string) error { return notImpl{name} }

// ErrReset marks a quota window whose reset time has passed with no newer
// data: the row shows "đã reset, chưa dùng" instead of "chưa cập nhật".
var ErrReset = errors.New("đã reset, chưa dùng")

// Reset wraps a reason as an ErrReset error. It also matches ErrStale:
// the old numbers are not current either way.
func Reset(reason string) error { return resetErr{reason} }

type resetErr struct{ reason string }

func (e resetErr) Error() string { return e.reason + ": " + ErrReset.Error() }
func (e resetErr) Is(target error) bool {
	return target == ErrReset || target == ErrStale
}

// ErrNoSource marks a service with no data source yet (no numbers are guessed).
var ErrNoSource = errors.New("chưa có nguồn số liệu")
