package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

func TestCurrentMatchesMygoJSON(t *testing.T) {
	b, err := os.ReadFile("../../mygo.json")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`"version":\s*"([^"]+)"`).FindSubmatch(b)
	if m == nil || string(m[1]) != Current {
		t.Fatalf("mygo.json version %q != Current %q", m, Current)
	}
}

func TestNewer(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"0.1.4", "0.1.3", true}, {"v0.2.0", "0.1.9", true}, {"1.0.0", "0.9.9", true},
		{"0.1.10", "0.1.9", true}, {"0.1.3", "0.1.3", false}, {"0.1.2", "0.1.3", false},
		{"v0.1.4-rc1", "0.1.3", true}, {"garbage", "0.1.3", false}, {"0.1", "0.1.3", false},
	} {
		if got := Newer(c.a, c.b); got != c.want {
			t.Errorf("Newer(%q,%q)=%v", c.a, c.b, got)
		}
	}
}

const sum = "2c1d98b5917680b2eeb6e85e96d79d754027418fe3f0d1f5302902681e860d6b"

func TestParse(t *testing.T) {
	body := `{"tag_name":"v0.1.4","body":"**SHA-256**:\n` + "```\\n" + sum + "\\n```" + `",
	"assets":[{"name":"AI-limit-0.1.4-universal.dmg.sha256","browser_download_url":"https://x/s"},
	{"name":"AI-limit-0.1.4-universal.dmg","browser_download_url":"https://x/d"}]}`
	r, err := Parse([]byte(body))
	if err != nil || r.Version != "0.1.4" || r.DMGURL != "https://x/d" || r.SHA256URL != "https://x/s" || r.NotesSHA != sum {
		t.Fatalf("%+v %v", r, err)
	}
	if _, err := Parse([]byte(`{"tag_name":"v1.0.0","assets":[]}`)); err == nil {
		t.Fatal("want no-dmg error")
	}
	if _, err := Parse([]byte(`{"tag_name":"v1.0.0","prerelease":true}`)); !errors.Is(err, ErrQuiet) {
		t.Fatal("prerelease should be quiet")
	}
}

func TestVerify(t *testing.T) {
	if !Verify(sum, sum) || !Verify("  "+sum, "2C1D98B5917680B2EEB6E85E96D79D754027418FE3F0D1F5302902681E860D6B") ||
		Verify(sum, "") || Verify(sum[:63]+"0", sum) {
		t.Fatal("verify")
	}
}

func server(t *testing.T, dmg []byte, shaFile string, status int) (*Checker, string) {
	t.Helper()
	mux := http.NewServeMux()
	var s *httptest.Server
	mux.HandleFunc("/latest", func(w http.ResponseWriter, r *http.Request) {
		if status != 200 {
			w.WriteHeader(status)
			return
		}
		fmt.Fprintf(w, `{"tag_name":"v9.0.0","body":"","assets":[
		{"name":"a.dmg","browser_download_url":"%[1]s/a.dmg"},
		{"name":"a.dmg.sha256","browser_download_url":"%[1]s/a.dmg.sha256"}]}`, s.URL)
	})
	mux.HandleFunc("/a.dmg", func(w http.ResponseWriter, r *http.Request) { w.Write(dmg) })
	mux.HandleFunc("/a.dmg.sha256", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, shaFile) })
	s = httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return &Checker{URL: s.URL + "/latest", HTTP: s.Client(), AllowAnyHost: true}, s.URL
}

func TestDownloadVerified(t *testing.T) {
	dmg := []byte("fake dmg")
	h := sha256.Sum256(dmg)
	c, _ := server(t, dmg, hex.EncodeToString(h[:])+"  a.dmg\n", 200)
	r, newer, err := c.Latest(context.Background(), "0.1.3")
	if err != nil || !newer {
		t.Fatal(newer, err)
	}
	dir := t.TempDir()
	p, err := c.Download(context.Background(), r, dir)
	if err != nil || filepath.Base(p) != "a.dmg" {
		t.Fatal(p, err)
	}
	if got, _ := FileSHA256(p); got != hex.EncodeToString(h[:]) {
		t.Fatal("hash")
	}
}

func TestDownloadMismatchDeletes(t *testing.T) {
	c, _ := server(t, []byte("tampered"), sum+"  a.dmg\n", 200)
	r, _, _ := c.Latest(context.Background(), "0.1.3")
	dir := t.TempDir()
	if _, err := c.Download(context.Background(), r, dir); err == nil {
		t.Fatal("want mismatch error")
	}
	if ents, _ := os.ReadDir(dir); len(ents) != 0 {
		t.Fatalf("left files: %v", ents)
	}
}

func TestRateLimitQuiet(t *testing.T) {
	c, _ := server(t, nil, "", 403)
	if _, _, err := c.Latest(context.Background(), "0.1.3"); !errors.Is(err, ErrQuiet) {
		t.Fatal(err)
	}
	off := &Checker{URL: "http://127.0.0.1:1/x"}
	if _, _, err := off.Latest(context.Background(), "0.1.3"); !errors.Is(err, ErrQuiet) {
		t.Fatal(err)
	}
}
