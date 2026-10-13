// Package codex reports Codex (ChatGPT plan) rate limits from the local
// rollout logs that Codex CLI writes. It is read-only and offline: no
// network, no token. ~/.codex/auth.json is never opened.
//
// After each turn Codex appends to
// $CODEX_HOME/sessions/YYYY/MM/DD/rollout-*.jsonl (CODEX_HOME defaults to
// ~/.codex) a line like:
//
//	{"timestamp":"2026-10-13T01:00:00.000Z","type":"event_msg",
//	 "payload":{"type":"token_count","rate_limits":{
//	   "primary":  {"used_percent":42,"window_minutes":300,  "resets_at":1791495755},
//	   "secondary":{"used_percent":17,"window_minutes":10080,"resets_at":1791833135}}}}
//
// Older CLI versions wrote resets_in_seconds (relative to the line's
// timestamp) instead of resets_at. Some versions write "rate_limits": null;
// those lines are skipped.
package codex

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/hoanglong2311/AI-limit/internal/provider"
)

const (
	FiveHourMinutes = 300
	WeekMinutes     = 10080
	// MaxFiles is how many of the newest rollout files are scanned.
	MaxFiles = 20
	// TailBytes is how much of the end of each file is read.
	TailBytes = 2 << 20
	// MinInterval between two scans of the log directory.
	MinInterval = time.Minute
)

// Days is how many local-date folders before today are scanned, in
// addition to today: 7 covers the whole weekly window.
const Days = 7

// DayDirs returns root/YYYY/MM/DD for today and the Days previous days,
// in local time (Codex names its folders by local date), newest first.
func DayDirs(root string, now time.Time) []string {
	now = now.In(time.Local)
	out := make([]string, 0, Days+1)
	for i := 0; i <= Days; i++ {
		d := now.AddDate(0, 0, -i)
		out = append(out, filepath.Join(root, d.Format("2006"), d.Format("01"), d.Format("02")))
	}
	return out
}

// Window selects a row.
type Window int

const (
	FiveHour Window = iota
	Weekly
)

type rawWindow struct {
	UsedPercent     *float64 `json:"used_percent"`
	WindowMinutes   int      `json:"window_minutes"`
	ResetsAt        int64    `json:"resets_at"`
	ResetsInSeconds *int64   `json:"resets_in_seconds"`
}

type line struct {
	Timestamp string `json:"timestamp"`
	Type      string `json:"type"`
	Payload   struct {
		Type       string `json:"type"`
		RateLimits *struct {
			Primary   *rawWindow `json:"primary"`
			Secondary *rawWindow `json:"secondary"`
		} `json:"rate_limits"`
	} `json:"payload"`
}

// Snap is one parsed window.
type Snap struct {
	UsedPercent   float64
	WindowMinutes int
	ResetAt       time.Time
	ObservedAt    time.Time // the log line's timestamp (or file mtime)
}

// Snapshot is the newest data found.
type Snapshot struct {
	FiveHour, Weekly *Snap
	File             string
}

func toSnap(w *rawWindow, observed time.Time) *Snap {
	if w == nil || w.UsedPercent == nil {
		return nil
	}
	s := &Snap{UsedPercent: *w.UsedPercent, WindowMinutes: w.WindowMinutes, ObservedAt: observed}
	switch {
	case w.ResetsAt > 0:
		s.ResetAt = time.Unix(w.ResetsAt, 0)
	case w.ResetsInSeconds != nil && !observed.IsZero():
		s.ResetAt = observed.Add(time.Duration(*w.ResetsInSeconds) * time.Second)
	}
	return s
}

// ParseTail returns the newest rate-limit snapshot in r, or nil. fallback
// is used as the observation time for lines without a timestamp.
func ParseTail(r io.Reader, fallback time.Time) (fh, wk *Snap) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 16<<20)
	for sc.Scan() {
		b := sc.Bytes()
		if !bytes.Contains(b, []byte(`"rate_limits"`)) {
			continue
		}
		var l line
		if json.Unmarshal(b, &l) != nil || l.Payload.Type != "token_count" || l.Payload.RateLimits == nil {
			continue
		}
		obs := fallback
		if t, err := time.Parse(time.RFC3339Nano, l.Timestamp); err == nil {
			obs = t
		}
		var f, w *Snap
		for i, rw := range []*rawWindow{l.Payload.RateLimits.Primary, l.Payload.RateLimits.Secondary} {
			s := toSnap(rw, obs)
			if s == nil {
				continue
			}
			switch {
			case s.WindowMinutes == FiveHourMinutes, s.WindowMinutes == 0 && i == 0:
				f = s
			case s.WindowMinutes == WeekMinutes, s.WindowMinutes == 0 && i == 1:
				w = s
			}
		}
		if f == nil && w == nil {
			continue
		}
		// Later lines are newer; a line replaces both windows so the two
		// rows stay consistent, but keep an older window if the newer line
		// lacks it.
		if f != nil {
			fh = f
		}
		if w != nil {
			wk = w
		}
	}
	return fh, wk
}

// Reader scans the log directory, shared by the two rows.
type Reader struct {
	Home        string // CODEX_HOME; default $CODEX_HOME or ~/.codex
	Label       string
	Now         func() time.Time
	MinInterval time.Duration

	mu   sync.Mutex
	last time.Time
	snap Snapshot
	err  error
}

func (r *Reader) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func (r *Reader) home() (string, error) {
	if r.Home != "" {
		return r.Home, nil
	}
	if h := os.Getenv("CODEX_HOME"); h != "" {
		return h, nil
	}
	h, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, ".codex"), nil
}

func (r *Reader) get(ctx context.Context) (Snapshot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	min := r.MinInterval
	if min <= 0 {
		min = MinInterval
	}
	if !r.last.IsZero() && r.now().Sub(r.last) < min {
		return r.snap, r.err
	}
	s, err := r.scan(ctx)
	if ctx.Err() == nil {
		r.last, r.snap, r.err = r.now(), s, err
	}
	return s, err
}

type fileInfo struct {
	path string
	mod  time.Time
}

func (r *Reader) scan(ctx context.Context) (Snapshot, error) {
	home, err := r.home()
	if err != nil {
		return Snapshot{}, err
	}
	root := filepath.Join(home, "sessions")
	if _, err := os.Stat(root); err != nil {
		return Snapshot{}, provider.NoKey(SetupHint)
	}
	var files []fileInfo
	for _, dir := range DayDirs(root, r.now()) {
		if ctx.Err() != nil {
			return Snapshot{}, ctx.Err()
		}
		ents, err := os.ReadDir(dir)
		if err != nil {
			continue // folder for that day does not exist
		}
		for _, e := range ents {
			n := e.Name()
			if e.IsDir() || !strings.HasPrefix(n, "rollout-") || !strings.HasSuffix(n, ".jsonl") {
				continue
			}
			if fi, err := e.Info(); err == nil {
				files = append(files, fileInfo{filepath.Join(dir, n), fi.ModTime()})
			}
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mod.After(files[j].mod) })
	if len(files) > MaxFiles {
		files = files[:MaxFiles]
	}
	var out Snapshot
	for _, f := range files {
		fh, wk, err := readFile(f)
		if err != nil {
			continue
		}
		if out.FiveHour == nil && fh != nil {
			out.FiveHour, out.File = fh, f.path
		}
		if out.Weekly == nil && wk != nil {
			out.Weekly = wk
			if out.File == "" {
				out.File = f.path
			}
		}
		if out.FiveHour != nil && out.Weekly != nil {
			break
		}
	}
	if out.FiveHour == nil && out.Weekly == nil {
		return out, provider.Stale("Codex: log chưa có rate_limits")
	}
	return out, nil
}

func readFile(f fileInfo) (fh, wk *Snap, err error) {
	fd, err := os.Open(f.path)
	if err != nil {
		return nil, nil, err
	}
	defer fd.Close()
	if st, err := fd.Stat(); err == nil && st.Size() > TailBytes {
		if _, err := fd.Seek(-TailBytes, io.SeekEnd); err != nil {
			return nil, nil, err
		}
		// Drop the partial first line.
		br := bufio.NewReader(fd)
		if _, err := br.ReadBytes('\n'); err != nil {
			return nil, nil, err
		}
		fh, wk = ParseTail(br, f.mod)
		return fh, wk, nil
	}
	fh, wk = ParseTail(fd, f.mod)
	return fh, wk, nil
}

// Provider is one row.
type Provider struct {
	Reader *Reader
	Window Window
}

// Rows returns "Codex 5h" and "Codex tuần" sharing one reader.
// SetupHint is the one-line instruction shown when Codex has no logs yet.
const SetupHint = "Chưa thấy log Codex: dùng Codex CLI một lần để có số liệu"

func Rows(r *Reader) []provider.Provider {
	return []provider.Provider{&Provider{r, FiveHour}, &Provider{r, Weekly}}
}

func (p *Provider) ID() string {
	if p.Window == FiveHour {
		return "codex:" + p.Reader.Label + ":5h"
	}
	return "codex:" + p.Reader.Label + ":week"
}

func (p *Provider) Service() string {
	if p.Window == FiveHour {
		return "Codex 5h"
	}
	return "Codex tuần"
}

func (p *Provider) Account() string { return p.Reader.Label }

func (p *Provider) Fetch(ctx context.Context) (provider.Usage, error) {
	s, err := p.Reader.get(ctx)
	if err != nil {
		return provider.Usage{}, err
	}
	w, name, minutes := s.FiveHour, "5h", FiveHourMinutes
	if p.Window == Weekly {
		w, name, minutes = s.Weekly, "tuần", WeekMinutes
	}
	if w == nil {
		return provider.Usage{}, provider.Stale("Codex: log chưa có cửa sổ " + name)
	}
	if err := checkFresh(w, minutes, p.Reader.now()); err != nil {
		return provider.Usage{}, err
	}
	return provider.Usage{Kind: provider.KindQuota, UsedPercent: w.UsedPercent, ResetAt: w.ResetAt}, nil
}

// checkFresh rejects data that can no longer describe the current window:
// the reset time has passed, or it was observed longer ago than the window.
func checkFresh(w *Snap, defMinutes int, now time.Time) error {
	if !w.ResetAt.IsZero() && !now.Before(w.ResetAt) {
		return provider.Reset("Codex: cửa sổ đã reset từ sau lần dùng cuối")
	}
	m := w.WindowMinutes
	if m <= 0 {
		m = defMinutes
	}
	if now.Sub(w.ObservedAt) > time.Duration(m)*time.Minute {
		return provider.Stale("Codex: dữ liệu cũ hơn độ dài cửa sổ")
	}
	return nil
}
