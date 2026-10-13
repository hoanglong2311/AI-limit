package main

import (
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/hoanglong2311/AI-limit/internal/scheduler"

	"github.com/hoanglong2311/AI-limit/internal/update"
)

// The helper of an older version launches the new app with
// `-update-ok <marker>` and deletes the backup only once the marker
// exists. If this handling is removed, every self-update rolls back.
func TestUpdateOKFlagRegistered(t *testing.T) {
	f := flag.Lookup(update.UpdateOKFlag)
	if f == nil || update.UpdateOKFlag != "update-ok" {
		t.Fatal("-update-ok flag not registered")
	}
	if err := flag.CommandLine.Parse([]string{"-update-ok", "/x/m.ok"}); err != nil || *updateOK != "/x/m.ok" {
		t.Fatalf("parse: %v %q", err, *updateOK)
	}
	*updateOK = ""
}

func TestMarkLaunchedWritesMarker(t *testing.T) {
	m := filepath.Join(t.TempDir(), "ok")
	*updateOK = m
	defer func() { *updateOK = "" }()
	markLaunched()
	fi, err := os.Stat(m)
	if err != nil {
		t.Fatal("marker not written")
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", fi.Mode().Perm())
	}
	*updateOK = ""
	markLaunched() // no flag: nothing happens, no panic
}

// Startup must call markLaunched (in ready) and dispatch -finish-update.
func TestStartupHandlesUpdateFlags(t *testing.T) {
	b, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	ready := regexp.MustCompile(`(?s)func \(a \*app\) ready\(\) \{.*?\n\}`).Find(b)
	if ready == nil || !regexp.MustCompile(`\bmarkLaunched\(\)`).Match(ready) {
		t.Fatal("ready() no longer calls markLaunched()")
	}
	mainFn := regexp.MustCompile(`(?s)func main\(\) \{.*?\n\}`).Find(b)
	if mainFn == nil || !regexp.MustCompile(`(?s)flag\.Parse\(\).*if \*finishUpdate \{\s*runFinishHelper\(\)`).Match(mainFn) {
		t.Fatal("main() no longer handles -finish-update after flag.Parse")
	}
}

func TestFooterShowsUpdateCurrent(t *testing.T) {
	now := time.Date(2026, 10, 13, 20, 5, 0, 0, time.Local)
	got := footerText(now, []scheduler.State{{LastSuccess: now}})
	if want := "v" + update.Current + " · Cập nhật lúc 20:05"; got != want {
		t.Fatalf("%q want %q", got, want)
	}
	// The popup must render footerText, not a separate string.
	b, _ := os.ReadFile("main.go")
	if !regexp.MustCompile(`ui\.Text\(c, footerText\(now, rows\)\)`).Match(b) {
		t.Fatal("footer no longer uses footerText")
	}
}
