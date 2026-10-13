// Package claude reports Claude subscription usage (5-hour and weekly
// windows) for one account, using the OAuth credential Claude Code already
// stores in the macOS Keychain.
//
// The credential is read-only: this package never refreshes, rotates or
// writes the token. A refresh would rotate it and log out Claude Code.
// When the token is missing or expired the rows show "chưa cập nhật" until
// Claude Code itself refreshes it.
//
// Endpoint (undocumented, used by Claude Code's /usage):
//
//	GET https://api.anthropic.com/api/oauth/usage
//	Authorization: Bearer <claudeAiOauth.accessToken>
//	anthropic-beta: oauth-2025-04-20
//
// Response: {"five_hour":{"utilization":33.0,"resets_at":"<RFC3339>"},
// "seven_day":{...}, ...}; utilization is a percentage 0..100.
package claude

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/hoanglong2311/AI-limit/internal/keychain"
	"github.com/hoanglong2311/AI-limit/internal/provider"
)

const (
	// KeychainService is the generic-password item Claude Code writes on macOS.
	KeychainService = "Claude Code-credentials"
	DefaultBaseURL  = "https://api.anthropic.com"
	BetaHeader      = "oauth-2025-04-20"
	// MinInterval is the shortest time between two HTTP calls; the endpoint
	// rate-limits aggressively.
	MinInterval = 5 * time.Minute
	userAgent   = "ai-limit/0.1"
)

// SetupHint is the one-line instruction shown when no credential exists.
const SetupHint = "Chưa đăng nhập Claude Code: chạy `claude` và đăng nhập một lần"

// NextAllowed is the earliest time a new HTTP call may be made; zero if
// no call was made yet.
func (c *Client) NextAllowed() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.last.IsZero() {
		return time.Time{}
	}
	min := c.MinInterval
	if min <= 0 {
		min = MinInterval
	}
	return c.last.Add(min)
}

// CooldownLeft is how long until a manual refresh may call the API again
// (0 when allowed now). It never returns a negative duration.
func CooldownLeft(next, now time.Time) time.Duration {
	if next.IsZero() || !now.Before(next) {
		return 0
	}
	return next.Sub(now)
}

// Window selects which usage window a row shows.
type Window string

const (
	FiveHour Window = "five_hour"
	SevenDay Window = "seven_day"
)

// credential is the JSON stored in the Keychain item.
type credential struct {
	ClaudeAiOauth struct {
		AccessToken string `json:"accessToken"`
		ExpiresAt   int64  `json:"expiresAt"` // Unix milliseconds
	} `json:"claudeAiOauth"`
}

type window struct {
	Utilization *float64 `json:"utilization"`
	ResetsAt    *string  `json:"resets_at"`
}

type usageResponse struct {
	FiveHour *window `json:"five_hour"`
	SevenDay *window `json:"seven_day"`
}

// Client fetches the usage once and shares it between the rows, at most
// once per MinInterval.
type Client struct {
	// Email overrides the detected account email. Placeholder addresses
	// such as you@example.com are ignored.
	Email   string
	BaseURL string
	HTTP    *http.Client
	// LocalEmail returns the email Claude Code stored locally
	// (~/.claude.json oauthAccount.emailAddress); default reads that file.
	LocalEmail func() string
	// Credential returns the raw Keychain JSON; default reads the Keychain.
	Credential  func(ctx context.Context) (string, error)
	Now         func() time.Time
	MinInterval time.Duration

	mu      sync.Mutex
	last    time.Time
	lastRes usageResponse
	lastErr error
	email   string // detected account email
	profTry bool   // profile endpoint already tried
}

// placeholder reports example addresses that must never be shown.
func placeholder(e string) bool {
	e = strings.ToLower(strings.TrimSpace(e))
	return e == "" || strings.HasSuffix(e, "@example.com") || strings.HasSuffix(e, "@example.org")
}

// AccountEmail is the real account email, or "" when unknown.
func (c *Client) AccountEmail() string {
	if !placeholder(c.Email) {
		return strings.TrimSpace(c.Email)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.email == "" {
		read := c.LocalEmail
		if read == nil {
			read = readLocalEmail
		}
		if e := read(); !placeholder(e) {
			c.email = e
		}
	}
	return c.email
}

func readLocalEmail() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(home, ".claude.json"))
	if err != nil {
		return ""
	}
	var v struct {
		OauthAccount struct {
			EmailAddress string `json:"emailAddress"`
		} `json:"oauthAccount"`
	}
	_ = json.Unmarshal(b, &v)
	return strings.TrimSpace(v.OauthAccount.EmailAddress)
}

func (c *Client) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (c *Client) get(ctx context.Context) (usageResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	min := c.MinInterval
	if min <= 0 {
		min = MinInterval
	}
	if !c.last.IsZero() && c.now().Sub(c.last) < min {
		return c.lastRes, c.lastErr
	}
	r, err := c.fetch(ctx)
	if err == nil && c.email == "" && placeholder(c.Email) && !c.profTry {
		c.profTry = true
		if e := c.profileEmail(ctx); !placeholder(e) {
			c.email = e
		}
	}
	// Do not cache a context cancellation; it says nothing about the server.
	if ctx.Err() == nil {
		c.last, c.lastRes, c.lastErr = c.now(), r, err
	}
	return r, err
}

func (c *Client) token(ctx context.Context) (string, error) {
	read := c.Credential
	if read == nil {
		read = func(ctx context.Context) (string, error) { return keychain.Read(ctx, KeychainService, "") }
	}
	raw, err := read(ctx)
	if err != nil {
		return "", provider.NoKey(SetupHint)
	}
	var cr credential
	if err := json.Unmarshal([]byte(raw), &cr); err != nil {
		return "", errors.New("Claude: credential không đúng định dạng")
	}
	tok := cr.ClaudeAiOauth.AccessToken
	if tok == "" {
		return "", provider.Stale("Claude: credential thiếu accessToken")
	}
	if exp := cr.ClaudeAiOauth.ExpiresAt; exp > 0 && !c.now().Before(time.UnixMilli(exp)) {
		return "", provider.Stale("Claude: token hết hạn, mở Claude Code để làm mới")
	}
	return tok, nil
}

func (c *Client) fetch(ctx context.Context) (usageResponse, error) {
	tok, err := c.token(ctx)
	if err != nil {
		return usageResponse{}, err
	}
	base := c.BaseURL
	if base == "" {
		base = DefaultBaseURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+"/api/oauth/usage", nil)
	if err != nil {
		return usageResponse{}, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("anthropic-beta", BetaHeader)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)
	hc := c.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return usageResponse{}, fmt.Errorf("Claude: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return usageResponse{}, fmt.Errorf("Claude: %w", err)
	}
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		// Token rejected: wait for Claude Code to refresh it, never refresh here.
		return usageResponse{}, provider.Stale("Claude: token bị từ chối (401)")
	case resp.StatusCode != http.StatusOK:
		return usageResponse{}, fmt.Errorf("Claude: HTTP %d", resp.StatusCode)
	}
	var r usageResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return usageResponse{}, fmt.Errorf("Claude: JSON không hợp lệ: %w", err)
	}
	return r, nil
}

// profileEmail asks GET /api/oauth/profile for account.email; "" on error.
func (c *Client) profileEmail(ctx context.Context) string {
	tok, err := c.token(ctx)
	if err != nil {
		return ""
	}
	base := c.BaseURL
	if base == "" {
		base = DefaultBaseURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+"/api/oauth/profile", nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("anthropic-beta", BetaHeader)
	req.Header.Set("User-Agent", userAgent)
	hc := c.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	var v struct {
		Account struct {
			Email        string `json:"email"`
			EmailAddress string `json:"email_address"`
		} `json:"account"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&v) != nil {
		return ""
	}
	if v.Account.Email != "" {
		return v.Account.Email
	}
	return v.Account.EmailAddress
}

// Provider is one row (one window) of the Claude account.
type Provider struct {
	Client *Client
	Window Window
}

// Rows returns the 5-hour and weekly rows sharing one client.
func Rows(c *Client) []provider.Provider {
	return []provider.Provider{&Provider{c, FiveHour}, &Provider{c, SevenDay}}
}

func (p *Provider) ID() string { return "claude:" + string(p.Window) }
func (p *Provider) Service() string {
	if p.Window == FiveHour {
		return "Claude 5h"
	}
	return "Claude tuần"
}
func (p *Provider) Account() string { return p.Client.AccountEmail() }

func (p *Provider) Fetch(ctx context.Context) (provider.Usage, error) {
	r, err := p.Client.get(ctx)
	if err != nil {
		return provider.Usage{}, err
	}
	w := r.FiveHour
	if p.Window == SevenDay {
		w = r.SevenDay
	}
	if w == nil || w.Utilization == nil {
		return provider.Usage{}, fmt.Errorf("Claude: thiếu %s trong phản hồi", p.Window)
	}
	u := provider.Usage{Kind: provider.KindQuota, UsedPercent: *w.Utilization}
	if w.ResetsAt != nil && *w.ResetsAt != "" {
		t, err := time.Parse(time.RFC3339Nano, *w.ResetsAt)
		if err != nil {
			return provider.Usage{}, fmt.Errorf("Claude: resets_at không hợp lệ %q", *w.ResetsAt)
		}
		u.ResetAt = t
		// A cached response may predate the reset: never pass it off as fresh.
		if !p.Client.now().Before(t) {
			return provider.Usage{}, provider.Reset("Claude: cửa sổ đã reset, chờ lần lấy số liệu mới")
		}
	}
	return u, nil
}
