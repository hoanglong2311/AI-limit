package deepseek

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
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
