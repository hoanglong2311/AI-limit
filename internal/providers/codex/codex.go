// Package codex reports OpenAI Codex usage limits.
package codex

import (
	"context"

	"github.com/hoanglong2311/AI-limit/internal/provider"
)

type Provider struct{ Label string }

func (p *Provider) ID() string      { return "codex:" + p.Label }
func (p *Provider) Service() string { return "Codex" }
func (p *Provider) Account() string { return p.Label }

func (p *Provider) Fetch(ctx context.Context) (provider.Usage, error) {
	// TODO: determine the source of Codex rate-limit data (CLI session/status output
	// or an API), store any credential in Keychain (service "ai-limit.codex"), map to KindQuota.
	return provider.Usage{}, provider.NotImplemented("Codex")
}
