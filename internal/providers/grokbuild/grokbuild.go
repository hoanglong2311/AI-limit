// Package grokbuild reports Grok Build usage limits.
package grokbuild

import (
	"context"

	"github.com/hoanglong2311/AI-limit/internal/provider"
)

type Provider struct{ Label string }

func (p *Provider) ID() string      { return "grokbuild:" + p.Label }
func (p *Provider) Service() string { return "Grok Build" }
func (p *Provider) Account() string { return p.Label }

func (p *Provider) Fetch(ctx context.Context) (provider.Usage, error) {
	// TODO: find the Grok Build usage endpoint, keep credentials in Keychain
	// (service "ai-limit.grokbuild"), map to KindQuota (used %, resetAt).
	return provider.Usage{}, provider.NotImplemented("Grok Build")
}
