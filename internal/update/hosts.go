package update

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// Repo is the only repository downloads may come from.
const Repo = "hoanglong2311/AI-limit"

// allowedHosts are the only hosts the updater downloads from (HTTPS only).
// GitHub serves release assets from github.com and redirects to a
// githubusercontent.com asset host: objects.githubusercontent.com (legacy)
// or release-assets.githubusercontent.com (current).
var allowedHosts = map[string]bool{
	"github.com":                           true,
	"objects.githubusercontent.com":        true,
	"release-assets.githubusercontent.com": true,
}

// ErrHost is returned for a URL outside the allowlist.
var ErrHost = errors.New("địa chỉ tải không được phép")

// AllowedURL accepts only https URLs on an allowed host, with no
// credentials or explicit port; github.com URLs must be this repo's
// release downloads.
func AllowedURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return ErrHost
	}
	host := strings.ToLower(u.Hostname())
	switch {
	case u.Scheme != "https", u.User != nil, u.Port() != "", !allowedHosts[host], u.Host != u.Hostname():
		return fmt.Errorf("%w: %s", ErrHost, safeHost(u))
	case host == "github.com" && !strings.HasPrefix(u.EscapedPath(), "/"+Repo+"/releases/download/"):
		return fmt.Errorf("%w: %s", ErrHost, safeHost(u))
	}
	return nil
}

// safeHost names only scheme and host in error messages (never query
// strings, which on the asset CDN carry signed tokens).
func safeHost(u *url.URL) string { return u.Scheme + "://" + u.Hostname() }

// downloadClient follows redirects only within the allowlist.
func downloadClient(base *http.Client) *http.Client {
	c := &http.Client{}
	if base != nil {
		c.Transport, c.Timeout = base.Transport, base.Timeout
	}
	c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("quá nhiều chuyển hướng")
		}
		return AllowedURL(req.URL.String())
	}
	return c
}
