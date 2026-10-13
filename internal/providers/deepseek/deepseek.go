// Package deepseek reports the DeepSeek account balance.
package deepseek

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/hoanglong2311/AI-limit/internal/keychain"
	"github.com/hoanglong2311/AI-limit/internal/provider"
)

// KeychainService is the Keychain item holding the API key.
const KeychainService = "ai-limit.deepseek"

// DefaultBaseURL is the official API.
const DefaultBaseURL = "https://api.deepseek.com"

// AddKeyHint is the one-line instruction shown when the key is missing.
const AddKeyHint = "Thêm key vào Keychain: mục ai-limit.deepseek"

// Provider calls GET /user/balance. The key is read from Keychain on every
// fetch and is never written anywhere.
type Provider struct {
	Label   string
	BaseURL string                                    // default DefaultBaseURL
	Client  *http.Client                              // default http.DefaultClient
	Key     func(ctx context.Context) (string, error) // default: Keychain
}

func (p *Provider) ID() string      { return "deepseek:" + p.Label }
func (p *Provider) Service() string { return "DeepSeek" }
func (p *Provider) Account() string { return p.Label }

type balanceResponse struct {
	IsAvailable  bool `json:"is_available"`
	BalanceInfos []struct {
		Currency     string `json:"currency"`
		TotalBalance string `json:"total_balance"`
	} `json:"balance_infos"`
}

func (p *Provider) key(ctx context.Context) (string, error) {
	if p.Key != nil {
		return p.Key(ctx)
	}
	return keychain.Read(ctx, KeychainService, "")
}

func (p *Provider) Fetch(ctx context.Context) (provider.Usage, error) {
	key, err := p.key(ctx)
	if err != nil || strings.TrimSpace(key) == "" {
		if ctx.Err() != nil {
			return provider.Usage{}, ctx.Err()
		}
		return provider.Usage{}, provider.NoKey(AddKeyHint)
	}
	base := p.BaseURL
	if base == "" {
		base = DefaultBaseURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+"/user/balance", nil)
	if err != nil {
		return provider.Usage{}, err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(key))
	req.Header.Set("Accept", "application/json")
	c := p.Client
	if c == nil {
		c = http.DefaultClient
	}
	resp, err := c.Do(req)
	if err != nil {
		return provider.Usage{}, fmt.Errorf("deepseek: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return provider.Usage{}, fmt.Errorf("deepseek: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return provider.Usage{}, fmt.Errorf("deepseek: HTTP %d", resp.StatusCode)
	}
	return parse(body)
}

// parse picks USD if present, otherwise the first currency reported.
func parse(body []byte) (provider.Usage, error) {
	var r balanceResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return provider.Usage{}, fmt.Errorf("deepseek: JSON không hợp lệ: %w", err)
	}
	if len(r.BalanceInfos) == 0 {
		return provider.Usage{}, errors.New("deepseek: không có balance_infos")
	}
	pick := 0
	for i, b := range r.BalanceInfos {
		if strings.EqualFold(b.Currency, "USD") {
			pick = i
			break
		}
	}
	b := r.BalanceInfos[pick]
	if b.Currency == "" {
		return provider.Usage{}, errors.New("deepseek: thiếu currency")
	}
	v, err := strconv.ParseFloat(b.TotalBalance, 64)
	if err != nil {
		return provider.Usage{}, fmt.Errorf("deepseek: total_balance không hợp lệ %q", b.TotalBalance)
	}
	return provider.Usage{Kind: provider.KindBalance, Balance: v, Currency: strings.ToUpper(b.Currency)}, nil
}
