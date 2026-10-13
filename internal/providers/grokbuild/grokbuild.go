// Package grokbuild is the Grok row. There is no usage data source yet,
// so it reports provider.ErrNoSource and never shows numbers.
package grokbuild

import (
	"context"

	"github.com/hoanglong2311/AI-limit/internal/provider"
)

type Provider struct{ Label string }

func (p *Provider) ID() string      { return "grok:" + p.Label }
func (p *Provider) Service() string { return "Grok" }
func (p *Provider) Account() string { return p.Label }

func (p *Provider) Fetch(ctx context.Context) (provider.Usage, error) {
	// TODO: find a Grok usage endpoint; never guess numbers.
	return provider.Usage{}, provider.ErrNoSource
}
