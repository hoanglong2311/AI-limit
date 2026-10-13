// Package config loads the app settings file. On macOS it lives at
// ~/Library/Application Support/AI-limit/config.json. It holds no secrets.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// Config is the JSON file. Interval is a Go duration string, e.g. "5m".
type Config struct {
	ClaudeEmail string `json:"claude_email"`
	CodexLabel  string `json:"codex_label"`
	Interval    string `json:"interval"`
	Demo        bool   `json:"demo"`
	// Services turns rows on/off by service key (see ServiceKeys). A
	// missing key means the default from Defaults.
	Services map[string]bool `json:"services"`
}

// Service keys used in Services.
const (
	Claude   = "claude"
	Codex    = "codex"
	DeepSeek = "deepseek"
	Grok     = "grok"
)

// ServiceKeys is the order shown in Settings.
var ServiceKeys = []string{Claude, Codex, DeepSeek, Grok}

// Enabled reports whether a service is on. Grok is off by default (no
// data source yet); the others are on.
func (c Config) Enabled(key string) bool {
	if v, ok := c.Services[key]; ok {
		return v
	}
	return key != Grok
}

// SetEnabled returns a copy with one service toggled (the map is copied).
func (c Config) SetEnabled(key string, on bool) Config {
	m := make(map[string]bool, len(ServiceKeys))
	for _, k := range ServiceKeys {
		m[k] = c.Enabled(k)
	}
	m[key] = on
	c.Services = m
	return c
}

// Save writes c to path atomically.
func Save(path string, c Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(c, "", "  ")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Defaults are written when the file is missing.
func Defaults() Config {
	return Config{CodexLabel: "ChatGPT", Interval: "5m", Demo: false}
}

// IntervalDuration parses Interval, falling back to 5m when empty or invalid
// and enforcing a 1-minute minimum.
func (c Config) IntervalDuration() time.Duration {
	d, err := time.ParseDuration(c.Interval)
	if err != nil || d <= 0 {
		return 5 * time.Minute
	}
	return max(d, time.Minute)
}

// DefaultPath is <UserConfigDir>/AI-limit/config.json.
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "AI-limit", "config.json"), nil
}

// Load reads path; if it does not exist, it writes Defaults there and
// returns them. Missing fields keep their default values.
func Load(path string) (Config, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		c := Defaults()
		if err := write(path, c); err != nil {
			return c, err
		}
		return c, nil
	}
	if err != nil {
		return Defaults(), err
	}
	c := Defaults()
	if err := json.Unmarshal(b, &c); err != nil {
		return Defaults(), fmt.Errorf("config %s: %w", path, err)
	}
	return c, nil
}

func write(path string, c Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(c, "", "  ")
	return os.WriteFile(path, append(b, '\n'), 0o644)
}
