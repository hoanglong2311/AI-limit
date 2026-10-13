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
const AddKeyHint = "Chưa có API key DeepSeek: nhập key trong Cài đặt"

// keychainGet reads the item; a variable so tests can check the lookup.
var keychainGet = keychain.Get

// KeychainAccount is the account of the Keychain item.
const KeychainAccount = "default"

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
	return keychainGet(KeychainService, KeychainAccount)
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
		return provider.Usage{}, &HTTPError{Status: resp.StatusCode}
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

// SaveKey stores the key in the Keychain (native API, never a command line).
func SaveKey(key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return errors.New("key trống")
	}
	return keychain.Set(KeychainService, KeychainAccount, key)
}

// Check result texts shown in Settings; they never include the key.
const (
	CheckOK    = "Key hợp lệ"
	CheckBad   = "Key sai/không dùng được"
	CheckRetry = "Không kiểm tra được, thử lại sau"
)

// HTTPError is a non-200 response from the API. It holds only the status
// code, never the key or the request.
type HTTPError struct{ Status int }

func (e *HTTPError) Error() string { return fmt.Sprintf("deepseek: HTTP %d", e.Status) }

// Rejected reports whether the server refused the key (401 or 403).
func (e *HTTPError) Rejected() bool {
	return e.Status == http.StatusUnauthorized || e.Status == http.StatusForbidden
}

// CheckText maps the result of a balance call to the Settings text: only
// 401/403 mean the key is bad; network errors, 5xx and anything else
// mean the check could not be done.
func CheckText(err error) string {
	if err == nil {
		return CheckOK
	}
	var he *HTTPError
	if errors.As(err, &he) && he.Rejected() {
		return CheckBad
	}
	return CheckRetry
}
