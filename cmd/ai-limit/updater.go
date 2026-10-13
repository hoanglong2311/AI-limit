package main

import (
	"context"
	"errors"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/hoanglong2311/AI-limit/internal/update"
	"github.com/hoanglong2311/AI-limit/internal/view"
)

// updater holds the update state; it is read and written on the UI thread
// (via win.Update), the network work runs in goroutines.
type updater struct {
	checker update.Checker
	rel     *update.Release // newer release, nil if none
	busy    bool
	err     string
}

// checkLoop checks at startup and then once a day; failures are quiet.
func (a *app) checkLoop(ctx context.Context) {
	for {
		cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		r, newer, err := a.upd.checker.Latest(cctx, update.Current)
		cancel()
		switch {
		case err != nil && !errors.Is(err, update.ErrQuiet):
			log.Print("update: ", err)
		case err == nil && newer:
			a.win.Update(func() { a.upd.rel = &r })
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(24 * time.Hour):
		}
	}
}

func downloadDir() string {
	if h, err := os.UserHomeDir(); err == nil {
		return filepath.Join(h, "Downloads")
	}
	return os.TempDir()
}

func (a *app) startUpdate() {
	if a.upd.busy || a.upd.rel == nil {
		return
	}
	r := *a.upd.rel
	a.upd.busy, a.upd.err = true, ""
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		p, err := a.upd.checker.Download(ctx, r, downloadDir())
		if err == nil {
			err = exec.Command("/usr/bin/open", p).Run()
		}
		a.win.Update(func() {
			a.upd.busy = false
			if err != nil {
				a.upd.err = "Cập nhật lỗi: " + err.Error()
			}
		})
	}()
}

// updateButton is a compact button in the footer row, left of Thoát, only
// when a newer release exists.
func (a *app) updateButton(c *ui.Context) {
	if a.upd.rel == nil {
		return
	}
	label := view.UpdateLabel(a.upd.rel.Tag)
	if a.upd.busy {
		label = "Đang tải…"
	}
	ui.Button(c, label).FontSize(view.SmallSize).TextColor(c.Theme().Accent).
		Disabled(a.upd.busy).OnClick(a.startUpdate)
}

// updateError is shown under the footer after a failed update.
func (a *app) updateError(c *ui.Context) {
	if a.upd.err != "" {
		ui.Text(c, a.upd.err).FontSize(view.SmallSize).TextColor(c.Theme().Danger)
	}
}
