//go:build darwin

package keychain

import (
	"fmt"
	"runtime"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

const (
	errSecSuccess       = 0
	errSecDuplicateItem = -25299
	errSecItemNotFound  = -25300
	cfStringUTF8        = 0x08000100
)

var (
	loadOnce sync.Once
	loadErr  error

	cfStringCreateWithBytes func(alloc uintptr, b *byte, n int, enc uint32, ext bool) uintptr
	cfDataCreate            func(alloc uintptr, b *byte, n int) uintptr
	cfDataGetLength         func(d uintptr) int
	cfDataGetBytePtr        func(d uintptr) uintptr
	cfDictionaryCreate      func(alloc uintptr, keys, vals *uintptr, n int, kcb, vcb uintptr) uintptr
	cfRelease               func(ref uintptr)
	secItemCopyMatching     func(query uintptr, result *uintptr) int32
	secItemAdd              func(attrs uintptr, result uintptr) int32
	secItemUpdate           func(query, attrs uintptr) int32

	kCFBooleanTrue, kSecClass, kSecClassGenericPassword, kSecAttrService,
	kSecAttrAccount, kSecValueData, kSecReturnData, kSecMatchLimit,
	kSecMatchLimitOne uintptr
	keyCallbacks, valueCallbacks uintptr
)

// deref reads the CFTypeRef stored in a global symbol.
func deref(lib uintptr, name string) (uintptr, error) {
	p, err := purego.Dlsym(lib, name)
	if err != nil {
		return 0, err
	}
	return **(**uintptr)(unsafe.Pointer(&p)), nil
}

func load() error {
	loadOnce.Do(func() {
		cf, err := purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			loadErr = err
			return
		}
		sec, err := purego.Dlopen("/System/Library/Frameworks/Security.framework/Security", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			loadErr = err
			return
		}
		purego.RegisterLibFunc(&cfStringCreateWithBytes, cf, "CFStringCreateWithBytes")
		purego.RegisterLibFunc(&cfDataCreate, cf, "CFDataCreate")
		purego.RegisterLibFunc(&cfDataGetLength, cf, "CFDataGetLength")
		purego.RegisterLibFunc(&cfDataGetBytePtr, cf, "CFDataGetBytePtr")
		purego.RegisterLibFunc(&cfDictionaryCreate, cf, "CFDictionaryCreate")
		purego.RegisterLibFunc(&cfRelease, cf, "CFRelease")
		purego.RegisterLibFunc(&secItemCopyMatching, sec, "SecItemCopyMatching")
		purego.RegisterLibFunc(&secItemAdd, sec, "SecItemAdd")
		purego.RegisterLibFunc(&secItemUpdate, sec, "SecItemUpdate")
		if keyCallbacks, err = purego.Dlsym(cf, "kCFTypeDictionaryKeyCallBacks"); err != nil {
			loadErr = err
			return
		}
		if valueCallbacks, err = purego.Dlsym(cf, "kCFTypeDictionaryValueCallBacks"); err != nil {
			loadErr = err
			return
		}
		for _, s := range []struct {
			lib  uintptr
			name string
			dst  *uintptr
		}{
			{cf, "kCFBooleanTrue", &kCFBooleanTrue},
			{sec, "kSecClass", &kSecClass},
			{sec, "kSecClassGenericPassword", &kSecClassGenericPassword},
			{sec, "kSecAttrService", &kSecAttrService},
			{sec, "kSecAttrAccount", &kSecAttrAccount},
			{sec, "kSecValueData", &kSecValueData},
			{sec, "kSecReturnData", &kSecReturnData},
			{sec, "kSecMatchLimit", &kSecMatchLimit},
			{sec, "kSecMatchLimitOne", &kSecMatchLimitOne},
		} {
			if *s.dst, err = deref(s.lib, s.name); err != nil {
				loadErr = err
				return
			}
		}
	})
	return loadErr
}

func cfString(s string) uintptr {
	b := []byte(s)
	if len(b) == 0 {
		b = []byte{0}
		return cfStringCreateWithBytes(0, &b[0], 0, cfStringUTF8, false)
	}
	return cfStringCreateWithBytes(0, &b[0], len(b), cfStringUTF8, false)
}

func cfData(b []byte) uintptr {
	if len(b) == 0 {
		var z byte
		return cfDataCreate(0, &z, 0)
	}
	return cfDataCreate(0, &b[0], len(b))
}

func dict(keys, vals []uintptr) uintptr {
	d := cfDictionaryCreate(0, &keys[0], &vals[0], len(keys), keyCallbacks, valueCallbacks)
	runtime.KeepAlive(keys)
	runtime.KeepAlive(vals)
	return d
}

// baseQuery is {class: generic password, service[, account]}; the
// returned refs must be released.
func baseQuery(service, account string) (keys, vals, owned []uintptr) {
	svc := cfString(service)
	keys = []uintptr{kSecClass, kSecAttrService}
	vals = []uintptr{kSecClassGenericPassword, svc}
	owned = []uintptr{svc}
	if account != "" {
		acc := cfString(account)
		keys, vals = append(keys, kSecAttrAccount), append(vals, acc)
		owned = append(owned, acc)
	}
	return
}

func release(refs []uintptr) {
	for _, r := range refs {
		if r != 0 {
			cfRelease(r)
		}
	}
}

// Get reads a generic-password item. account may be empty to match any.
func Get(service, account string) (string, error) {
	if err := load(); err != nil {
		return "", ErrUnsupported
	}
	keys, vals, owned := baseQuery(service, account)
	defer release(owned)
	keys = append(keys, kSecReturnData, kSecMatchLimit)
	vals = append(vals, kCFBooleanTrue, kSecMatchLimitOne)
	q := dict(keys, vals)
	defer cfRelease(q)
	var res uintptr
	switch st := secItemCopyMatching(q, &res); st {
	case errSecSuccess:
	case errSecItemNotFound:
		return "", ErrNotFound
	default:
		return "", fmt.Errorf("keychain: lỗi đọc (OSStatus %d)", st)
	}
	defer cfRelease(res)
	n := cfDataGetLength(res)
	p := cfDataGetBytePtr(res)
	if n <= 0 || p == 0 {
		return "", nil
	}
	b := unsafe.Slice(*(**byte)(unsafe.Pointer(&p)), n)
	return string(b), nil
}

// Set creates the item or updates its secret if it exists.
func Set(service, account, secret string) error {
	if err := load(); err != nil {
		return ErrUnsupported
	}
	data := cfData([]byte(secret))
	defer cfRelease(data)
	keys, vals, owned := baseQuery(service, account)
	defer release(owned)
	q := dict(keys, vals)
	defer cfRelease(q)
	upd := dict([]uintptr{kSecValueData}, []uintptr{data})
	defer cfRelease(upd)
	st := secItemUpdate(q, upd)
	if st == errSecItemNotFound {
		attrs := dict(append(keys, kSecValueData), append(vals, data))
		defer cfRelease(attrs)
		st = secItemAdd(attrs, 0)
	}
	if st != errSecSuccess {
		return fmt.Errorf("keychain: lỗi lưu (OSStatus %d)", st)
	}
	return nil
}
