//go:build !darwin

package keychain

// Get reads a generic-password item natively (macOS only).
func Get(service, account string) (string, error) { return "", ErrUnsupported }

// Set creates or updates a generic-password item natively (macOS only).
func Set(service, account, secret string) error { return ErrUnsupported }
