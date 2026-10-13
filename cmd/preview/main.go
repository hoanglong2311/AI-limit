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
	"github.com/hoanglong2311/AI-limit/internal/providers/deepseek"
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
		{ID: "cw", Service: "Codex tuần", Account: "ChatGPT", Status: scheduler.StatusReset},
		{ID: "ds", Service: "DeepSeek", Account: "default", Status: scheduler.StatusNoKey,
			Err: deepseek.AddKeyHint},
		{ID: "gk", Service: "Grok", Status: scheduler.StatusNoSource},
	}
	on := map[string]bool{"claude": true, "codex": true, "deepseek": true, "grok": false}
	rows = view.FilterEnabled(rows, func(k string) bool { return on[k] })
	settingsPage := len(os.Args) > 1 && os.Args[1] == "-settings"
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
.foot{display:flex;align-items:center;gap:%s}.upd{font-size:11px;color:#2563eb}
.btn{font-size:12px;padding:2px 10px;border-radius:5px;background:#fff;box-shadow:0 0 0 .5px rgba(0,0,0,.25),0 1px 1px rgba(0,0,0,.08)}
.btn.upd{font-size:11px;color:#2563eb}.btn.s{font-size:11px}.btn.dis{color:#9ca3af}
.sw{width:30px;height:18px;border-radius:9px;background:#d1d5db;position:relative}.sw.on{background:#2563eb}
.sw:after{content:"";position:absolute;top:2px;left:2px;width:14px;height:14px;border-radius:50%%;background:#fff;box-shadow:0 1px 2px rgba(0,0,0,.3)}.sw.on:after{left:14px}
.inp{font-size:12px;padding:4px 8px;border-radius:5px;background:#fff;box-shadow:0 0 0 .5px rgba(0,0,0,.25);letter-spacing:2px}
.ok{color:#16a34a}
</style><div class=win>`,
		px(view.WinWidth), px(view.Pad), px(view.FooterGap), px(view.BlockGap), px(view.TitleGap), px(view.ColGap),
		px(view.TitleSize), px(view.SmallSize), px(view.RowGap), px(view.ColGap), px(view.BodySize), px(view.LabelWidth),
		px(view.BarWidth), px(view.BarHeight), px(view.BarRadius), px(view.BarRadius), px(view.PctWidth), px(view.SubIndent), px(view.ColGap))
	if settingsPage {
		b.WriteString(`<b style="font-size:13px">Cài đặt</b><div class=lines><span class="muted small">Dịch vụ hiển thị</span>`)
		for _, k := range []struct{ k, n string }{{"claude", "Claude"}, {"codex", "Codex"}, {"deepseek", "DeepSeek"}, {"grok", "Grok"}} {
			cls := "sw"
			if on[k.k] {
				cls += " on"
			}
			note := ""
			if k.k == "grok" {
				note = `<div class="muted small">chưa có nguồn số liệu</div>`
			}
			fmt.Fprintf(&b, `<div class=row><div style="flex:1">%s%s</div><div class="%s"></div></div>`, e(k.n), note, cls)
		}
		b.WriteString(`</div><hr><div class=lines><span style="font-size:12px">API key DeepSeek</span>` +
			`<div class=inp>••••••••••••••••••••</div><div class=row><span class="btn s">Lưu vào Keychain</span><span class="btn s">Kiểm tra</span></div>` +
			`<span class="small ok">Key hợp lệ</span><span class="muted small">Key chỉ lưu trong macOS Keychain (mục ai-limit.deepseek).</span></div>` +
			`<div class=foot style="height:20px"></div><div class=foot><span style="flex:1"></span><span class=btn>Xong</span></div></div>`)
		os.Stdout.WriteString(b.String())
		return
	}
	b.WriteString(`<div class=blocks>`)
	for _, blk := range view.Group(rows) {
		fmt.Fprintf(&b, `<div class=blk><div class=title><b>%s</b>`, e(blk.Title))
		if blk.Account != "" {
			fmt.Fprintf(&b, `<span class="muted small">%s</span>`, e(blk.Account))
		}
		b.WriteString(`</div>`)
		if blk.Title == "Claude" {
			fmt.Fprintf(&b, `<span class="muted small">%s</span>`, e(view.CooldownText(3*time.Minute+10*time.Second)))
		}
		if blk.Setup != "" {
			fmt.Fprintf(&b, `<span class="muted small">%s</span><div><span class="btn s">Mở Cài đặt</span></div></div>`, e(blk.Setup))
			continue
		}
		b.WriteString(`<div class=lines>`)
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
	fmt.Fprintf(&b, `</div><hr><div class=foot><span class="muted small" style="flex:1">%s</span>`, e(view.LastUpdated(now, rows)))
	b.WriteString(`<span class="btn s">Làm mới</span></div>`)
	b.WriteString(`<div class=foot><span style="flex:1"></span><span class=btn>Cài đặt</span><span class=btn>Thoát</span></div></div>`)
	os.Stdout.WriteString(b.String())
}
