package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/egoist/mygo"

	"github.com/hoanglong2311/AI-limit/internal/update"
)

// Hidden flags for the self-update protocol.
var (
	updateOK     = flag.String(update.UpdateOKFlag, "", "nội bộ: file báo app mới đã chạy")
	finishUpdate = flag.Bool("finish-update", false, "nội bộ: tiến trình phụ hoàn tất cập nhật")
)

// runCmd runs a program with an argument list; output is discarded (never
// logged), only the exit status matters.
func runCmd(ctx context.Context, name string, args ...string) error {
	return exec.CommandContext(ctx, name, args...).Run()
}

func alive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// runFinishHelper is the detached helper: -finish-update <pid> <app> <backup> <marker>.
// It runs from the old binary (inside the backup bundle) and exits.
func runFinishHelper() {
	a := flag.Args()
	if len(a) != 4 {
		os.Exit(2)
	}
	pid, err := strconv.Atoi(a[0])
	if err != nil {
		os.Exit(2)
	}
	f := update.Finish{PID: pid, App: a[1], Backup: a[2], Marker: a[3], Run: runCmd, Alive: alive}
	if err := f.Do(context.Background()); err != nil {
		os.Exit(1)
	}
	os.Exit(0)
}

// markLaunched tells the helper this (new) app started.
func markLaunched() {
	if *updateOK == "" {
		return
	}
	if f, err := os.OpenFile(*updateOK, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600); err == nil {
		f.Close()
	}
}

// selfUpdate installs the verified dmg over the running app and starts the
// helper that relaunches it after this process exits. On error the old app
// is untouched.
func selfUpdate(ctx context.Context, dmg, version string) error {
	exe, err := os.Executable()
	if err != nil {
		return update.ErrNotBundle
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return update.ErrNotBundle
	}
	cur, err := update.AppBundle(exe)
	if err != nil {
		return err
	}
	backup, err := update.Installer{Run: runCmd}.Install(ctx, dmg, cur, version)
	if err != nil {
		return err
	}
	// The dmg is detached by now; it is no longer needed.
	_ = os.Remove(dmg)
	marker := filepath.Join(os.TempDir(), fmt.Sprintf("ai-limit-update-%d-%d.ok", os.Getpid(), time.Now().UnixNano()))
	helper := filepath.Join(backup, "Contents", "MacOS", filepath.Base(exe))
	cmd := exec.Command(helper, "-finish-update", strconv.Itoa(os.Getpid()), cur, backup, marker)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		_ = update.Restore(cur, backup)
		return errors.New("không chạy được tiến trình khởi động lại (đã giữ bản cũ)")
	}
	_ = cmd.Process.Release()
	return nil
}

// quitForUpdate quits so the helper can relaunch the new app.
func (a *app) quitForUpdate() { mygo.App.Quit() }
