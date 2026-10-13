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

// updateRow is the button (and error) above the footer, only when a newer
// release exists.
func (a *app) updateRow(c *ui.Context) {
	if a.upd.rel == nil {
		return
	}
	t := c.Theme()
	ui.Column(c).Gap(4).Children(func() {
		label := view.UpdateLabel(a.upd.rel.Tag)
		if a.upd.busy {
			label = "Đang tải " + a.upd.rel.Tag + "…"
		}
		ui.PrimaryButton(c, label).Disabled(a.upd.busy).OnClick(a.startUpdate)
		if a.upd.err != "" {
			ui.Text(c, a.upd.err).FontSize(view.SmallSize).TextColor(t.Danger)
		}
	})
}
