package main

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/hoanglong2311/AI-limit/internal/config"
	"github.com/hoanglong2311/AI-limit/internal/provider"
	"github.com/hoanglong2311/AI-limit/internal/providers/claude"
	"github.com/hoanglong2311/AI-limit/internal/providers/deepseek"
	"github.com/hoanglong2311/AI-limit/internal/view"
)

// serviceNames are the Settings labels, in config.ServiceKeys order.
var serviceNames = map[string]string{
	config.Claude: "Claude", config.Codex: "Codex", config.DeepSeek: "DeepSeek", config.Grok: "Grok",
}

// settings is the Settings screen state. UI fields are touched on the UI
// thread only; mu guards cfg, which scheduler goroutines read via skip.
type settings struct {
	open bool
	path string
	on   map[string]*bool // switch values
	key  string           // DeepSeek key being typed; cleared after save
	msg  string           // result of save/check (never contains the key)
	ok   bool
	busy bool
	ids  map[string]string // provider ID -> service key

	mu  sync.Mutex
	cfg config.Config
}

func (s *settings) init(path string, cfg config.Config, ps []provider.Provider) {
	s.path, s.cfg = path, cfg
	s.on = map[string]*bool{}
	for _, k := range config.ServiceKeys {
		v := cfg.Enabled(k)
		s.on[k] = &v
	}
	s.ids = map[string]string{}
	for _, p := range ps {
		s.ids[p.ID()] = view.ServiceKey(p.Service())
	}
}

func (s *settings) enabled(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg.Enabled(key)
}

// skip is the scheduler hook: disabled services are not fetched.
func (s *settings) skip(id string) bool {
	k := s.ids[id]
	return k != "" && !s.enabled(k)
}

// sync persists switches that changed this frame; returns the services
// turned on so they can be fetched right away.
func (s *settings) sync() (turnedOn []string) {
	s.mu.Lock()
	cfg := s.cfg
	changed := false
	for _, k := range config.ServiceKeys {
		if v := *s.on[k]; v != cfg.Enabled(k) {
			cfg = cfg.SetEnabled(k, v)
			changed = true
			if v {
				turnedOn = append(turnedOn, k)
			}
		}
	}
	s.cfg = cfg
	s.mu.Unlock()
	if changed && s.path != "" {
		if err := config.Save(s.path, cfg); err != nil {
			log.Print("config: ", err)
		}
	}
	return turnedOn
}

func (a *app) openSettings() {
	a.set.open = true
	a.set.msg = ""
}

// providersFor returns the scheduler providers of the given service keys.
func (a *app) providersFor(keys []string) []provider.Provider {
	var out []provider.Provider
	for _, p := range a.sched.Providers() {
		for _, k := range keys {
			if a.set.ids[p.ID()] == k {
				out = append(out, p)
			}
		}
	}
	return out
}

func (a *app) refreshIn(ps []provider.Provider) {
	if len(ps) == 0 {
		return
	}
	go a.sched.RefreshAll(context.Background(), ps)
}

// refreshNow refreshes every enabled service at once. Claude keeps its
// 5-minute minimum: inside it, Claude is skipped and shows how long to wait.
func (a *app) refreshNow() {
	if a.refreshing {
		return
	}
	now := time.Now()
	wait := claude.CooldownLeft(a.claude.NextAllowed(), now)
	a.claudeWait = time.Time{}
	if wait > 0 {
		a.claudeWait = now.Add(wait)
	}
	gwait := claude.CooldownLeft(a.grok.NextAllowed(), now)
	a.grokWait = time.Time{}
	if gwait > 0 {
		a.grokWait = now.Add(gwait)
	}
	var ps []provider.Provider
	for _, p := range a.sched.Providers() {
		k := a.set.ids[p.ID()]
		if k != "" && !a.set.enabled(k) {
			continue
		}
		if (k == config.Claude && wait > 0) || (k == config.Grok && gwait > 0) {
			continue
		}
		ps = append(ps, p)
	}
	a.refreshing = true
	if a.upd.poller != nil {
		a.upd.poller.Trigger()
	}
	go func() {
		a.sched.RefreshAll(context.Background(), ps)
		snap := a.sched.Snapshot()
		a.win.Update(func() { a.rows, a.refreshing = snap, false })
	}()
}

func (a *app) saveKey() {
	err := deepseek.SaveKey(a.set.key)
	a.set.key = ""
	if err != nil {
		// err never contains the key (OSStatus code or "key trống").
		a.set.msg, a.set.ok = "Không lưu được key: "+err.Error(), false
		return
	}
	a.set.msg, a.set.ok = "Đã lưu key vào Keychain", true
	a.refreshIn(a.providersFor([]string{config.DeepSeek}))
}

// checkKey calls the balance API with the typed key, or the saved one when
// the field is empty, and shows only valid/invalid.
func (a *app) checkKey() {
	if a.set.busy {
		return
	}
	typed := strings.TrimSpace(a.set.key)
	p := &deepseek.Provider{Label: "check"}
	if typed != "" {
		p.Key = func(context.Context) (string, error) { return typed, nil }
	}
	a.set.busy, a.set.msg = true, "Đang kiểm tra…"
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_, err := p.Fetch(ctx)
		a.win.Update(func() {
			a.set.busy = false
			a.set.msg, a.set.ok = deepseek.CheckText(err), err == nil
		})
	}()
}

func (a *app) settingsView(c *ui.Context) {
	t := c.Theme()
	if c.Shortcut(0, ui.KeyEscape) {
		a.set.open = false
	}
	ui.Column(c).Fill().Padding(view.Pad).Gap(view.BlockGap).Children(func() {
		ui.Text(c, "Cài đặt").FontSize(view.TitleSize).FontWeight(600)
		ui.Column(c).Gap(view.RowGap).Children(func() {
			ui.Text(c, "Dịch vụ hiển thị").FontSize(view.SmallSize).TextColor(t.TextMuted)
			for _, k := range config.ServiceKeys {
				ui.Row(c.Key(k)).Gap(view.ColGap).AlignItems(ui.Center).Children(func() {
					ui.Text(c, serviceNames[k]).FontSize(view.BodySize).Grow(1)
					ui.Switch(c, a.set.on[k])
				})
			}
		})
		if on := a.set.sync(); len(on) > 0 {
			a.refreshIn(a.providersFor(on))
		}
		ui.Divider(c)
		ui.Column(c).Gap(view.RowGap).Children(func() {
			ui.Text(c, "API key DeepSeek").FontSize(view.BodySize)
			ui.TextInput(c, &a.set.key).Password().Placeholder("sk-…").FontSize(view.BodySize)
			ui.Row(c).Gap(view.ColGap).AlignItems(ui.Center).Children(func() {
				ui.Button(c, "Lưu vào Keychain").FontSize(view.SmallSize).
					Disabled(strings.TrimSpace(a.set.key) == "").OnClick(a.saveKey)
				ui.Button(c, "Kiểm tra").FontSize(view.SmallSize).Disabled(a.set.busy).OnClick(a.checkKey)
			})
			if a.set.msg != "" {
				m := ui.Text(c, a.set.msg).FontSize(view.SmallSize)
				if a.set.ok {
					m.TextColor(ui.Hex(view.GreenHex))
				} else if !a.set.busy {
					m.TextColor(t.Danger)
				}
			}
			ui.Text(c, "Key chỉ lưu trong macOS Keychain (mục ai-limit.deepseek).").
				FontSize(view.SmallSize).TextColor(t.TextMuted)
		})
		ui.Spacer(c)
		ui.Row(c).Children(func() {
			ui.Spacer(c)
			ui.Button(c, "Xong").OnClick(func() { a.set.open = false })
		})
	})
}
