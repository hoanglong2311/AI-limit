// Package claude reports Claude subscription usage for one account.
package claude

import (
	"context"

	"github.com/hoanglong2311/AI-limit/internal/keychain"
	"github.com/hoanglong2311/AI-limit/internal/provider"
)

// KeychainService is the item Claude Code stores its OAuth credentials under.
const KeychainService = "Claude Code-credentials"

// Provider reads the token READ-ONLY. It never refreshes the token, because
// a refresh would rotate it and log out the Claude Code client that owns it.
type Provider struct{ Email string }

func (p *Provider) ID() string      { return "claude:" + p.Email }
func (p *Provider) Service() string { return "Claude" }
func (p *Provider) Account() string { return p.Email }

func (p *Provider) Fetch(ctx context.Context) (provider.Usage, error) {
	if _, err := keychain.Read(ctx, KeychainService, ""); err != nil {
		return provider.Usage{}, err
	}
	// TODO: parse the JSON credential (accessToken, expiresAt). If expired, return an
	// error ("token hết hạn") instead of refreshing. Otherwise call the usage endpoint
	// and map the 5h / weekly windows to Usage{Kind: KindQuota, UsedPercent, ResetAt}.
	// Verify the account email matches p.Email.
	return provider.Usage{}, provider.NotImplemented("Claude")
}
