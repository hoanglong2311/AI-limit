package keychain

import "errors"

// ErrNotFound is returned by Get when no item exists.
var ErrNotFound = errors.New("keychain: chưa có mục")

// The native Get/Set below call Security.framework directly
// (SecItemCopyMatching / SecItemAdd / SecItemUpdate) through purego, the
// same no-cgo FFI the UI framework uses. The secret only ever travels in
// memory as CFData: it is never passed on a command line, logged, or put
// into an error message. Errors carry only the OSStatus code.
