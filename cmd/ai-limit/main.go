// Command ai-limit is a macOS menu bar app showing AI usage limits.
package main

import (
	"bytes"
	"context"
	"flag"
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
	"github.com/hoanglong2311/AI-limit/internal/providers/mock"
	"github.com/hoanglong2311/AI-limit/internal/scheduler"
	"github.com/hoanglong2311/AI-limit/internal/view"
)

type app struct {
	win      *mygo.Window
	tray     *mygo.Tray
	sched    *scheduler.Scheduler
	rows     []scheduler.State
	quitting bool
	upd      updater
}

// barColor maps view levels to theme colors.
func barColor(t *ui.Theme, l view.Level) ui.Color {
	switch l {
	case view.Red:
		return ui.Hex(view.RedHex)
	case view.Yellow:
		return ui.Hex(view.YellowHex)
	}
	return ui.Hex(view.GreenHex)
}

func (a *app) line(c *ui.Context, now time.Time, l view.Line) {
	t := c.Theme()
	s := l.State
	ui.Column(c.Key(s.ID)).Gap(2).Children(func() {
		ui.Row(c).Gap(view.ColGap).AlignItems(ui.Center).Children(func() {
			ui.Text(c, l.Label).FontSize(view.BodySize).Width(view.LabelWidth).TextColor(t.TextMuted)
			switch {
			case !s.Fresh():
				lbl := ui.Text(c, s.Label()).FontSize(view.BodySize).Grow(1)
				if s.Status == scheduler.StatusError {
					lbl.TextColor(t.Danger)
				} else {
					lbl.TextColor(t.TextMuted)
				}
			case s.Usage.Kind == provider.KindBalance:
				ui.Spacer(c)
				ui.Text(c, view.Money(s.Usage.Balance, s.Usage.Currency)).FontSize(view.BodySize).
					FontWeight(600).FontFeatures("tnum")
			default:
				p := min(max(s.Usage.UsedPercent, 0), 100)
				ui.Box(c).Width(view.BarWidth).Height(view.BarHeight).Radius(view.BarRadius).
					Background(t.TextMuted.Alpha(0.2)).Children(func() {
					ui.Box(c).Width(float32(p / 100 * view.BarWidth)).Height(view.BarHeight).
						Radius(view.BarRadius).Background(barColor(t, view.LevelFor(p)))
				})
				ui.Text(c, view.Percent(s.Usage.UsedPercent)).FontSize(view.BodySize).
					Width(view.PctWidth).TextAlign(ui.End).FontFeatures("tnum")
			}
		})
		if n := view.Note(now, s); n != "" {
			ui.Text(c, n).FontSize(view.SmallSize).TextColor(t.TextMuted).
				FontFeatures("tnum").Padding(0, 0, 0, view.SubIndent)
		}
	})
}

func (a *app) block(c *ui.Context, now time.Time, b view.Block) {
	t := c.Theme()
	ui.Column(c.Key(b.Title)).Gap(view.TitleGap).Children(func() {
		ui.Row(c).Gap(view.ColGap).Children(func() {
			ui.Text(c, b.Title).FontSize(view.TitleSize).FontWeight(600).Grow(1)
			if b.Account != "" {
				ui.Text(c, b.Account).FontSize(view.SmallSize).TextColor(t.TextMuted)
			}
		})
		ui.Column(c).Gap(view.RowGap).Children(func() {
			for _, l := range b.Lines {
				a.line(c, now, l)
			}
		})
	})
}

func (a *app) view(c *ui.Context) {
	if c.Shortcut(0, ui.KeyEscape) {
		a.win.Hide()
	}
	c.OnShortcut(ui.Cmd, ui.KeyQ, mygo.App.Quit)
	now := time.Now()
	t := c.Theme()
	ui.Column(c).Fill().Padding(view.Pad).Gap(view.FooterGap).Children(func() {
		ui.Scroll(c).Grow(1).Children(func() {
			ui.Column(c).Gap(view.BlockGap).Children(func() {
				for _, b := range view.Group(a.rows) {
					a.block(c, now, b)
				}
			})
		})
		ui.Divider(c)
		a.updateRow(c)
		ui.Row(c).Gap(view.ColGap).AlignItems(ui.Center).Children(func() {
			ui.Text(c, view.LastUpdated(now, a.rows)).FontSize(view.SmallSize).
				TextColor(t.TextMuted).FontFeatures("tnum").Grow(1)
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
		Title: "AI Limit", Width: view.WinWidth, Height: 440, Hidden: true, Frameless: true,
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
	go a.checkLoop(ctx)
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
