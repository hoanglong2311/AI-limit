package update

import (
	"context"
	"errors"
	"os"
	"time"
)

// UpdateOKFlag is passed to the new app (open -n … --args -update-ok
// <marker>); the new app writes the marker file once it has started, which
// tells the helper the launch succeeded.
const UpdateOKFlag = "update-ok"

// Finish is the detached helper's job after Install: wait until the old
// process has fully exited, launch the new app, and delete the backup
// only after the new app reports it started; otherwise restore the backup
// and launch the old app again.
type Finish struct {
	PID    int
	App    string // swapped-in bundle path
	Backup string
	Marker string // file the new app creates on start
	Run    Runner
	Alive  func(pid int) bool
	// Timeouts; defaults 60s for exit, 30s for launch.
	ExitWait, LaunchWait time.Duration
	Poll                 time.Duration // default 200ms
}

// ErrOldStillRunning: the old process did not exit; nothing is launched.
var ErrOldStillRunning = errors.New("app cũ chưa thoát")

// ErrLaunch: the new app did not start; the old one was restored.
var ErrLaunch = errors.New("app mới không khởi động được, đã khôi phục bản cũ")

func (f Finish) wait(d time.Duration, done func() bool) bool {
	poll := f.Poll
	if poll <= 0 {
		poll = 200 * time.Millisecond
	}
	end := time.Now().Add(d)
	for {
		if done() {
			return true
		}
		if time.Now().After(end) {
			return false
		}
		time.Sleep(poll)
	}
}

// Do runs the helper.
func (f Finish) Do(ctx context.Context) error {
	if f.ExitWait <= 0 {
		f.ExitWait = 60 * time.Second
	}
	if f.LaunchWait <= 0 {
		f.LaunchWait = 30 * time.Second
	}
	if !f.wait(f.ExitWait, func() bool { return !f.Alive(f.PID) }) {
		// Do not launch a second copy; put the old bundle back.
		_ = Restore(f.App, f.Backup)
		return ErrOldStillRunning
	}
	_ = os.Remove(f.Marker)
	err := f.Run(ctx, "/usr/bin/open", "-n", f.App, "--args", "-"+UpdateOKFlag, f.Marker)
	ok := err == nil && f.wait(f.LaunchWait, func() bool {
		_, err := os.Stat(f.Marker)
		return err == nil
	})
	_ = os.Remove(f.Marker)
	if ok {
		return os.RemoveAll(f.Backup)
	}
	if err := Restore(f.App, f.Backup); err != nil {
		return err
	}
	_ = f.Run(ctx, "/usr/bin/open", "-n", f.App)
	return ErrLaunch
}
