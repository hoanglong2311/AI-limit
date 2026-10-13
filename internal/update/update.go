// Package update checks GitHub for a newer release and downloads its dmg,
// verifying the SHA-256 before handing it to the caller. No token is used;
// the public API allows 60 unauthenticated requests per hour per IP.
package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Current is the app version; a test keeps it equal to mygo.json.
const Current = "0.1.8"

// LatestURL is the GitHub API endpoint for the newest release.
const LatestURL = "https://api.github.com/repos/hoanglong2311/AI-limit/releases/latest"

// ErrQuiet marks failures not worth showing (offline, rate limit, no release).
var ErrQuiet = errors.New("update: không kiểm tra được")

// Release is the part of the latest release the app needs.
type Release struct {
	Tag       string // "v0.1.4"
	Version   string // "0.1.4"
	DMGName   string
	DMGURL    string
	SHA256URL string // asset "<dmg>.sha256", may be empty
	NotesSHA  string // 64-hex found in the release notes, may be empty
}

type apiRelease struct {
	TagName    string `json:"tag_name"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
	Body       string `json:"body"`
	Assets     []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

var hexRe = regexp.MustCompile(`\b[0-9a-fA-F]{64}\b`)

// Parse decodes the API response.
func Parse(body []byte) (Release, error) {
	var a apiRelease
	if err := json.Unmarshal(body, &a); err != nil {
		return Release{}, fmt.Errorf("update: JSON không hợp lệ: %w", err)
	}
	if a.Draft || a.Prerelease || a.TagName == "" {
		return Release{}, ErrQuiet
	}
	r := Release{Tag: a.TagName, Version: strings.TrimPrefix(a.TagName, "v")}
	for _, as := range a.Assets {
		if strings.HasSuffix(as.Name, ".dmg") && r.DMGURL == "" {
			r.DMGName, r.DMGURL = as.Name, as.URL
		}
	}
	for _, as := range a.Assets {
		if r.DMGName != "" && as.Name == r.DMGName+".sha256" {
			r.SHA256URL = as.URL
		}
	}
	if r.DMGURL == "" {
		return Release{}, fmt.Errorf("update: bản %s không có dmg", a.TagName)
	}
	if m := hexRe.FindString(a.Body); m != "" {
		r.NotesSHA = strings.ToLower(m)
	}
	return r, nil
}

// parseSemver returns major, minor, patch; pre-release/build suffixes are ignored.
func parseSemver(v string) ([3]int, bool) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return [3]int{}, false
	}
	var out [3]int
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return [3]int{}, false
		}
		out[i] = n
	}
	return out, true
}

// Newer reports whether candidate is a strictly newer semver than current.
func Newer(candidate, current string) bool {
	a, ok1 := parseSemver(candidate)
	b, ok2 := parseSemver(current)
	if !ok1 || !ok2 {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return false
}

// Checker fetches the latest release.
type Checker struct {
	URL  string       // default LatestURL
	HTTP *http.Client // default http.DefaultClient
	// AllowAnyHost disables the download host allowlist; tests only.
	AllowAnyHost bool
}

func (c *Checker) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

// dl is the client for release assets: redirects stay in the allowlist.
// Tests that inject HTTP (httptest servers) bypass the allowlist via
// AllowAnyHost.
func (c *Checker) dl() *http.Client {
	if c.AllowAnyHost {
		return c.client()
	}
	return downloadClient(c.HTTP)
}

func (c *Checker) allowed(u string) error {
	if c.AllowAnyHost {
		return nil
	}
	return AllowedURL(u)
}

func get(ctx context.Context, hc *http.Client, url string, limit int64) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("User-Agent", "ai-limit/"+Current)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := hc.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit))
	return b, resp.StatusCode, err
}

// Latest returns the latest release and whether it is newer than Current.
// Network errors, rate limits (403/429) and 404 return ErrQuiet.
func (c *Checker) Latest(ctx context.Context, current string) (Release, bool, error) {
	url := c.URL
	if url == "" {
		url = LatestURL
	}
	b, code, err := get(ctx, c.client(), url, 1<<20)
	if err != nil {
		return Release{}, false, fmt.Errorf("%w: %v", ErrQuiet, err)
	}
	if code != http.StatusOK {
		return Release{}, false, fmt.Errorf("%w: HTTP %d", ErrQuiet, code)
	}
	r, err := Parse(b)
	if err != nil {
		return Release{}, false, err
	}
	return r, Newer(r.Version, current), nil
}

// ExpectedSHA prefers the .sha256 asset, falling back to the notes.
func (c *Checker) ExpectedSHA(ctx context.Context, r Release) (string, error) {
	if r.SHA256URL != "" && c.allowed(r.SHA256URL) == nil {
		b, code, err := get(ctx, c.dl(), r.SHA256URL, 4096)
		if err == nil && code == http.StatusOK {
			if m := hexRe.FindString(string(b)); m != "" {
				return strings.ToLower(m), nil
			}
		}
	}
	if r.NotesSHA != "" {
		return r.NotesSHA, nil
	}
	return "", errors.New("không tìm thấy SHA-256 của bản phát hành")
}

// Download saves the dmg into dir and verifies it; on mismatch the file is
// deleted and an error returned. It returns the path of the verified file.
func (c *Checker) Download(ctx context.Context, r Release, dir string) (string, error) {
	if err := c.allowed(r.DMGURL); err != nil {
		return "", err
	}
	want, err := c.ExpectedSHA(ctx, r)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.DMGURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "ai-limit/"+Current)
	resp, err := c.dl().Do(req)
	if err != nil {
		if errors.Is(err, ErrHost) {
			return "", ErrHost
		}
		return "", errors.New("tải thất bại: lỗi mạng")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("tải thất bại: HTTP %d", resp.StatusCode)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	f, err := os.CreateTemp(dir, ".ai-limit-*.part")
	if err != nil {
		return "", err
	}
	tmp := f.Name()
	defer os.Remove(tmp) // no-op after a successful rename
	h := sha256.New()
	_, err = io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, 512<<20))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", errors.New("tải thất bại: lỗi ghi file")
	}
	if got := hex.EncodeToString(h.Sum(nil)); !Verify(got, want) {
		return "", fmt.Errorf("SHA-256 không khớp (nhận %s…, cần %s…)", got[:12], want[:min(12, len(want))])
	}
	dst := filepath.Join(dir, filepath.Base(r.DMGName))
	if err := os.Rename(tmp, dst); err != nil {
		return "", err
	}
	return dst, nil
}

// Verify compares two hex digests case-insensitively.
func Verify(got, want string) bool {
	got, want = strings.TrimSpace(got), strings.TrimSpace(want)
	return len(want) == 64 && hexRe.MatchString(want) && strings.EqualFold(got, want)
}

// FileSHA256 hashes a file.
func FileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
