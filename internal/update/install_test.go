package update

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestAllowedURL(t *testing.T) {
	ok := []string{
		"https://github.com/hoanglong2311/AI-limit/releases/download/v0.1.7/AI-limit-0.1.7-universal.dmg",
		"https://github.com/hoanglong2311/AI-limit/releases/download/v0.1.7/AI-limit-0.1.7-universal.dmg.sha256",
		"https://objects.githubusercontent.com/github-production-release-asset/1/2?X-Amz-Signature=abc",
		"https://release-assets.githubusercontent.com/github-production-release-asset/1/2?sig=abc",
	}
	bad := []string{
		"http://github.com/hoanglong2311/AI-limit/releases/download/v0.1.7/x.dmg",
		"https://github.com/evil/AI-limit/releases/download/v0.1.7/x.dmg",
		"https://github.com/hoanglong2311/AI-limit/archive/main.zip",
		"https://github.com.evil.com/hoanglong2311/AI-limit/releases/download/x.dmg",
		"https://evil.com/objects.githubusercontent.com/x",
		"https://user:pw@objects.githubusercontent.com/x",
		"https://objects.githubusercontent.com:8443/x",
		"https://raw.githubusercontent.com/x",
		"file:///etc/passwd",
		"",
	}
	for _, u := range ok {
		if err := AllowedURL(u); err != nil {
			t.Errorf("%s: %v", u, err)
		}
	}
	for _, u := range bad {
		if err := AllowedURL(u); !errors.Is(err, ErrHost) {
			t.Errorf("%s: want ErrHost, got %v", u, err)
		}
	}
	// Errors never echo query strings (signed tokens).
	err := AllowedURL("http://objects.githubusercontent.com/x?X-Amz-Signature=SECRET")
	if err == nil || strings.Contains(err.Error(), "SECRET") {
		t.Fatal(err)
	}
}

func TestDownloadRefusesForeignHost(t *testing.T) {
	c := &Checker{}
	_, err := c.Download(context.Background(), Release{DMGName: "a.dmg", DMGURL: "https://evil.example/a.dmg", NotesSHA: strings.Repeat("a", 64)}, t.TempDir())
	if !errors.Is(err, ErrHost) {
		t.Fatal(err)
	}
}

const plistTmpl = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
	<key>CFBundleExecutable</key><string>AI-limit</string>
	<key>CFBundleIdentifier</key><string>ID</string>
	<key>LSUIElement</key><true/>
	<key>CFBundleShortVersionString</key><string>VER</string>
</dict></plist>`

func mkApp(t *testing.T, dir, name, id, ver string) string {
	t.Helper()
	app := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Join(app, "Contents", "MacOS"), 0o755); err != nil {
		t.Fatal(err)
	}
	pl := strings.NewReplacer("ID", id, "VER", ver).Replace(plistTmpl)
	must(t, os.WriteFile(filepath.Join(app, "Contents", "Info.plist"), []byte(pl), 0o644))
	must(t, os.WriteFile(filepath.Join(app, "Contents", "MacOS", "AI-limit"), []byte(ver), 0o755))
	return app
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func binVer(t *testing.T, app string) string {
	b, err := os.ReadFile(filepath.Join(app, "Contents", "MacOS", "AI-limit"))
	must(t, err)
	return string(b)
}

func TestBundleInfo(t *testing.T) {
	app := mkApp(t, t.TempDir(), "AI-limit.app", "com.hoanglong2311.ailimit", "0.1.7")
	id, v, err := BundleInfo(app)
	if err != nil || id != "com.hoanglong2311.ailimit" || v != "0.1.7" {
		t.Fatal(id, v, err)
	}
}

func TestAppBundle(t *testing.T) {
	if a, err := AppBundle("/Applications/AI-limit.app/Contents/MacOS/AI-limit"); err != nil || a != "/Applications/AI-limit.app" {
		t.Fatal(a, err)
	}
	for _, p := range []string{
		"/private/var/folders/x/AppTranslocation/ABC/d/AI-limit.app/Contents/MacOS/AI-limit",
		"/Volumes/AI-limit/AI-limit.app/Contents/MacOS/AI-limit",
	} {
		if _, err := AppBundle(p); !errors.Is(err, ErrTranslocated) {
			t.Errorf("%s: %v", p, err)
		}
	}
	if _, err := AppBundle("/usr/local/bin/ai-limit"); !errors.Is(err, ErrNotBundle) {
		t.Fatal(err)
	}
}

func TestFindApp(t *testing.T) {
	d := t.TempDir()
	if _, err := FindApp(d); !errors.Is(err, ErrDMGContent) {
		t.Fatal("empty dmg accepted")
	}
	mkApp(t, d, "AI-limit.app", "x", "1")
	must(t, os.Symlink("/Applications", filepath.Join(d, "Applications")))
	if p, err := FindApp(d); err != nil || p != filepath.Join(d, "AI-limit.app") {
		t.Fatal(p, err)
	}
	mkApp(t, d, "Other.app", "x", "1")
	if _, err := FindApp(d); !errors.Is(err, ErrDMGContent) {
		t.Fatal("two apps accepted")
	}
	d2 := t.TempDir()
	must(t, os.Symlink(t.TempDir(), filepath.Join(d2, "AI-limit.app")))
	if _, err := FindApp(d2); !errors.Is(err, ErrDMGContent) {
		t.Fatal("symlinked app accepted")
	}
}

func TestSwapAndRestore(t *testing.T) {
	d := t.TempDir()
	cur := mkApp(t, d, "AI-limit.app", "id", "old")
	staged := mkApp(t, t.TempDir(), "AI-limit.app", "id", "new")
	backup := BackupPath(cur)
	must(t, Swap(cur, staged, backup))
	if binVer(t, cur) != "new" || binVer(t, backup) != "old" {
		t.Fatal("swap")
	}
	must(t, Restore(cur, backup))
	if binVer(t, cur) != "old" {
		t.Fatal("restore")
	}
	if _, err := os.Lstat(backup); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("backup left after restore")
	}
	if _, err := os.Lstat(cur + ".failed"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("failed copy left")
	}
}

func TestSwapRollbackWhenStagedMissing(t *testing.T) {
	d := t.TempDir()
	cur := mkApp(t, d, "AI-limit.app", "id", "old")
	err := Swap(cur, filepath.Join(d, "missing.app"), BackupPath(cur))
	if !errors.Is(err, ErrSwap) || binVer(t, cur) != "old" {
		t.Fatal(err)
	}
	if _, err := os.Lstat(BackupPath(cur)); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("backup left")
	}
}

func TestSwapRefusesExistingBackup(t *testing.T) {
	d := t.TempDir()
	cur := mkApp(t, d, "AI-limit.app", "id", "old")
	mkApp(t, d, ".AI-limit.app.backup", "id", "older")
	staged := mkApp(t, t.TempDir(), "AI-limit.app", "id", "new")
	if err := Swap(cur, staged, BackupPath(cur)); !errors.Is(err, ErrSwap) || binVer(t, cur) != "old" {
		t.Fatal(err)
	}
}

// fakeMac simulates hdiutil/ditto/codesign on temp dirs.
type fakeMac struct {
	t        *testing.T
	dmgApps  func(mnt string) // populates the "dmg"
	failOn   string           // program base name to fail
	attached atomic.Int32
	calls    []string
}

func (f *fakeMac) run(_ context.Context, name string, args ...string) error {
	f.calls = append(f.calls, filepath.Base(name)+" "+strings.Join(args, " "))
	if filepath.Base(name) == f.failOn {
		return errors.New("fail")
	}
	switch filepath.Base(name) {
	case "hdiutil":
		mnt := args[len(args)-1]
		if args[0] == "attach" {
			mnt = args[len(args)-2]
			f.attached.Add(1)
			f.dmgApps(mnt)
			return nil
		}
		f.attached.Add(-1)
		ents, _ := os.ReadDir(mnt)
		for _, e := range ents {
			_ = os.RemoveAll(filepath.Join(mnt, e.Name()))
		}
	case "ditto":
		return os.CopyFS(args[1], os.DirFS(args[0]))
	}
	return nil
}

func installSetup(t *testing.T, id, ver string, fail string) (*fakeMac, string, string, error) {
	t.Helper()
	appsDir := t.TempDir()
	cur := mkApp(t, appsDir, "AI-limit.app", "com.hoanglong2311.ailimit", "0.1.6")
	f := &fakeMac{t: t, failOn: fail, dmgApps: func(mnt string) { mkApp(t, mnt, "AI-limit.app", id, ver) }}
	in := Installer{Run: f.run, MountDir: t.TempDir()}
	backup, err := in.Install(context.Background(), "/tmp/x.dmg", cur, "v0.1.7")
	return f, cur, backup, err
}

func noLeftovers(t *testing.T, cur string) {
	t.Helper()
	ents, _ := os.ReadDir(filepath.Dir(cur))
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), ".AI-limit-update-") {
			t.Fatalf("staging dir left: %s", e.Name())
		}
	}
}

func TestInstallOK(t *testing.T) {
	f, cur, backup, err := installSetup(t, "com.hoanglong2311.ailimit", "0.1.7", "")
	if err != nil {
		t.Fatal(err, f.calls)
	}
	if binVer(t, cur) != "0.1.7" || binVer(t, backup) != "0.1.6" || f.attached.Load() != 0 {
		t.Fatal("not swapped or not detached", f.calls)
	}
	if !strings.Contains(f.calls[0], "attach -nobrowse -readonly") {
		t.Fatal(f.calls[0])
	}
	noLeftovers(t, cur)
}

func TestInstallFailuresKeepOldApp(t *testing.T) {
	cases := []struct {
		name, id, ver, fail string
		want                error
	}{
		{"mount", "com.hoanglong2311.ailimit", "0.1.7", "hdiutil", ErrMount},
		{"bundle id", "com.evil.app", "0.1.7", "", ErrBundleID},
		{"version", "com.hoanglong2311.ailimit", "0.1.8", "", ErrVersion},
		{"copy", "com.hoanglong2311.ailimit", "0.1.7", "ditto", ErrCopy},
		{"signature", "com.hoanglong2311.ailimit", "0.1.7", "codesign", ErrSignature},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f, cur, _, err := installSetup(t, c.id, c.ver, c.fail)
			if !errors.Is(err, c.want) {
				t.Fatalf("got %v", err)
			}
			if binVer(t, cur) != "0.1.6" {
				t.Fatal("old app changed")
			}
			if _, err := os.Lstat(BackupPath(cur)); err == nil {
				t.Fatal("backup created on failure")
			}
			if c.fail != "hdiutil" && f.attached.Load() != 0 {
				t.Fatal("dmg left attached")
			}
			noLeftovers(t, cur)
		})
	}
}

func TestInstallNoWritePermission(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores permissions")
	}
	appsDir := t.TempDir()
	cur := mkApp(t, appsDir, "AI-limit.app", "com.hoanglong2311.ailimit", "0.1.6")
	must(t, os.Chmod(appsDir, 0o555))
	defer os.Chmod(appsDir, 0o755)
	f := &fakeMac{t: t, dmgApps: func(string) {}}
	_, err := Installer{Run: f.run, MountDir: t.TempDir()}.Install(context.Background(), "x.dmg", cur, "0.1.7")
	if !errors.Is(err, ErrNoWrite) || len(f.calls) != 0 {
		t.Fatal(err, f.calls)
	}
}

func finishSetup(t *testing.T) (Finish, string, string) {
	d := t.TempDir()
	cur := mkApp(t, d, "AI-limit.app", "id", "new")
	backup := mkApp(t, d, ".AI-limit.app.backup", "id", "old")
	return Finish{PID: 42, App: cur, Backup: backup, Marker: filepath.Join(t.TempDir(), "ok"),
		Poll: time.Millisecond, ExitWait: 50 * time.Millisecond, LaunchWait: 50 * time.Millisecond}, cur, backup
}

func TestFinishSuccessDeletesBackup(t *testing.T) {
	f, cur, backup := finishSetup(t)
	exited := atomic.Bool{}
	f.Alive = func(int) bool { return !exited.Swap(true) } // alive once, then gone
	var opened []string
	f.Run = func(_ context.Context, name string, args ...string) error {
		if !exited.Load() {
			t.Fatal("launched before old process exited")
		}
		opened = append(opened, strings.Join(args, " "))
		return os.WriteFile(args[len(args)-1], nil, 0o600) // new app writes marker
	}
	must(t, f.Do(context.Background()))
	if _, err := os.Lstat(backup); err == nil || binVer(t, cur) != "new" || len(opened) != 1 ||
		!strings.HasPrefix(opened[0], "-n "+cur+" --args -update-ok ") {
		t.Fatal(opened)
	}
}

func TestFinishLaunchFailRestores(t *testing.T) {
	f, cur, backup := finishSetup(t)
	f.Alive = func(int) bool { return false }
	var n int
	f.Run = func(context.Context, string, ...string) error { n++; return nil } // never writes marker
	if err := f.Do(context.Background()); !errors.Is(err, ErrLaunch) {
		t.Fatal(err)
	}
	if binVer(t, cur) != "old" || n != 2 {
		t.Fatal("not restored/relaunched", n)
	}
	if _, err := os.Lstat(backup); err == nil {
		t.Fatal("backup still there after restore")
	}
}

func TestFinishOldNeverExits(t *testing.T) {
	f, cur, _ := finishSetup(t)
	f.Alive = func(int) bool { return true }
	f.Run = func(context.Context, string, ...string) error { t.Fatal("launched"); return nil }
	if err := f.Do(context.Background()); !errors.Is(err, ErrOldStillRunning) || binVer(t, cur) != "old" {
		t.Fatal(err)
	}
}

func TestVerifyChecksum(t *testing.T) {
	sum := strings.Repeat("ab", 32)
	if !Verify(sum, strings.ToUpper(sum)) || !Verify(sum+"\n", " "+sum) {
		t.Fatal("equal digests rejected")
	}
	if Verify(sum, strings.Repeat("ab", 31)+"ac") || Verify(sum, "") || Verify(sum, sum[:63]) {
		t.Fatal("bad digest accepted")
	}
}
