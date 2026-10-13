// Package keychain accesses the macOS Keychain.
//
// Read is read-only and runs /usr/bin/security find-generic-password for
// items other apps own (Claude Code); no secret is passed on its command
// line. Get and Set use the native Security.framework API (see
// native_darwin.go) for the app's own items such as the DeepSeek key;
// they never shell out.
package keychain

import (
	"context"
	"errors"
	"os/exec"
	"runtime"
	"strings"
)

// ErrUnsupported is returned on non-macOS systems.
var ErrUnsupported = errors.New("keychain: chỉ hỗ trợ macOS")

// Read returns the password of a generic-password item (service, account).
// account may be empty to match any account for the service.
func Read(ctx context.Context, service, account string) (string, error) {
	if runtime.GOOS != "darwin" {
		return "", ErrUnsupported
	}
	args := []string{"find-generic-password", "-s", service}
	if account != "" {
		args = append(args, "-a", account)
	}
	args = append(args, "-w")
	out, err := exec.CommandContext(ctx, "/usr/bin/security", args...).Output()
	if err != nil {
		return "", errors.New("keychain: không tìm thấy mục " + service)
	}
	return strings.TrimRight(string(out), "\n"), nil
}
