package deepseek

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hoanglong2311/AI-limit/internal/provider"
)

func keyOK(context.Context) (string, error) { return "sk-test", nil }

func server(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/user/balance" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sk-test" {
			t.Errorf("auth header %q", got)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(s.Close)
	return s
}

func fetch(t *testing.T, status int, body string) (provider.Usage, error) {
	s := server(t, status, body)
	p := &Provider{Label: "x", BaseURL: s.URL, Client: s.Client(), Key: keyOK}
	return p.Fetch(context.Background())
}

func TestUSDPreferred(t *testing.T) {
	u, err := fetch(t, 200, `{"is_available":true,"balance_infos":[
		{"currency":"CNY","total_balance":"110.00"},
		{"currency":"USD","total_balance":"12.34","granted_balance":"0","topped_up_balance":"12.34"}]}`)
	if err != nil || u.Kind != provider.KindBalance || u.Currency != "USD" || u.Balance != 12.34 {
		t.Fatalf("%+v %v", u, err)
	}
}

func TestCNYOnlyShownHonestly(t *testing.T) {
	u, err := fetch(t, 200, `{"is_available":true,"balance_infos":[{"currency":"CNY","total_balance":"110.00","granted_balance":"10.00","topped_up_balance":"100.00"}]}`)
	if err != nil || u.Currency != "CNY" || u.Balance != 110 {
		t.Fatalf("%+v %v", u, err)
	}
}

func TestErrors(t *testing.T) {
	cases := map[string]struct {
		status int
		body   string
	}{
		"http401":    {401, `{"error":"bad key"}`},
		"http500":    {500, ``},
		"badJSON":    {200, `{not json`},
		"empty":      {200, `{"is_available":true,"balance_infos":[]}`},
		"badNumber":  {200, `{"balance_infos":[{"currency":"USD","total_balance":"abc"}]}`},
		"noCurrency": {200, `{"balance_infos":[{"total_balance":"1"}]}`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := fetch(t, c.status, c.body); err == nil {
				t.Fatal("want error")
			}
		})
	}
}

func TestNoKey(t *testing.T) {
	called := false
	s := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	defer s.Close()
	for _, k := range []func(context.Context) (string, error){
		func(context.Context) (string, error) { return "", errors.New("no item") },
		func(context.Context) (string, error) { return "  ", nil },
	} {
		p := &Provider{BaseURL: s.URL, Key: k}
		if _, err := p.Fetch(context.Background()); !errors.Is(err, provider.ErrNoKey) || err.Error() != AddKeyHint {
			t.Fatalf("want ErrNoKey, got %v", err)
		}
	}
	if called {
		t.Fatal("should not call API without a key")
	}
}

func TestCheckTextNeverEchoesKey(t *testing.T) {
	const key = "sk-secret-123"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	p := &Provider{BaseURL: srv.URL, Key: func(context.Context) (string, error) { return key, nil }}
	_, err := p.Fetch(context.Background())
	if err == nil || strings.Contains(err.Error(), key) {
		t.Fatalf("err %v", err)
	}
	if got := CheckText(err); got != CheckBad {
		t.Fatal(got)
	}
	if CheckText(nil) != CheckOK {
		t.Fatal("ok text")
	}
}

func TestCheckTextByStatus(t *testing.T) {
	const key = "sk-secret-456"
	cases := map[int]string{401: CheckBad, 403: CheckBad, 500: CheckRetry, 502: CheckRetry, 429: CheckRetry, 404: CheckRetry}
	for code, want := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(code) }))
		p := &Provider{BaseURL: srv.URL, Key: func(context.Context) (string, error) { return key, nil }}
		_, err := p.Fetch(context.Background())
		srv.Close()
		var he *HTTPError
		if !errors.As(err, &he) || he.Status != code || strings.Contains(err.Error(), key) {
			t.Fatalf("%d: %v", code, err)
		}
		if got := CheckText(err); got != want {
			t.Errorf("%d: %q want %q", code, got, want)
		}
	}
}

func TestCheckTextNetworkError(t *testing.T) {
	const key = "sk-secret-789"
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close() // connection refused
	p := &Provider{BaseURL: url, Key: func(context.Context) (string, error) { return key, nil }}
	_, err := p.Fetch(context.Background())
	if err == nil || strings.Contains(err.Error(), key) {
		t.Fatalf("err %v", err)
	}
	if got := CheckText(err); got != CheckRetry {
		t.Fatal(got)
	}
	if got := CheckText(errors.New("deepseek: JSON không hợp lệ")); got != CheckRetry {
		t.Fatal(got)
	}
}

func TestKeychainAccountMatchesSave(t *testing.T) {
	// SaveKey writes (KeychainService, KeychainAccount) and key() must read
	// exactly that item, not any account of the service.
	var gotSvc, gotAcct string
	old := keychainGet
	keychainGet = func(svc, acct string) (string, error) { gotSvc, gotAcct = svc, acct; return "k", nil }
	defer func() { keychainGet = old }()
	if _, err := (&Provider{}).key(context.Background()); err != nil {
		t.Fatal(err)
	}
	if gotSvc != KeychainService || gotAcct != "default" {
		t.Fatalf("lookup %q %q", gotSvc, gotAcct)
	}
}
