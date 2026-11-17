// Package update checks GitHub releases for newer Startship versions and
// installs verified release binaries in place.
package update

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultReleasesURL = "https://api.github.com/repos/sammwyy/startship/releases"
	// DefaultLatestPageURL redirects to the latest release tag without the REST API's per-IP limit.
	DefaultLatestPageURL = "https://github.com/sammwyy/startship/releases/latest"
	defaultDownloadURL   = "https://github.com/sammwyy/startship/releases/download"
	maxBinarySize        = 512 << 20
)

// Binaries are the release assets installed side by side.
var Binaries = []string{"startship", "startshipd"}

var versionPattern = regexp.MustCompile(`^v(\d+)\.(\d+)\.(\d+)(?:-([0-9A-Za-z.-]+))?(?:\+[0-9A-Za-z.-]+)?$`)

// ValidVersion reports whether v is a release tag such as v1.2.3 or v1.2.3-rc.1.
func ValidVersion(v string) bool { return versionPattern.MatchString(v) }

// Compare orders two release tags like semantic versions; invalid tags sort first.
func Compare(a, b string) int {
	ma, mb := versionPattern.FindStringSubmatch(a), versionPattern.FindStringSubmatch(b)
	switch {
	case ma == nil && mb == nil:
		return 0
	case ma == nil:
		return -1
	case mb == nil:
		return 1
	}
	for i := 1; i <= 3; i++ {
		x, _ := strconv.Atoi(ma[i])
		y, _ := strconv.Atoi(mb[i])
		if x != y {
			return cmp(x, y)
		}
	}
	return comparePrerelease(ma[4], mb[4])
}

func cmp(x, y int) int {
	if x < y {
		return -1
	}
	if x > y {
		return 1
	}
	return 0
}

func comparePrerelease(a, b string) int {
	if a == b {
		return 0
	}
	if a == "" {
		return 1
	}
	if b == "" {
		return -1
	}
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(pa) && i < len(pb); i++ {
		x, ex := strconv.Atoi(pa[i])
		y, ey := strconv.Atoi(pb[i])
		switch {
		case ex == nil && ey == nil && x != y:
			return cmp(x, y)
		case ex == nil && ey != nil:
			return -1
		case ex != nil && ey == nil:
			return 1
		case ex != nil && ey != nil && pa[i] != pb[i]:
			return strings.Compare(pa[i], pb[i])
		}
	}
	return cmp(len(pa), len(pb))
}

// Release is the subset of a GitHub release the updater uses.
type Release struct {
	Version   string    `json:"version"`
	URL       string    `json:"url"`
	Notes     string    `json:"notes,omitempty"`
	Published time.Time `json:"published"`
}

// Client talks to the release API and asset downloads.
type Client struct {
	HTTP        *http.Client
	ReleasesURL string
	// LatestPageURL, when set, is the fallback for Latest when the release API fails (e.g. rate limited).
	LatestPageURL string
	// Token, when set, authenticates release API requests (STARTSHIP_GITHUB_TOKEN).
	Token string
	// DownloadURL, when set, is the asset base for every version (like install.sh's STARTSHIP_DOWNLOAD_URL).
	DownloadURL string
	GOOS        string
	GOARCH      string
}

// FromEnv returns a client honoring STARTSHIP_RELEASES_URL, STARTSHIP_DOWNLOAD_URL and STARTSHIP_GITHUB_TOKEN.
func FromEnv() *Client {
	c := &Client{HTTP: &http.Client{Timeout: 5 * time.Minute}, ReleasesURL: DefaultReleasesURL, LatestPageURL: DefaultLatestPageURL, Token: os.Getenv("STARTSHIP_GITHUB_TOKEN"), DownloadURL: os.Getenv("STARTSHIP_DOWNLOAD_URL"), GOOS: runtime.GOOS, GOARCH: runtime.GOARCH}
	if v := os.Getenv("STARTSHIP_RELEASES_URL"); v != "" {
		c.ReleasesURL, c.LatestPageURL = strings.TrimRight(v, "/"), ""
	}
	return c
}

func (c *Client) get(ctx context.Context, url string) (*http.Response, error) {
	return c.do(ctx, url, false)
}

func (c *Client) do(ctx context.Context, url string, api bool) (*http.Response, error) {
	r, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	r.Header.Set("User-Agent", "startship-updater")
	if api {
		r.Header.Set("Accept", "application/vnd.github+json")
		if c.Token != "" {
			r.Header.Set("Authorization", "Bearer "+c.Token)
		}
	}
	resp, err := c.HTTP.Do(r)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		resp.Body.Close()
		if api && (resp.StatusCode == 403 || resp.StatusCode == 429) && resp.Header.Get("X-RateLimit-Remaining") == "0" {
			reset := ""
			if n, err := strconv.ParseInt(resp.Header.Get("X-RateLimit-Reset"), 10, 64); err == nil {
				reset = " until " + time.Unix(n, 0).UTC().Format(time.RFC3339)
			}
			return nil, fmt.Errorf("GET %s: GitHub API rate limit exceeded%s (set STARTSHIP_GITHUB_TOKEN to raise it)", url, reset)
		}
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return resp, nil
}

// Latest returns the newest published (non-draft, non-prerelease) release.
// When the release API fails (unauthenticated requests share a 60/hour limit per IP),
// it falls back to the release page redirect, which has no notes or date.
func (c *Client) Latest(ctx context.Context) (Release, error) {
	r, err := c.latestAPI(ctx)
	if err == nil || c.LatestPageURL == "" || ctx.Err() != nil {
		return r, err
	}
	r, pageErr := c.latestPage(ctx)
	if pageErr != nil {
		return Release{}, fmt.Errorf("%w; fallback: %v", err, pageErr)
	}
	return r, nil
}

func (c *Client) latestPage(ctx context.Context) (Release, error) {
	r, err := http.NewRequestWithContext(ctx, "HEAD", c.LatestPageURL, nil)
	if err != nil {
		return Release{}, err
	}
	r.Header.Set("User-Agent", "startship-updater")
	hc := *c.HTTP
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := hc.Do(r)
	if err != nil {
		return Release{}, err
	}
	resp.Body.Close()
	loc, err := resp.Location()
	if err != nil {
		return Release{}, fmt.Errorf("HEAD %s: %s without a release redirect", c.LatestPageURL, resp.Status)
	}
	tag := path.Base(loc.Path)
	if !strings.HasSuffix(path.Dir(loc.Path), "/releases/tag") || !ValidVersion(tag) {
		return Release{}, fmt.Errorf("HEAD %s: unexpected redirect to %s", c.LatestPageURL, loc)
	}
	return Release{Version: tag, URL: loc.String()}, nil
}

func (c *Client) latestAPI(ctx context.Context) (Release, error) {
	resp, err := c.do(ctx, c.ReleasesURL+"/latest", true)
	if err != nil {
		return Release{}, err
	}
	defer resp.Body.Close()
	var v struct {
		Tag       string    `json:"tag_name"`
		URL       string    `json:"html_url"`
		Body      string    `json:"body"`
		Published time.Time `json:"published_at"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&v); err != nil {
		return Release{}, fmt.Errorf("invalid release response: %w", err)
	}
	if !ValidVersion(v.Tag) {
		return Release{}, fmt.Errorf("latest release has invalid tag %q", v.Tag)
	}
	return Release{Version: v.Tag, URL: v.URL, Notes: v.Body, Published: v.Published}, nil
}

func (c *Client) assetBase(version string) string {
	if c.DownloadURL != "" {
		return strings.TrimRight(c.DownloadURL, "/")
	}
	return defaultDownloadURL + "/" + version
}

func (c *Client) checksums(ctx context.Context, version string) (map[string]string, error) {
	resp, err := c.get(ctx, c.assetBase(version)+"/checksums.txt")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	sums := map[string]string{}
	s := bufio.NewScanner(io.LimitReader(resp.Body, 1<<20))
	for s.Scan() {
		f := strings.Fields(s.Text())
		if len(f) == 2 && len(f[0]) == 64 {
			sums[strings.TrimPrefix(f[1], "*")] = strings.ToLower(f[0])
		}
	}
	return sums, s.Err()
}

// Install downloads version's assets for each named binary present in dir (at
// least the first is required), verifies their SHA-256 checksums and atomically
// replaces them. Nothing is replaced unless every download verifies.
func (c *Client) Install(ctx context.Context, version, dir string, names []string) ([]string, error) {
	if !ValidVersion(version) {
		return nil, fmt.Errorf("invalid version %q", version)
	}
	if c.GOOS != "linux" || (c.GOARCH != "amd64" && c.GOARCH != "arm64") {
		return nil, fmt.Errorf("releases are published for linux/amd64 and linux/arm64, not %s/%s", c.GOOS, c.GOARCH)
	}
	sums, err := c.checksums(ctx, version)
	if err != nil {
		return nil, err
	}
	type staged struct{ tmp, dst string }
	var ready []staged
	defer func() {
		for _, s := range ready {
			os.Remove(s.tmp)
		}
	}()
	for i, name := range names {
		dst := filepath.Join(dir, name)
		info, err := os.Stat(dst)
		if err != nil {
			if i > 0 && errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, err
		}
		asset := name + "_" + c.GOOS + "_" + c.GOARCH
		want := sums[asset]
		if want == "" {
			return nil, fmt.Errorf("checksums.txt has no entry for %s", asset)
		}
		tmp, err := c.download(ctx, c.assetBase(version)+"/"+asset, dir, name, want, info.Mode().Perm()|0o111)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", asset, err)
		}
		ready = append(ready, staged{tmp, dst})
	}
	var installed []string
	for _, s := range ready {
		if err := os.Rename(s.tmp, s.dst); err != nil {
			return installed, err
		}
		installed = append(installed, s.dst)
	}
	ready = nil
	return installed, nil
}

func (c *Client) download(ctx context.Context, url, dir, name, want string, mode os.FileMode) (string, error) {
	resp, err := c.get(ctx, url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	f, err := os.CreateTemp(dir, "."+name+".update-*")
	if err != nil {
		return "", err
	}
	ok := false
	defer func() {
		if !ok {
			f.Close()
			os.Remove(f.Name())
		}
	}()
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, maxBinarySize+1))
	if err != nil {
		return "", err
	}
	if n > maxBinarySize {
		return "", errors.New("asset too large")
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		return "", fmt.Errorf("checksum mismatch: got %s, want %s", got, want)
	}
	if err := f.Chmod(mode); err != nil {
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	ok = true
	return f.Name(), nil
}

// Request asks the privileged updater (startship-update.service) to install a version.
type Request struct {
	Version   string    `json:"version"`
	Requested time.Time `json:"requested"`
	By        string    `json:"by,omitempty"`
}

// Status is the last result written by the privileged updater.
type Status struct {
	State    string    `json:"state"` // running, succeeded or failed
	Version  string    `json:"version"`
	Error    string    `json:"error,omitempty"`
	Started  time.Time `json:"started"`
	Finished time.Time `json:"finished,omitzero"`
}

// WriteJSON atomically replaces path with v (rename replaces a symlink, never its target).
func WriteJSON(path string, v any, mode os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+"-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err := json.NewEncoder(f).Encode(v); err != nil {
		f.Close()
		return err
	}
	if err := f.Chmod(mode); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

// ReadJSON decodes a small JSON file into v.
func ReadJSON(path string, v any) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return json.NewDecoder(io.LimitReader(f, 64<<10)).Decode(v)
}
