package view

import (
	"testing"
	"time"

	"github.com/hoanglong2311/AI-limit/internal/scheduler"
)

func TestCountdown(t *testing.T) {
	n := time.Date(2026, 10, 13, 10, 0, 0, 0, time.UTC)
	for d, want := range map[time.Duration]string{
		2*time.Hour + 6*time.Minute:               "còn 2 giờ 6 phút",
		2*time.Hour + 5*time.Minute + time.Second: "còn 2 giờ 6 phút",
		3 * time.Hour:                "còn 3 giờ",
		45 * time.Minute:             "còn 45 phút",
		30 * time.Second:             "còn 1 phút",
		3*24*time.Hour + 4*time.Hour: "còn 3 ngày 4 giờ",
		-time.Minute:                 "đang reset",
	} {
		if got := Countdown(n, n.Add(d)); got != want {
			t.Errorf("%v: %q want %q", d, got, want)
		}
	}
}

func TestLevel(t *testing.T) {
	if LevelFor(49.9) != Green || LevelFor(50) != Yellow || LevelFor(80) != Yellow || LevelFor(80.1) != Red {
		t.Fatal("levels")
	}
}

func TestGroup(t *testing.T) {
	b := Group([]scheduler.State{
		{Service: "Claude 5h", Account: "a@b.c"}, {Service: "Claude tuần", Account: "a@b.c"},
		{Service: "Codex 5h"}, {Service: "Codex tuần"}, {Service: "DeepSeek", Account: "default"},
	})
	if len(b) != 3 || b[0].Title != "Claude" || len(b[0].Lines) != 2 || b[0].Lines[1].Label != "tuần" ||
		b[2].Account != "" || b[2].Lines[0].Label != "Số dư" {
		t.Fatalf("%+v", b)
	}
}

func TestLastUpdated(t *testing.T) {
	n := time.Date(2026, 10, 13, 10, 0, 0, 0, time.Local)
	s := []scheduler.State{{LastSuccess: n.Add(-time.Hour)}, {LastSuccess: n.Add(-time.Minute)}}
	if got := LastUpdated(n, s); got != "Cập nhật lúc 09:59" {
		t.Fatal(got)
	}
}

func TestUpdateLabel(t *testing.T) {
	if UpdateLabel("v0.1.4") != "Cập nhật lên v0.1.4" || UpdateLabel("0.2.0") != "Cập nhật lên v0.2.0" {
		t.Fatal(UpdateLabel("v0.1.4"))
	}
}

func TestFilterEnabled(t *testing.T) {
	rows := []scheduler.State{
		{ID: "a", Service: "Claude 5h"}, {ID: "b", Service: "Claude tuần"},
		{ID: "c", Service: "Codex 5h"}, {ID: "d", Service: "DeepSeek"},
		{ID: "e", Service: "Grok"}, {ID: "f", Service: "Mock"},
	}
	on := map[string]bool{"claude": false, "codex": true, "deepseek": true, "grok": false}
	got := FilterEnabled(rows, func(k string) bool { return on[k] })
	var ids string
	for _, s := range got {
		ids += s.ID
	}
	if ids != "cdf" {
		t.Fatalf("got %q", ids)
	}
	if n := len(FilterEnabled(rows, func(string) bool { return false })); n != 1 {
		t.Fatalf("all off keeps only mock, got %d", n)
	}
}

func TestCooldownText(t *testing.T) {
	cases := map[time.Duration]string{
		0:                           "",
		-time.Second:                "",
		time.Second:                 "làm mới được sau 1 phút",
		3 * time.Minute:             "làm mới được sau 3 phút",
		3*time.Minute + time.Second: "làm mới được sau 4 phút",
		5 * time.Minute:             "làm mới được sau 5 phút",
	}
	for d, want := range cases {
		if got := CooldownText(d); got != want {
			t.Errorf("%v: %q want %q", d, got, want)
		}
	}
}

func TestSetupBlock(t *testing.T) {
	rows := []scheduler.State{
		{ID: "1", Service: "Claude 5h", Status: scheduler.StatusNoKey, Err: "hint"},
		{ID: "2", Service: "Claude tuần", Status: scheduler.StatusNoKey, Err: "hint"},
		{ID: "3", Service: "Codex 5h", Status: scheduler.StatusNoKey, Err: "x"},
		{ID: "4", Service: "Codex tuần", Status: scheduler.StatusReset},
	}
	b := Group(rows)
	if b[0].Setup != "hint" || b[1].Setup != "" {
		t.Fatalf("%+v", b)
	}
	if rows[3].Label() != "đã reset, chưa dùng" {
		t.Fatal(rows[3].Label())
	}
}

func TestGrokErrorNoteHasUsageHint(t *testing.T) {
	now := time.Date(2026, 10, 13, 12, 0, 0, 0, time.Local)
	s := scheduler.State{Service: "Grok tuần", Status: scheduler.StatusError, LastAttempt: now}
	if got := Note(now, s); got != "lỗi lúc 12:00 · "+GrokUsageHint || s.Label() != "lỗi" {
		t.Fatal(got)
	}
	s.Service = "Claude 5h"
	if got := Note(now, s); got != "lỗi lúc 12:00" {
		t.Fatal(got)
	}
}

func TestGrokNeverBalanceLabel(t *testing.T) {
	rows := []scheduler.State{
		{ID: "g", Service: "Grok tuần", Status: scheduler.StatusError},
		{ID: "g2", Service: "Grok", Status: scheduler.StatusPending},
		{ID: "d", Service: "DeepSeek", Status: scheduler.StatusNoKey},
	}
	b := Group(rows)
	if b[0].Lines[0].Label != "tuần" || b[0].Lines[1].Label != "tuần" {
		t.Fatalf("grok labels %+v", b[0].Lines)
	}
	if b[1].Lines[0].Label != "Số dư" {
		t.Fatal(b[1].Lines[0].Label)
	}
}
