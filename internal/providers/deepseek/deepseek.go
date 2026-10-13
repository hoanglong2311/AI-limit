// Package deepseek reports the DeepSeek account balance in USD.
package deepseek

import (
	"context"

	"github.com/hoanglong2311/AI-limit/internal/keychain"
	"github.com/hoanglong2311/AI-limit/internal/provider"
)

// KeychainService is the Keychain item holding the API key.
const KeychainService = "ai-limit.deepseek"

type Provider struct{ Label string }

func (p *Provider) ID() string      { return "deepseek:" + p.Label }
func (p *Provider) Service() string { return "DeepSeek" }
func (p *Provider) Account() string { return p.Label }

func (p *Provider) Fetch(ctx context.Context) (provider.Usage, error) {
	if _, err := keychain.Read(ctx, KeychainService, ""); err != nil {
		return provider.Usage{}, err
	}
	// TODO: GET https://api.deepseek.com/user/balance with "Authorization: Bearer <key>",
	// parse balance_infos[currency=USD].total_balance into Usage{Kind: KindBalance}.
	return provider.Usage{}, provider.NotImplemented("DeepSeek")
}
