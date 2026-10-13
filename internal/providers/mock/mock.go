// Package mock is a demo provider with random-ish data and occasional failures.
package mock

import (
	"context"
	"errors"
	"math/rand/v2"
	"time"

	"github.com/hoanglong2311/AI-limit/internal/provider"
)

// Provider is a demo provider.
type Provider struct {
	Name, Label string
	Balance     bool    // report a USD balance instead of a quota
	FailRate    float64 // 0..1 chance of returning an error
}

func (p *Provider) ID() string      { return "mock:" + p.Name + ":" + p.Label }
func (p *Provider) Service() string { return p.Name }
func (p *Provider) Account() string { return p.Label }

func (p *Provider) Fetch(ctx context.Context) (provider.Usage, error) {
	select {
	case <-ctx.Done():
		return provider.Usage{}, ctx.Err()
	case <-time.After(time.Duration(200+rand.IntN(800)) * time.Millisecond):
	}
	if rand.Float64() < p.FailRate {
		return provider.Usage{}, errors.New("mock: lỗi giả lập")
	}
	if p.Balance {
		return provider.Usage{Kind: provider.KindBalance, Balance: 5 + rand.Float64()*50, Currency: "USD"}, nil
	}
	return provider.Usage{
		Kind:        provider.KindQuota,
		UsedPercent: rand.Float64() * 100,
		ResetAt:     time.Now().Add(time.Duration(1+rand.IntN(5)) * time.Hour).Truncate(time.Minute),
	}, nil
}
