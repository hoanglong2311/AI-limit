package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestCreatesDefaults(t *testing.T) {
	p := filepath.Join(t.TempDir(), "AI-limit", "config.json")
	c, err := Load(p)
	if err != nil || !reflect.DeepEqual(c, Defaults()) || c.Demo {
		t.Fatalf("%+v %v", c, err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatal("file not created")
	}
	c2, err := Load(p)
	if err != nil || !reflect.DeepEqual(c2, c) {
		t.Fatalf("reload %+v %v", c2, err)
	}
}

func TestPartialFileKeepsDefaults(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	os.WriteFile(p, []byte(`{"claude_email":"me@x.com","demo":true}`), 0o644)
	c, err := Load(p)
	if err != nil || c.ClaudeEmail != "me@x.com" || !c.Demo || c.CodexLabel != "ChatGPT" || c.Interval != "5m" {
		t.Fatalf("%+v %v", c, err)
	}
}

func TestBadJSON(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	os.WriteFile(p, []byte(`{`), 0o644)
	if c, err := Load(p); err == nil || !reflect.DeepEqual(c, Defaults()) {
		t.Fatalf("%+v %v", c, err)
	}
}

func TestInterval(t *testing.T) {
	for in, want := range map[string]time.Duration{"10m": 10 * time.Minute, "": 5 * time.Minute, "x": 5 * time.Minute, "10s": time.Minute, "-1m": 5 * time.Minute} {
		if got := (Config{Interval: in}).IntervalDuration(); got != want {
			t.Errorf("%q: %v want %v", in, got, want)
		}
	}
}

func TestEnabledDefaultsAndSave(t *testing.T) {
	c := Defaults()
	for _, k := range []string{Claude, Codex, DeepSeek} {
		if !c.Enabled(k) {
			t.Errorf("%s should default on", k)
		}
	}
	if c.Enabled(Grok) {
		t.Error("grok should default off")
	}
	d := c.SetEnabled(Codex, false)
	if c.Services != nil || d.Enabled(Codex) || !d.Enabled(Claude) {
		t.Fatalf("SetEnabled: %+v %+v", c, d)
	}
	path := filepath.Join(t.TempDir(), "sub", "config.json")
	if err := Save(path, d); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Enabled(Codex) || !got.Enabled(DeepSeek) || got.Enabled(Grok) {
		t.Fatalf("reloaded %+v", got)
	}
}
