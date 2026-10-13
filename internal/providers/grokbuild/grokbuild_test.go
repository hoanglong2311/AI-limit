package grokbuild

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hoanglong2311/AI-limit/internal/provider"
)

const (
	secretKey = "xai-secret-token-abc"
	secretUID = "user-1234-secret"
)

var now = time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)

func authFile(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "auth.json")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func validAuth(t *testing.T) string {
	return authFile(t, `{"https://api.x.ai":{"key":"`+secretKey+`","user_id":"`+secretUID+`",
		"expires_at":"2026-10-14T00:00:00Z","create_time":"2026-10-01T00:00:00Z","auth_mode":"web_login",
		"refresh_token":"rt-should-never-be-read"}}`)
}

type srv struct {
	calls atomic.Int32
	hdr   http.Header
}

func setup(t *testing.T, status int, body string) (*srv, *Client) {
	t.Helper()
	s := &srv{}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.calls.Add(1)
		s.hdr = r.Header.Clone()
		if r.URL.Path != "/v1/billing" || r.URL.Query().Get("format") != "credits" {
			t.Errorf("path %s", r.URL)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(ts.Close)
	old := baseURL
	baseURL = ts.URL
	t.Cleanup(func() { baseURL = old })
	return s, &Client{AuthPath: validAuth(t), Now: func() time.Time { return now }}
}

func noSecret(t *testing.T, err error) {
	t.Helper()
	if err != nil && (strings.Contains(err.Error(), secretKey) || strings.Contains(err.Error(), secretUID)) {
		t.Fatalf("secret in error: %v", err)
	}
}

func TestParseAndHeaders(t *testing.T) {
	s, c := setup(t, 200, `{"config":{"creditUsagePercent":42.5,"currentPeriod":{"start":"x","end":"2026-10-16T16:00:00Z"}}}`)
	u, err := (&Provider{c}).Fetch(context.Background())
	if err != nil || u.UsedPercent != 42.5 || !u.ResetAt.Equal(time.Date(2026, 10, 16, 16, 0, 0, 0, time.UTC)) || u.Kind != provider.KindQuota {
		t.Fatalf("%+v %v", u, err)
	}
	h := s.hdr
	if h.Get("Authorization") != "Bearer "+secretKey || h.Get("x-userid") != secretUID ||
		h.Get("Accept") != "application/json" || !strings.HasPrefix(h.Get("User-Agent"), "ai-limit/") {
		t.Fatalf("headers %v", h)
	}
	for k := range h {
		lk := strings.ToLower(k)
		if lk == "x-xai-token-auth" || strings.Contains(lk, "grok") || strings.Contains(lk, "shell") {
			t.Fatalf("forbidden header %s", k)
		}
	}
}

func TestParseMissingFields(t *testing.T) {
	for _, body := range []string{`{}`, `{"config":{}}`, `{"config":{"currentPeriod":{}}}`} {
		u, err := parse([]byte(body))
		if err != nil || u.UsedPercent != 0 || !u.ResetAt.IsZero() {
			t.Fatalf("%s: %+v %v", body, u, err)
		}
	}
	for _, body := range []string{`nope`, `{"config":{"creditUsagePercent":"x"}}`, `{"config":{"creditUsagePercent":150}}`,
		`{"config":{"currentPeriod":{"end":"bad"}}}`} {
		if _, err := parse([]byte(body)); err == nil {
			t.Fatalf("%s: want error", body)
		}
	}
}

func TestUnauthorized(t *testing.T) {
	for _, code := range []int{401, 403} {
		_, c := setup(t, code, `{"error":"`+secretKey+`"}`)
		_, err := (&Provider{c}).Fetch(context.Background())
		noSecret(t, err)
		if !errors.Is(err, provider.ErrNoKey) || err.Error() != ReloginHint {
			t.Fatalf("%d: %v", code, err)
		}
	}
}

func TestServerErrorAndBadJSON(t *testing.T) {
	_, c := setup(t, 500, `oops`)
	_, err := (&Provider{c}).Fetch(context.Background())
	var he *HTTPError
	if !errors.As(err, &he) || he.Status != 500 || errors.Is(err, provider.ErrNoKey) {
		t.Fatal(err)
	}
	noSecret(t, err)
	_, c = setup(t, 200, `{"config":`)
	_, err = (&Provider{c}).Fetch(context.Background())
	if err == nil || errors.Is(err, provider.ErrNoKey) {
		t.Fatal(err)
	}
	noSecret(t, err)
}

func TestRedirectRefused(t *testing.T) {
	var hit atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hit.Add(1) }))
	defer other.Close()
	_, c := setup(t, 0, "")
	ts := httptest.NewServer(http.RedirectHandler(other.URL, http.StatusFound))
	defer ts.Close()
	baseURL = ts.URL
	_, err := c.get(context.Background())
	if err == nil || hit.Load() != 0 {
		t.Fatalf("redirect followed: %v", err)
	}
	noSecret(t, err)
}

func TestExpiredNoCall(t *testing.T) {
	cases := map[string]string{
		"within 5 min":  `{"s":{"key":"` + secretKey + `","user_id":"u","expires_at":"2026-10-13T12:04:00Z","auth_mode":"oidc"}}`,
		"past":          `{"s":{"key":"` + secretKey + `","user_id":"u","expires_at":"2026-10-12T00:00:00Z","auth_mode":"web_login"}}`,
		"no expiry 30d": `{"s":{"key":"` + secretKey + `","user_id":"u","create_time":"2026-09-01T00:00:00Z","auth_mode":"web_login"}}`,
	}
	for name, body := range cases {
		s, c := setup(t, 200, `{}`)
		c.AuthPath = authFile(t, body)
		_, err := (&Provider{c}).Fetch(context.Background())
		if s.calls.Load() != 0 || !errors.Is(err, provider.ErrNoKey) || err.Error() != ReloginHint {
			t.Fatalf("%s: calls=%d err=%v", name, s.calls.Load(), err)
		}
		noSecret(t, err)
	}
	// No expiry but recent create_time is still valid.
	s, c := setup(t, 200, `{}`)
	c.AuthPath = authFile(t, `{"s":{"key":"k","user_id":"u","create_time":"2026-10-10T00:00:00Z","auth_mode":"web_login"}}`)
	if _, err := c.get(context.Background()); err != nil || s.calls.Load() != 1 {
		t.Fatal(err)
	}
}

func TestNoAuthFile(t *testing.T) {
	s, c := setup(t, 200, `{}`)
	c.AuthPath = filepath.Join(t.TempDir(), "missing.json")
	_, err := c.get(context.Background())
	if !errors.Is(err, provider.ErrNoKey) || err.Error() != SetupHint || s.calls.Load() != 0 {
		t.Fatal(err)
	}
	c.AuthPath = authFile(t, `{"s":{"key":"k","user_id":"u","auth_mode":"api_key","create_time":"2026-10-12T00:00:00Z"}}`)
	if _, err := c.get(context.Background()); err == nil || err.Error() != SetupHint || s.calls.Load() != 0 {
		t.Fatalf("api_key entry used: %v", err)
	}
}

func TestPickEntry(t *testing.T) {
	l, ok := pick([]byte(`{
		"a":{"key":"api","user_id":"u0","auth_mode":"api_key","create_time":"2026-10-12T00:00:00Z"},
		"b":{"key":"old","user_id":"u1","auth_mode":"web_login","create_time":"2026-10-01T00:00:00Z"},
		"c":{"key":"new","user_id":"u2","auth_mode":"oidc","create_time":"2026-10-05T00:00:00Z"},
		"d":{"key":"","user_id":"u3","auth_mode":"web_login","create_time":"2026-10-11T00:00:00Z"}}`))
	if !ok || l.key != "new" || l.userID != "u2" {
		t.Fatalf("%+v %v", l.userID, ok)
	}
	if _, ok := pick([]byte(`[]`)); ok {
		t.Fatal("array accepted")
	}
}

func TestCooldown(t *testing.T) {
	s, c := setup(t, 200, `{"config":{"creditUsagePercent":10}}`)
	cur := now
	c.Now = func() time.Time { return cur }
	if !c.NextAllowed().IsZero() {
		t.Fatal("next before call")
	}
	p := &Provider{c}
	_, _ = p.Fetch(context.Background())
	cur = cur.Add(4 * time.Minute)
	_, _ = p.Fetch(context.Background())
	if s.calls.Load() != 1 {
		t.Fatalf("calls %d within 5 min", s.calls.Load())
	}
	if !c.NextAllowed().Equal(now.Add(MinInterval)) {
		t.Fatal(c.NextAllowed())
	}
	cur = now.Add(5 * time.Minute)
	_, _ = p.Fetch(context.Background())
	if s.calls.Load() != 2 {
		t.Fatalf("calls %d after 5 min", s.calls.Load())
	}
}

func TestResetPassed(t *testing.T) {
	_, c := setup(t, 200, `{"config":{"creditUsagePercent":70,"currentPeriod":{"end":"2026-10-13T11:00:00Z"}}}`)
	_, err := (&Provider{c}).Fetch(context.Background())
	if !errors.Is(err, provider.ErrReset) {
		t.Fatal(err)
	}
}
