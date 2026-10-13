package claude

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hoanglong2311/AI-limit/internal/provider"
)

var now = time.Date(2026, 10, 13, 2, 0, 0, 0, time.UTC)

func cred(tok string, exp time.Time) func(context.Context) (string, error) {
	return func(context.Context) (string, error) {
		return fmt.Sprintf(`{"claudeAiOauth":{"accessToken":%q,"refreshToken":"r","expiresAt":%d,"scopes":["user:inference"],"subscriptionType":"max"}}`, tok, exp.UnixMilli()), nil
	}
}

const okBody = `{"five_hour":{"utilization":83.0,"resets_at":"2026-10-13T05:00:00.528743+00:00"},
"seven_day":{"utilization":13.0,"resets_at":"2026-10-17T00:59:59.951713+00:00"},
"seven_day_opus":null,"extra_usage":{"is_enabled":false}}`

func setup(t *testing.T, status int, body string, c func(context.Context) (string, error)) (*Client, *atomic.Int32) {
	t.Helper()
	var n atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		if r.Method != http.MethodGet || r.URL.Path != "/api/oauth/usage" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer tok" || r.Header.Get("anthropic-beta") != BetaHeader {
			t.Errorf("bad headers %v", r.Header)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(s.Close)
	return &Client{Email: "me@example.com", BaseURL: s.URL, HTTP: s.Client(), Credential: c,
		Now: func() time.Time { return now }}, &n
}

func TestBothWindowsOneRequest(t *testing.T) {
	c, n := setup(t, 200, okBody, cred("tok", now.Add(time.Hour)))
	rows := Rows(c)
	u5, err := rows[0].Fetch(context.Background())
	if err != nil || u5.UsedPercent != 83 || u5.ResetAt.Hour() != 5 {
		t.Fatalf("5h %+v %v", u5, err)
	}
	u7, err := rows[1].Fetch(context.Background())
	if err != nil || u7.UsedPercent != 13 || u7.ResetAt.Day() != 17 {
		t.Fatalf("7d %+v %v", u7, err)
	}
	if n.Load() != 1 {
		t.Fatalf("requests = %d, want 1", n.Load())
	}
	if rows[0].Account() != "me@example.com" || rows[0].ID() == rows[1].ID() {
		t.Fatal("labels")
	}
}

func TestMinInterval(t *testing.T) {
	c, n := setup(t, 200, okBody, cred("tok", now.Add(24*time.Hour)))
	cur := now
	c.Now = func() time.Time { return cur }
	p := Rows(c)[0]
	p.Fetch(context.Background())
	cur = cur.Add(4 * time.Minute)
	p.Fetch(context.Background())
	if n.Load() != 1 {
		t.Fatalf("refetched before 5m: %d", n.Load())
	}
	cur = cur.Add(2 * time.Minute)
	p.Fetch(context.Background())
	if n.Load() != 2 {
		t.Fatalf("did not refetch after 5m: %d", n.Load())
	}
}

func TestStaleCases(t *testing.T) {
	cases := map[string]struct {
		status int
		cred   func(context.Context) (string, error)
	}{
		"missing": {200, func(context.Context) (string, error) { return "", errors.New("not found") }},
		"expired": {200, cred("tok", now.Add(-time.Minute))},
		"noToken": {200, cred("", now.Add(time.Hour))},
		"401":     {401, cred("tok", now.Add(time.Hour))},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			c, n := setup(t, tc.status, `{}`, tc.cred)
			_, err := Rows(c)[0].Fetch(context.Background())
			if !errors.Is(err, provider.ErrStale) {
				t.Fatalf("want stale, got %v", err)
			}
			if name != "401" && n.Load() != 0 {
				t.Fatal("called API without a valid token")
			}
		})
	}
}

func TestErrors(t *testing.T) {
	cases := map[string]struct {
		status int
		body   string
	}{
		"429":      {429, `{"error":{"type":"rate_limit_error"}}`},
		"500":      {500, ``},
		"badJSON":  {200, `{nope`},
		"missing":  {200, `{"seven_day":{"utilization":1}}`},
		"badReset": {200, `{"five_hour":{"utilization":1,"resets_at":"tomorrow"}}`},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			c, _ := setup(t, tc.status, tc.body, cred("tok", now.Add(time.Hour)))
			_, err := Rows(c)[0].Fetch(context.Background())
			if err == nil || errors.Is(err, provider.ErrStale) {
				t.Fatalf("want hard error, got %v", err)
			}
		})
	}
}

func TestBadCredentialJSON(t *testing.T) {
	c, _ := setup(t, 200, okBody, func(context.Context) (string, error) { return "{x", nil })
	if _, err := Rows(c)[0].Fetch(context.Background()); err == nil {
		t.Fatal("want error")
	}
}
