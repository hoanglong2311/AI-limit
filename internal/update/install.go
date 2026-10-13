package update

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// BundleName is the app bundle inside the dmg.
const BundleName = "AI-limit.app"

// Install errors, shown to the user (Vietnamese, no paths with secrets).
var (
	ErrTranslocated = errors.New("app đang chạy từ vị trí tạm của macOS (App Translocation) hoặc từ dmg; hãy kéo AI-limit vào Applications rồi mở lại")
	ErrNotBundle    = errors.New("không xác định được thư mục AI-limit.app đang chạy")
	ErrMount        = errors.New("không mở được file dmg")
	ErrDMGContent   = errors.New("dmg phải chứa đúng một AI-limit.app")
	ErrNoWrite      = errors.New("không có quyền ghi vào thư mục chứa app")
	ErrCopy         = errors.New("không chép được app mới")
	ErrBundleID     = errors.New("bundle id của bản mới không khớp với app đang chạy")
	ErrVersion      = errors.New("phiên bản trong dmg không khớp với bản đã tải")
	ErrSignature    = errors.New("chữ ký của app mới không hợp lệ")
	ErrSwap         = errors.New("không thay được app (đã giữ nguyên bản cũ)")
)

// Runner runs a program with an argument list (never a shell string).
type Runner func(ctx context.Context, name string, args ...string) error

// AppBundle returns the .app containing exe (…/X.app/Contents/MacOS/bin).
// It refuses App Translocation and anything under /Volumes/ (a mounted
// dmg): replacing those would not update the user's copy.
func AppBundle(exe string) (string, error) {
	if strings.Contains(exe, "/AppTranslocation/") || strings.HasPrefix(exe, "/Volumes/") {
		return "", ErrTranslocated
	}
	app := filepath.Dir(filepath.Dir(filepath.Dir(exe)))
	if filepath.Ext(app) != ".app" || filepath.Base(filepath.Dir(exe)) != "MacOS" {
		return "", ErrNotBundle
	}
	return app, nil
}

// FindApp returns mount/AI-limit.app if it is the only .app at the top
// of the mounted dmg and a real directory (not a symlink).
func FindApp(mount string) (string, error) {
	ents, err := os.ReadDir(mount)
	if err != nil {
		return "", ErrDMGContent
	}
	var apps []string
	for _, e := range ents {
		if strings.HasSuffix(e.Name(), ".app") {
			apps = append(apps, e.Name())
		}
	}
	if len(apps) != 1 || apps[0] != BundleName {
		return "", ErrDMGContent
	}
	p := filepath.Join(mount, BundleName)
	if fi, err := os.Lstat(p); err != nil || !fi.IsDir() {
		return "", ErrDMGContent
	}
	return p, nil
}

// BundleInfo reads CFBundleIdentifier and CFBundleShortVersionString from
// an XML Info.plist (top-level dict: <key> followed by <string>).
func BundleInfo(app string) (id, version string, err error) {
	b, err := os.ReadFile(filepath.Join(app, "Contents", "Info.plist"))
	if err != nil {
		return "", "", err
	}
	var pl struct {
		Dict struct {
			Items []struct {
				XMLName xml.Name
				Value   string `xml:",chardata"`
			} `xml:",any"`
		} `xml:"dict"`
	}
	if len(b) > 1<<20 || xml.Unmarshal(b, &pl) != nil {
		return "", "", errors.New("Info.plist không đọc được")
	}
	items := pl.Dict.Items
	for i := 0; i+1 < len(items); i++ {
		if items[i].XMLName.Local != "key" || items[i+1].XMLName.Local != "string" {
			continue
		}
		switch strings.TrimSpace(items[i].Value) {
		case "CFBundleIdentifier":
			id = strings.TrimSpace(items[i+1].Value)
		case "CFBundleShortVersionString":
			version = strings.TrimSpace(items[i+1].Value)
		}
	}
	return id, version, nil
}

// Swap replaces cur with staged: cur is renamed to backup, staged moved
// into cur. On failure cur is restored and an error returned. The backup
// is kept; delete it only after the new app has launched (or call
// Restore to roll back).
func Swap(cur, staged, backup string) error {
	if _, err := os.Lstat(backup); err == nil {
		return ErrSwap // never overwrite an existing backup
	}
	if err := os.Rename(cur, backup); err != nil {
		return ErrSwap
	}
	if err := os.Rename(staged, cur); err != nil {
		if rerr := os.Rename(backup, cur); rerr != nil {
			return fmt.Errorf("%w; khôi phục thất bại, bản cũ ở %s", ErrSwap, backup)
		}
		return ErrSwap
	}
	return nil
}

// Restore puts backup back at cur, moving the (failed) new app aside and
// deleting it.
func Restore(cur, backup string) error {
	if _, err := os.Lstat(backup); err != nil {
		return err
	}
	failed := cur + ".failed"
	_ = os.RemoveAll(failed)
	if _, err := os.Lstat(cur); err == nil {
		if err := os.Rename(cur, failed); err != nil {
			return err
		}
	}
	if err := os.Rename(backup, cur); err != nil {
		_ = os.Rename(failed, cur)
		return err
	}
	return os.RemoveAll(failed)
}

// BackupPath is the hidden backup next to cur.
func BackupPath(cur string) string {
	return filepath.Join(filepath.Dir(cur), "."+filepath.Base(cur)+".backup")
}

// Installer prepares an update from a verified dmg. Run executes hdiutil,
// ditto and codesign; tests inject a fake.
type Installer struct {
	Run Runner
	// MountDir is where temp mountpoints are created (default os.TempDir).
	MountDir string
}

// Install mounts dmg read-only, checks it holds exactly one AI-limit.app
// whose bundle id equals the running app's and whose version equals
// version, copies it with ditto into a temp dir next to cur (same
// volume), verifies the signature, detaches the dmg and swaps the
// bundles. It returns the backup path; the old app is untouched on error.
func (in Installer) Install(ctx context.Context, dmg, cur, version string) (backup string, err error) {
	curID, _, err := BundleInfo(cur)
	if err != nil || curID == "" {
		return "", ErrNotBundle
	}
	parent := filepath.Dir(cur)
	stageDir, err := os.MkdirTemp(parent, ".AI-limit-update-")
	if err != nil {
		return "", ErrNoWrite
	}
	defer os.RemoveAll(stageDir)
	mnt, err := os.MkdirTemp(in.MountDir, "ai-limit-mnt-")
	if err != nil {
		return "", ErrMount
	}
	defer os.Remove(mnt) // empty after detach
	if err := in.Run(ctx, "/usr/bin/hdiutil", "attach", "-nobrowse", "-readonly", "-noautoopen", "-mountpoint", mnt, dmg); err != nil {
		return "", ErrMount
	}
	detached := false
	detach := func() {
		if detached {
			return
		}
		detached = true
		if in.Run(context.Background(), "/usr/bin/hdiutil", "detach", mnt) != nil {
			_ = in.Run(context.Background(), "/usr/bin/hdiutil", "detach", "-force", mnt)
		}
	}
	defer detach()
	src, err := FindApp(mnt)
	if err != nil {
		return "", err
	}
	if id, v, err := BundleInfo(src); err != nil || id != curID {
		return "", ErrBundleID
	} else if v != strings.TrimPrefix(version, "v") {
		return "", ErrVersion
	}
	staged := filepath.Join(stageDir, BundleName)
	if err := in.Run(ctx, "/usr/bin/ditto", src, staged); err != nil {
		return "", ErrCopy
	}
	detach()
	// Re-check the copy (the dmg is gone now) and its signature.
	if id, v, err := BundleInfo(staged); err != nil || id != curID || v != strings.TrimPrefix(version, "v") {
		return "", ErrCopy
	}
	if err := in.Run(ctx, "/usr/bin/codesign", "--verify", "--deep", "--strict", staged); err != nil {
		return "", ErrSignature
	}
	backup = BackupPath(cur)
	_ = os.RemoveAll(backup) // stale backup from an earlier, finished run
	if err := Swap(cur, staged, backup); err != nil {
		return "", err
	}
	return backup, nil
}
