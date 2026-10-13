package codex

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hoanglong2311/AI-limit/internal/provider"
)

// copyHome copies testdata/home into a temp dir and sets mtimes explicitly,
// since git does not preserve them.
func copyHome(t *testing.T, mtimes map[string]time.Time) string {
	t.Helper()
	dst := t.TempDir()
	src := "testdata/home"
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		out := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(out, 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if err := os.WriteFile(out, b, 0o644); err != nil {
			return err
		}
		for k, m := range mtimes {
			if strings.Contains(d.Name(), k) {
				return os.Chtimes(out, m, m)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return dst
}

func ts(s string) time.Time { t, _ := time.Parse(time.RFC3339, s); return t }

func defaultHome(t *testing.T) string {
	return copyHome(t, map[string]time.Time{
		"new": ts("2026-10-13T01:06:00Z"),
		"old": ts("2026-10-12T20:00:00Z"),
	})
}

func fetch(t *testing.T, home string, now time.Time) (provider.Usage, error, provider.Usage, error) {
	r := &Reader{Home: home, Label: "default", Now: func() time.Time { return now }}
	rows := Rows(r)
	a, ea := rows[0].Fetch(context.Background())
	b, eb := rows[1].Fetch(context.Background())
	return a, ea, b, eb
}

func TestNewestNonNullWins(t *testing.T) {
	u5, e5, uw, ew := fetch(t, defaultHome(t), ts("2026-10-13T02:00:00Z"))
	if e5 != nil || u5.UsedPercent != 85 || !u5.ResetAt.Equal(ts("2026-10-13T05:00:00Z")) {
		t.Fatalf("5h %+v %v", u5, e5)
	}
	if ew != nil || uw.UsedPercent != 24 || !uw.ResetAt.Equal(ts("2026-10-17T01:20:00Z")) {
		t.Fatalf("week %+v %v", uw, ew)
	}
}

func TestRowsLabels(t *testing.T) {
	rows := Rows(&Reader{Label: "me"})
	if rows[0].Service() != "Codex 5h" || rows[1].Service() != "Codex tuần" || rows[0].ID() == rows[1].ID() {
		t.Fatal("labels")
	}
}

func TestResetPassedIsStale(t *testing.T) {
	u5, e5, uw, ew := fetch(t, defaultHome(t), ts("2026-10-13T05:30:00Z"))
	if !errors.Is(e5, provider.ErrStale) {
		t.Fatalf("5h should be stale: %+v %v", u5, e5)
	}
	if ew != nil || uw.UsedPercent != 24 {
		t.Fatalf("week should still be fresh: %+v %v", uw, ew)
	}
}

func TestOlderThanWindowIsStale(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "sessions", "2026", "10", "01")
	os.MkdirAll(dir, 0o755)
	// Reset far in the future but observed 8 days ago: weekly must be stale.
	os.WriteFile(filepath.Join(dir, "rollout-x.jsonl"), []byte(
		`{"timestamp":"2026-10-05T00:00:00Z","type":"event_msg","payload":{"type":"token_count","rate_limits":{"primary":{"used_percent":1,"window_minutes":300,"resets_at":1893456000},"secondary":{"used_percent":2,"window_minutes":10080,"resets_at":1893456000}}}}`+"\n"), 0o644)
	_, e5, _, ew := fetch(t, home, ts("2026-10-13T02:00:00Z"))
	if !errors.Is(e5, provider.ErrStale) || !errors.Is(ew, provider.ErrStale) {
		t.Fatalf("want stale: %v %v", e5, ew)
	}
}

func TestLegacyResetsInSeconds(t *testing.T) {
	// Make the old file newest, so its legacy line is used.
	home := copyHome(t, map[string]time.Time{
		"new": ts("2026-10-01T00:00:00Z"),
		"old": ts("2026-10-12T20:00:00Z"),
	})
	os.Remove(filepath.Join(home, "sessions/2026/10/13/rollout-2026-10-13T00-30-00-new.jsonl"))
	u5, e5, uw, ew := fetch(t, home, ts("2026-10-12T20:30:00Z"))
	if e5 != nil || u5.UsedPercent != 50 || !u5.ResetAt.Equal(ts("2026-10-12T21:00:00Z")) {
		t.Fatalf("5h %+v %v", u5, e5)
	}
	if ew != nil || !uw.ResetAt.Equal(ts("2026-10-13T20:00:00Z")) {
		t.Fatalf("week %+v %v", uw, ew)
	}
}

func TestNoSessionsDir(t *testing.T) {
	_, e5, _, _ := fetch(t, t.TempDir(), time.Now())
	if !errors.Is(e5, provider.ErrNoKey) || e5.Error() != SetupHint {
		t.Fatalf("want setup hint, got %v", e5)
	}
}

func TestOnlyNullRateLimits(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "sessions", "2026", "10", "13")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "rollout-n.jsonl"), []byte(
		`{"timestamp":"2026-10-13T01:00:00Z","type":"event_msg","payload":{"type":"token_count","rate_limits":null}}`+"\n"), 0o644)
	_, e5, _, ew := fetch(t, home, ts("2026-10-13T02:00:00Z"))
	if !errors.Is(e5, provider.ErrStale) || !errors.Is(ew, provider.ErrStale) {
		t.Fatalf("want stale: %v %v", e5, ew)
	}
}

func TestMinIntervalCaches(t *testing.T) {
	home := defaultHome(t)
	cur := ts("2026-10-13T02:00:00Z")
	r := &Reader{Home: home, Now: func() time.Time { return cur }}
	p := Rows(r)[0]
	if u, err := p.Fetch(context.Background()); err != nil || u.UsedPercent != 85 {
		t.Fatal(u, err)
	}
	f := filepath.Join(home, "sessions/2026/10/13/rollout-2026-10-13T00-30-00-new.jsonl")
	add := `{"timestamp":"2026-10-13T02:00:00Z","type":"event_msg","payload":{"type":"token_count","rate_limits":{"primary":{"used_percent":90,"window_minutes":300,"resets_at":1791867600},"secondary":{"used_percent":25,"window_minutes":10080,"resets_at":1792200000}}}}` + "\n"
	fd, _ := os.OpenFile(f, os.O_APPEND|os.O_WRONLY, 0)
	fd.WriteString("\n" + add)
	fd.Close()
	cur = cur.Add(30 * time.Second)
	if u, _ := p.Fetch(context.Background()); u.UsedPercent != 85 {
		t.Fatalf("should be cached, got %v", u.UsedPercent)
	}
	cur = cur.Add(time.Minute)
	if u, _ := p.Fetch(context.Background()); u.UsedPercent != 90 {
		t.Fatalf("should rescan, got %v", u.UsedPercent)
	}
}

func TestOnlyLastSevenDayFolders(t *testing.T) {
	old := time.Local
	time.Local = time.UTC
	defer func() { time.Local = old }()
	now := ts("2026-10-13T02:00:00Z")
	dirs := DayDirs("/r", now)
	if len(dirs) != Days+1 || dirs[0] != filepath.Join("/r", "2026", "10", "13") || dirs[Days] != filepath.Join("/r", "2026", "10", "06") {
		t.Fatalf("dirs %v", dirs)
	}
	home := t.TempDir()
	line := func(p float64) string {
		return fmt.Sprintf(`{"timestamp":"2026-10-13T01:00:00Z","type":"event_msg","payload":{"type":"token_count","rate_limits":{"primary":{"used_percent":%v,"window_minutes":300,"resets_at":1791867600}}}}`+"\n", p)
	}
	write := func(rel string, p float64, mod time.Time) {
		f := filepath.Join(home, "sessions", rel)
		os.MkdirAll(filepath.Dir(f), 0o755)
		os.WriteFile(f, []byte(line(p)), 0o644)
		os.Chtimes(f, mod, mod)
	}
	// Outside the range, though newest by mtime: must be ignored.
	write("2026/10/05/rollout-out.jsonl", 99, now)
	write("2025/01/01/rollout-ancient.jsonl", 98, now)
	write("misc/rollout-stray.jsonl", 97, now)
	// Inside the range.
	write("2026/10/06/rollout-in.jsonl", 40, now.Add(-time.Hour))
	u5, e5, _, _ := fetch(t, home, now)
	if e5 != nil || u5.UsedPercent != 40 {
		t.Fatalf("want 40 from in-range folder, got %+v %v", u5, e5)
	}
}
