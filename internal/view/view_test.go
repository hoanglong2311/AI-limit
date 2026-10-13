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
