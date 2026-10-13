// Package view turns scheduler states into the popover's display model:
// one block per service, a bar per window, countdowns and colors. It has
// no UI dependency so both the app and the preview renderer share it.
package view

import (
	"fmt"
	"strings"
	"time"

	"github.com/hoanglong2311/AI-limit/internal/provider"
	"github.com/hoanglong2311/AI-limit/internal/scheduler"
)

// Layout constants (points), used by cmd/ai-limit and cmd/preview.
const (
	WinWidth    = 340 // popover width
	Pad         = 14  // outer padding
	BlockGap    = 14  // space between service blocks
	TitleGap    = 6   // title row to first bar row
	RowGap      = 8   // between window rows in a block
	LabelWidth  = 36  // "5h" / "tuần" column
	PctWidth    = 40  // right-aligned percent column
	ColGap      = 8   // between columns
	BarHeight   = 6
	BarRadius   = 3
	TitleSize   = 13
	BodySize    = 12
	SmallSize   = 11
	SubIndent   = LabelWidth + ColGap // countdown/notes align with the bar
	FooterGap   = 10
	DividerSize = 1
	// BarWidth fills the space between the label and percent columns.
	BarWidth = WinWidth - 2*Pad - LabelWidth - PctWidth - 2*ColGap
)

// Thresholds for bar colors.
const (
	YellowFrom = 50.0
	RedAbove   = 80.0
)

// Bar colors (hex), shared by the app and the preview.
const (
	GreenHex  = "#16a34a"
	YellowHex = "#eab308"
	RedHex    = "#dc2626"
)

// Level is the bar color class.
type Level int

const (
	Green Level = iota
	Yellow
	Red
)

// LevelFor: green <50%, yellow 50–80%, red >80%.
func LevelFor(pct float64) Level {
	switch {
	case pct > RedAbove:
		return Red
	case pct >= YellowFrom:
		return Yellow
	}
	return Green
}

// Line is one window (or balance) row inside a block.
type Line struct {
	Label string // "5h", "tuần", "Số dư"
	State scheduler.State
}

// Block is one service.
type Block struct {
	Title   string // "Claude"
	Account string // email or label; "" to hide
	Lines   []Line
}

var windowSuffix = []string{"5h", "tuần"}

// split "Claude 5h" into ("Claude", "5h").
func split(service string) (string, string) {
	for _, w := range windowSuffix {
		if t, ok := strings.CutSuffix(service, " "+w); ok {
			return t, w
		}
	}
	return service, ""
}

// Group keeps provider order and merges rows of the same service.
func Group(states []scheduler.State) []Block {
	var out []Block
	idx := map[string]int{}
	for _, s := range states {
		title, win := split(s.Service)
		i, ok := idx[title]
		if !ok {
			i = len(out)
			idx[title] = i
			out = append(out, Block{Title: title})
		}
		b := &out[i]
		if b.Account == "" && s.Account != "default" {
			b.Account = s.Account
		}
		if win == "" {
			win = "Số dư"
			if s.HasUsage && s.Usage.Kind == provider.KindQuota {
				win = "Dùng"
			}
		}
		b.Lines = append(b.Lines, Line{Label: win, State: s})
	}
	return out
}

// Countdown formats the time left until t, e.g. "còn 2 giờ 6 phút".
func Countdown(now, t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := t.Sub(now)
	if d <= 0 {
		return "đang reset"
	}
	m := int((d + time.Minute - 1) / time.Minute) // round up: never show "0 phút" early
	days, h, mm := m/(24*60), m/60%24, m%60
	switch {
	case days > 0 && h > 0:
		return fmt.Sprintf("còn %d ngày %d giờ", days, h)
	case days > 0:
		return fmt.Sprintf("còn %d ngày", days)
	case h > 0 && mm > 0:
		return fmt.Sprintf("còn %d giờ %d phút", h, mm)
	case h > 0:
		return fmt.Sprintf("còn %d giờ", h)
	}
	return fmt.Sprintf("còn %d phút", mm)
}

// Clock formats a time as "15:04" today, otherwise "15:04 02/01".
func Clock(now, t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	t, now = t.Local(), now.Local()
	if t.YearDay() == now.YearDay() && t.Year() == now.Year() {
		return t.Format("15:04")
	}
	return t.Format("15:04 02/01")
}

// Percent is the fixed-width percent text, e.g. "83%".
func Percent(p float64) string { return fmt.Sprintf("%.0f%%", p) }

// Money formats a balance.
func Money(v float64, cur string) string {
	switch cur {
	case "USD", "":
		return fmt.Sprintf("$%.2f", v)
	case "CNY":
		return fmt.Sprintf("¥%.2f", v)
	}
	return fmt.Sprintf("%.2f %s", v, cur)
}

// Note is the secondary line under a row: the countdown when fresh, the
// key instructions when the key is missing, or the error's own time.
func Note(now time.Time, s scheduler.State) string {
	switch s.Status {
	case scheduler.StatusNoKey:
		return s.Err
	case scheduler.StatusError:
		return "lỗi lúc " + Clock(now, s.LastAttempt)
	}
	if s.Fresh() && s.Usage.Kind == provider.KindQuota {
		return Countdown(now, s.Usage.ResetAt)
	}
	return ""
}

// LastUpdated is the single footer line: the newest successful fetch.
func LastUpdated(now time.Time, states []scheduler.State) string {
	var last time.Time
	for _, s := range states {
		if s.LastSuccess.After(last) {
			last = s.LastSuccess
		}
	}
	if last.IsZero() {
		return "Chưa cập nhật lần nào"
	}
	return "Cập nhật lúc " + Clock(now, last)
}
