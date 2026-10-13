// Command preview writes an HTML mock of the popover with demo data, built
// from the same view model and layout constants as the app. Render it with
// a headless browser to get a PNG:
//
//	go run ./cmd/preview > p.html && google-chrome --headless --screenshot=p.png p.html
package main

import (
	"fmt"
	"html"
	"os"
	"strings"
	"time"

	"github.com/hoanglong2311/AI-limit/internal/provider"
	"github.com/hoanglong2311/AI-limit/internal/scheduler"
	"github.com/hoanglong2311/AI-limit/internal/view"
)

var colors = map[view.Level]string{view.Green: view.GreenHex, view.Yellow: view.YellowHex, view.Red: view.RedHex}

func quota(svc, acct string, pct float64, reset time.Duration, now time.Time) scheduler.State {
	return scheduler.State{ID: svc, Service: svc, Account: acct, Status: scheduler.StatusOK, HasUsage: true,
		LastSuccess: now.Add(-2 * time.Minute),
		Usage:       provider.Usage{Kind: provider.KindQuota, UsedPercent: pct, ResetAt: now.Add(reset)}}
}

func main() {
	now := time.Date(2026, 10, 13, 11, 30, 0, 0, time.Local)
	rows := []scheduler.State{
		quota("Claude 5h", "demo@ai-limit.app", 34, 2*time.Hour+6*time.Minute, now),
		quota("Claude tuần", "demo@ai-limit.app", 83, 3*24*time.Hour+4*time.Hour, now),
		quota("Codex 5h", "ChatGPT", 62, 47*time.Minute, now),
		{ID: "cw", Service: "Codex tuần", Account: "ChatGPT", Status: scheduler.StatusStale},
		{ID: "ds", Service: "DeepSeek", Account: "default", Status: scheduler.StatusNoKey,
			Err: "Thêm key vào Keychain: mục ai-limit.deepseek"},
	}
	var b strings.Builder
	e := html.EscapeString
	px := func(v float64) string { return fmt.Sprintf("%gpx", v) }
	fmt.Fprintf(&b, `<!doctype html><meta charset=utf-8><style>
body{margin:0;padding:24px;background:#c9d3e0;font-family:-apple-system,"Inter","Noto Sans",sans-serif;color:#1f2328}
.win{width:%s;box-sizing:border-box;padding:%s;background:#f6f6f6;border-radius:10px;box-shadow:0 8px 28px rgba(0,0,0,.25),0 0 0 .5px rgba(0,0,0,.2);display:flex;flex-direction:column;gap:%s}
.blocks{display:flex;flex-direction:column;gap:%s}.blk{display:flex;flex-direction:column;gap:%s}
.title{display:flex;gap:%s;align-items:baseline}.title b{font-size:%s;font-weight:600;flex:1}
.muted{color:#6b7280}.small{font-size:%s}.lines{display:flex;flex-direction:column;gap:%s}
.row{display:flex;gap:%s;align-items:center;font-size:%s}.lbl{width:%s}
.track{width:%s;height:%s;border-radius:%s;background:rgba(107,114,128,.2);overflow:hidden}.fill{height:100%%;border-radius:%s}
.pct{width:%s;text-align:right;font-variant-numeric:tabular-nums}.note{padding-left:%s;margin-top:2px;font-variant-numeric:tabular-nums;word-break:break-all}
.err{color:#dc2626}hr{border:0;border-top:1px solid rgba(0,0,0,.1);margin:0}
.foot{display:flex;align-items:center;gap:%s}.upd{display:block;text-align:center;font-size:13px;font-weight:500;color:#fff;background:#2563eb;border-radius:6px;padding:5px 0;box-shadow:0 1px 1px rgba(0,0,0,.12)}
.btn{font-size:12px;padding:2px 10px;border-radius:5px;background:#fff;box-shadow:0 0 0 .5px rgba(0,0,0,.25),0 1px 1px rgba(0,0,0,.08)}
</style><div class=win><div class=blocks>`,
		px(view.WinWidth), px(view.Pad), px(view.FooterGap), px(view.BlockGap), px(view.TitleGap), px(view.ColGap),
		px(view.TitleSize), px(view.SmallSize), px(view.RowGap), px(view.ColGap), px(view.BodySize), px(view.LabelWidth),
		px(view.BarWidth), px(view.BarHeight), px(view.BarRadius), px(view.BarRadius), px(view.PctWidth), px(view.SubIndent), px(view.ColGap))
	for _, blk := range view.Group(rows) {
		fmt.Fprintf(&b, `<div class=blk><div class=title><b>%s</b>`, e(blk.Title))
		if blk.Account != "" {
			fmt.Fprintf(&b, `<span class="muted small">%s</span>`, e(blk.Account))
		}
		b.WriteString(`</div><div class=lines>`)
		for _, l := range blk.Lines {
			s := l.State
			fmt.Fprintf(&b, `<div><div class=row><span class="lbl muted">%s</span>`, e(l.Label))
			switch {
			case !s.Fresh():
				cls := "muted"
				if s.Status == scheduler.StatusError {
					cls = "err"
				}
				fmt.Fprintf(&b, `<span class=%s>%s</span>`, cls, e(s.Label()))
			case s.Usage.Kind == provider.KindBalance:
				fmt.Fprintf(&b, `<span style="flex:1"></span><b class=pct>%s</b>`, e(view.Money(s.Usage.Balance, s.Usage.Currency)))
			default:
				p := s.Usage.UsedPercent
				fmt.Fprintf(&b, `<div class=track><div class=fill style="width:%.1f%%;background:%s"></div></div><span class=pct>%s</span>`,
					p, colors[view.LevelFor(p)], e(view.Percent(p)))
			}
			b.WriteString(`</div>`)
			if n := view.Note(now, s); n != "" {
				fmt.Fprintf(&b, `<div class="note muted small">%s</div>`, e(n))
			}
			b.WriteString(`</div>`)
		}
		b.WriteString(`</div></div>`)
	}
	fmt.Fprintf(&b, `</div><hr><span class=upd>%s</span><div class=foot>`, e(view.UpdateLabel("v0.1.4")))
	fmt.Fprintf(&b, `<span class="muted small" style="flex:1">%s</span><span class=btn>Thoát</span></div></div>`,
		e(view.LastUpdated(now, rows)))
	os.Stdout.WriteString(b.String())
}
