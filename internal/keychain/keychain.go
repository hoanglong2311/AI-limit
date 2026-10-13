// Package keychain reads secrets from the macOS Keychain, read-only.
// It shells out to /usr/bin/security (no cgo) and never writes or modifies items.
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
