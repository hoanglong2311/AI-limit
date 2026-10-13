// Command ai-limit is a macOS menu bar app showing AI usage limits.
package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"log"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/hoanglong2311/AI-limit/internal/config"
	"github.com/hoanglong2311/AI-limit/internal/provider"
	"github.com/hoanglong2311/AI-limit/internal/providers/claude"
	"github.com/hoanglong2311/AI-limit/internal/providers/codex"
	"github.com/hoanglong2311/AI-limit/internal/providers/deepseek"
	"github.com/hoanglong2311/AI-limit/internal/providers/grokbuild"
	"github.com/hoanglong2311/AI-limit/internal/providers/mock"
	"github.com/hoanglong2311/AI-limit/internal/scheduler"
)

type app struct {
	win      *mygo.Window
	tray     *mygo.Tray
	sched    *scheduler.Scheduler
	rows     []scheduler.State
	quitting bool
}

func fmtTime(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return t.Local().Format("15:04 02/01")
}

func fmtMoney(v float64, cur string) string {
	switch cur {
	case "USD", "":
		return fmt.Sprintf("$%.2f", v)
	case "CNY":
		return fmt.Sprintf("¥%.2f CNY", v)
	}
	return fmt.Sprintf("%.2f %s", v, cur)
}

func (a *app) row(c *ui.Context, s scheduler.State) {
	t := c.Theme()
	card := ui.Column(c.Key(s.ID)).Gap(4).Padding(8).Radius(8)
	if s.High() {
		card = card.Background(t.Warning.Alpha(0.18))
	}
	card.Children(func() {
		ui.Row(c).Gap(6).Children(func() {
			name := ui.Text(c, s.Service).Bold()
			if s.High() {
				name.TextColor(t.Danger)
			}
			ui.Text(c, s.Account).Grow(1).TextColor(t.TextMuted)
			switch {
			case !s.Fresh():
				ui.Text(c, s.Label()).TextColor(t.Danger)
			case s.Usage.Kind == provider.KindBalance:
				ui.Text(c, fmtMoney(s.Usage.Balance, s.Usage.Currency)).Bold()
			default:
				pct := ui.Text(c, fmt.Sprintf("%.0f%%", s.Usage.UsedPercent))
				if s.High() {
					pct.Bold().TextColor(t.Danger)
				}
			}
		})
		if s.Fresh() && s.Usage.Kind == provider.KindQuota {
			ui.Progress(c, s.Usage.UsedPercent/100)
			ui.Text(c, "Reset: "+fmtTime(s.Usage.ResetAt)).FontSize(11).TextColor(t.TextMuted)
		}
		ui.Text(c, "Cập nhật thành công lần cuối: "+fmtTime(s.LastSuccess)).FontSize(11).TextColor(t.TextMuted)
	})
}

func (a *app) view(c *ui.Context) {
	if c.Shortcut(0, ui.KeyEscape) {
		a.win.Hide()
	}
	c.OnShortcut(ui.Cmd, ui.KeyQ, mygo.App.Quit)
	ui.Column(c).Fill().Padding(12).Gap(8).Children(func() {
		ui.Text(c, "AI Limit").FontSize(16).Bold()
		ui.Scroll(c).Grow(1).Children(func() {
			ui.Column(c).Gap(6).Children(func() {
				for _, s := range a.rows {
					a.row(c, s)
				}
			})
		})
		ui.Row(c).Gap(8).Children(func() {
			ui.Spacer(c)
			ui.Button(c, "Thoát").OnClick(mygo.App.Quit)
		})
	})
}

func (a *app) toggle() {
	if a.win.IsVisible() {
		a.win.Hide()
		return
	}
	if b := a.tray.Bounds(); b.Width > 0 {
		w := a.win.Bounds()
		a.win.SetPosition(b.X+(b.Width-w.Width)/2, b.Y+b.Height+6)
	} else {
		a.win.Center()
	}
	a.win.Show()
}

func (a *app) ready() {
	var err error
	a.tray, err = mygo.NewTray(mygo.TrayOptions{Icon: trayIcon(), IconIsTemplate: true, ToolTip: "AI Limit"})
	if err != nil {
		log.Fatal("tray: ", err)
	}
	a.win = mygo.NewWindow(mygo.WindowOptions{
		Title: "AI Limit", Width: 380, Height: 460, Hidden: true, Frameless: true,
		DisableResize: true, DisableMinimize: true, DisableMaximize: true,
		DisableFullScreen: true, AlwaysOnTop: true, SkipTaskbar: true,
		Content: ui.View(a.view),
	})
	a.win.OnBlur(a.win.Hide)
	a.win.OnClose(func(e *mygo.CloseEvent) {
		if !a.quitting {
			e.PreventDefault()
			a.win.Hide()
		}
	})
	a.tray.OnClick(a.toggle)
	a.rows = a.sched.Snapshot()
	ctx, cancel := context.WithCancel(context.Background())
	mygo.App.OnWillQuit(func(*mygo.QuitEvent) { cancel(); a.tray.Destroy() })
	go a.sched.Run(ctx)
}

func trayIcon() []byte {
	img := image.NewNRGBA(image.Rect(0, 0, 32, 32))
	ink := image.NewUniform(color.NRGBA{A: 255})
	for i, h := range []int{10, 18, 26} {
		x := 5 + i*8
		draw.Draw(img, image.Rect(x, 29-h, x+6, 29), ink, image.Point{}, draw.Src)
	}
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}

// perProvider sets intervals: Claude never more often than claude.MinInterval.
func perProvider(ps []provider.Provider, def time.Duration) map[string]time.Duration {
	m := map[string]time.Duration{"mock:Mock:demo-quota": 30 * time.Second}
	for _, p := range ps {
		if _, ok := p.(*claude.Provider); ok {
			m[p.ID()] = max(def, claude.MinInterval)
		}
	}
	return m
}

func main() {
	cfgPath, err := config.DefaultPath()
	if err != nil {
		log.Print("config: ", err)
	}
	cfg := config.Defaults()
	if cfgPath != "" {
		if cfg, err = config.Load(cfgPath); err != nil {
			log.Print(err)
		}
	}
	// Flags override the config file; launched from Finder there are none.
	interval := flag.Duration("interval", cfg.IntervalDuration(), "chu kỳ làm mới mặc định")
	claudeEmail := flag.String("claude-email", cfg.ClaudeEmail, "email tài khoản Claude")
	codexLabel := flag.String("codex-label", cfg.CodexLabel, "nhãn tài khoản Codex")
	demo := flag.Bool("demo", cfg.Demo, "hiện các provider mock")
	flag.Parse()

	var ps []provider.Provider
	if *demo {
		ps = append(ps,
			&mock.Provider{Name: "Mock", Label: "demo-quota", FailRate: 0.2},
			&mock.Provider{Name: "Mock", Label: "demo-balance", Balance: true},
		)
	}
	ps = append(ps, codex.Rows(&codex.Reader{Label: *codexLabel})...)
	ps = append(ps, claude.Rows(&claude.Client{Email: *claudeEmail})...)
	ps = append(ps,
		&deepseek.Provider{Label: "default"},
		&grokbuild.Provider{Label: "default"},
	)
	a := &app{}
	a.sched = scheduler.New(scheduler.Config{
		Interval:    *interval,
		Timeout:     30 * time.Second,
		PerProvider: perProvider(ps, *interval),
	}, ps, func() {
		if a.win == nil {
			return
		}
		snap := a.sched.Snapshot()
		a.win.Update(func() { a.rows = snap })
	})
	mygo.App.SetName("AI Limit")
	mygo.App.SetActivationPolicy(mygo.ActivationPolicyAccessory)
	mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) { a.quitting = true })
	mygo.App.WhenReady(a.ready)
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
