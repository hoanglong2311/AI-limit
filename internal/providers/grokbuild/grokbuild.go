// Package grokbuild reports Grok Build credit usage for the current
// billing period, using the login Grok Build stores in ~/.grok/auth.json.
//
// The file is read-only here: only key, user_id, expires_at, create_time
// and auth_mode are decoded (refresh_token is never read), nothing is
// written and no token/refresh endpoint is ever called. An expired login
// shows "cần đăng nhập lại Grok Build" without any network call.
//
//	GET https://cli-chat-proxy.grok.com/v1/billing?format=credits
//	Authorization: Bearer <key>
//	x-userid: <user_id>
//
// Response (all optional): {"config":{"creditUsagePercent":12.5,
// "currentPeriod":{"end":"<RFC3339>"}}}.
package grokbuild

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/hoanglong2311/AI-limit/internal/provider"
	"github.com/hoanglong2311/AI-limit/internal/update"
)

const (
	// BaseURL is hardcoded on purpose: env vars and config.toml overrides
	// are ignored so the token can only go to grok.com.
	BaseURL     = "https://cli-chat-proxy.grok.com"
	billingPath = "/v1/billing?format=credits"
	// MinInterval is the shortest time between two API calls, manual
	// refresh included.
	MinInterval = 5 * time.Minute
	Timeout     = 15 * time.Second
	expirySlack = 5 * time.Minute
	noExpiryTTL = 30 * 24 * time.Hour

	SetupHint   = "Chưa đăng nhập Grok Build: cài Grok Build và chạy grok login"
	ReloginHint = "cần đăng nhập lại Grok Build"
	UsageHint   = "xem bằng lệnh /usage trong Grok Build"
)

// baseURL is overridden by tests only.
var baseURL = BaseURL

// entry is the subset of an auth.json scope entry that is decoded.
type entry struct {
	Key        string `json:"key"`
	UserID     string `json:"user_id"`
	ExpiresAt  string `json:"expires_at"`
	CreateTime string `json:"create_time"`
	AuthMode   string `json:"auth_mode"`
}

// login is the chosen credential.
type login struct {
	key, userID      string
	expires, created time.Time
}

// pick selects a web_login/oidc entry (never api_key); with several, the
// latest create_time wins.
func pick(raw []byte) (login, bool) {
	var m map[string]entry
	if json.Unmarshal(raw, &m) != nil {
		return login{}, false
	}
	var best login
	found := false
	for _, e := range m {
		if (e.AuthMode != "web_login" && e.AuthMode != "oidc") || e.Key == "" || e.UserID == "" {
			continue
		}
		l := login{key: e.Key, userID: e.UserID}
		l.created, _ = time.Parse(time.RFC3339Nano, e.CreateTime)
		l.expires, _ = time.Parse(time.RFC3339Nano, e.ExpiresAt)
		if !found || l.created.After(best.created) {
			best, found = l, true
		}
	}
	return best, found
}

// expired: now >= expires_at-5min, or without expires_at, create_time+30d
// has passed (unknown create_time counts as expired: never guess).
func (l login) expired(now time.Time) bool {
	if !l.expires.IsZero() {
		return !now.Before(l.expires.Add(-expirySlack))
	}
	return l.created.IsZero() || !now.Before(l.created.Add(noExpiryTTL))
}

// Client fetches at most once per MinInterval and shares the result.
type Client struct {
	// AuthPath defaults to ~/.grok/auth.json.
	AuthPath string
	HTTP     *http.Client
	Now      func() time.Time

	mu      sync.Mutex
	last    time.Time
	lastRes provider.Usage
	lastErr error
}

func (c *Client) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

// NextAllowed is the earliest time of the next API call; zero if none yet.
func (c *Client) NextAllowed() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.last.IsZero() {
		return time.Time{}
	}
	return c.last.Add(MinInterval)
}

func (c *Client) authPath() string {
	if c.AuthPath != "" {
		return c.AuthPath
	}
	h, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(h, ".grok", "auth.json")
}

// HTTPError holds only the status code.
type HTTPError struct{ Status int }

func (e *HTTPError) Error() string { return fmt.Sprintf("Grok Build: HTTP %d", e.Status) }

var errRedirect = errors.New("redirect bị chặn")

func (c *Client) get(ctx context.Context) (provider.Usage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.last.IsZero() && c.now().Sub(c.last) < MinInterval {
		return c.lastRes, c.lastErr
	}
	path := c.authPath()
	raw, err := os.ReadFile(path)
	if err != nil {
		return provider.Usage{}, provider.NoKey(SetupHint)
	}
	l, ok := pick(raw)
	if !ok {
		return provider.Usage{}, provider.NoKey(SetupHint)
	}
	if l.expired(c.now()) {
		return provider.Usage{}, provider.NoKey(ReloginHint)
	}
	u, err := c.call(ctx, l)
	if ctx.Err() == nil {
		c.last, c.lastRes, c.lastErr = c.now(), u, err
	}
	return u, err
}

func (c *Client) call(ctx context.Context, l login) (provider.Usage, error) {
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+billingPath, nil)
	if err != nil {
		return provider.Usage{}, errors.New("Grok Build: không tạo được request")
	}
	req.Header.Set("Authorization", "Bearer "+l.key)
	req.Header.Set("x-userid", l.userID)
	req.Header.Set("User-Agent", "ai-limit/"+update.Current)
	req.Header.Set("Accept", "application/json")
	hc := http.Client{Timeout: Timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return errRedirect }}
	if c.HTTP != nil {
		hc.Transport = c.HTTP.Transport
	}
	resp, err := hc.Do(req)
	if err != nil {
		// Never wrap err: url.Error carries the URL only, but keep the
		// message fixed so no request detail can leak.
		return provider.Usage{}, errors.New("Grok Build: lỗi mạng")
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return provider.Usage{}, provider.NoKey(ReloginHint)
	case resp.StatusCode != http.StatusOK:
		return provider.Usage{}, &HTTPError{Status: resp.StatusCode}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return provider.Usage{}, errors.New("Grok Build: lỗi đọc phản hồi")
	}
	return parse(body)
}

type billing struct {
	Config *struct {
		CreditUsagePercent *float64 `json:"creditUsagePercent"`
		CurrentPeriod      *struct {
			End string `json:"end"`
		} `json:"currentPeriod"`
	} `json:"config"`
}

func parse(body []byte) (provider.Usage, error) {
	var b billing
	if err := json.Unmarshal(body, &b); err != nil {
		return provider.Usage{}, errors.New("Grok Build: JSON không hợp lệ")
	}
	u := provider.Usage{Kind: provider.KindQuota}
	if b.Config == nil {
		return u, nil
	}
	if p := b.Config.CreditUsagePercent; p != nil {
		if *p < 0 || *p > 100 {
			return provider.Usage{}, fmt.Errorf("Grok Build: creditUsagePercent ngoài 0..100 (%g)", *p)
		}
		u.UsedPercent = *p
	}
	if cp := b.Config.CurrentPeriod; cp != nil && cp.End != "" {
		t, err := time.Parse(time.RFC3339Nano, cp.End)
		if err != nil {
			return provider.Usage{}, errors.New("Grok Build: currentPeriod.end không hợp lệ")
		}
		u.ResetAt = t
	}
	return u, nil
}

// Provider is the Grok weekly row.
type Provider struct{ Client *Client }

func (p *Provider) ID() string      { return "grok:tuần" }
func (p *Provider) Service() string { return "Grok tuần" }
func (p *Provider) Account() string { return "" }

func (p *Provider) Fetch(ctx context.Context) (provider.Usage, error) {
	u, err := p.Client.get(ctx)
	if err != nil {
		return provider.Usage{}, err
	}
	if !u.ResetAt.IsZero() && !p.Client.now().Before(u.ResetAt) {
		return provider.Usage{}, provider.Reset("Grok Build: kỳ đã reset, chờ số liệu mới")
	}
	return u, nil
}
