// Package update finds the latest GitHub release of groot, downloads its
// archive for this platform and replaces the running binary. Requests go out
// unauthenticated first. GITHUB_TOKEN is optional: it is sent only to retry a
// request GitHub refused with 403 (rate limit) or 404.
package update

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Repo is the GitHub repository that publishes the releases.
const Repo = "tinchoram/groot-cli"

// ChecksumsName is the checksum file goreleaser writes next to the archives.
const ChecksumsName = "checksums.txt"

// APIBase is the GitHub API root. Tests point it at an httptest server.
var APIBase = "https://api.github.com"

const (
	latestTimeout = 10 * time.Second
	maxChecksums  = 1 << 20
	maxArchive    = 256 << 20
	maxBinary     = 256 << 20
)

// ErrNotFound means GitHub has no such release or asset for the version asked.
var ErrNotFound = errors.New("release not found: check the version and https://github.com/tinchoram/groot-cli/releases")

// jsonAccept is the Accept header of the release API.
const jsonAccept = "application/vnd.github+json"

// Release is the latest published release.
type Release struct {
	Tag     string // as published, for example v0.6.0
	Version string // the tag without the leading v
	Assets  []Asset
}

// Asset is one file of a release. URL is the API url, used with a token;
// BrowserURL is the public download url.
type Asset struct {
	Name       string
	URL        string
	BrowserURL string
	Size       int64
}

// Token returns the GitHub token from the environment: GITHUB_TOKEN first, then GH_TOKEN.
func Token(getenv func(string) string) string {
	if t := getenv("GITHUB_TOKEN"); t != "" {
		return t
	}
	return getenv("GH_TOKEN")
}

// userAgent sends groot/<version> with every request.
type userAgent struct {
	version string
	next    http.RoundTripper
}

func (u userAgent) RoundTrip(req *http.Request) (*http.Response, error) {
	r := req.Clone(req.Context())
	r.Header.Set("User-Agent", "groot/"+u.version)
	next := u.next
	if next == nil {
		next = http.DefaultTransport
	}
	return next.RoundTrip(r)
}

// NewClient returns an HTTP client that identifies itself as groot/<version>.
func NewClient(version string) *http.Client {
	return &http.Client{Transport: userAgent{version: version}}
}

// apiRelease is the part of the GitHub release JSON that groot reads.
type apiRelease struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name               string `json:"name"`
		URL                string `json:"url"`
		BrowserDownloadURL string `json:"browser_download_url"`
		Size               int64  `json:"size"`
	} `json:"assets"`
}

// getOnce sends one GET. A token, when given, is sent as a bearer header.
func getOnce(ctx context.Context, client *http.Client, url, accept, token string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return client.Do(req)
}

// retryable is a refusal that a token can fix: a rate limit or a 404.
func retryable(status int) bool {
	return status == http.StatusForbidden || status == http.StatusNotFound
}

// Latest asks GitHub for the latest release of repo. The first request is
// unauthenticated; with a token, a 403 or 404 is retried once with it.
func Latest(ctx context.Context, client *http.Client, repo, token string) (Release, error) {
	ctx, cancel := context.WithTimeout(ctx, latestTimeout)
	defer cancel()
	url := APIBase + "/repos/" + repo + "/releases/latest"
	resp, err := getOnce(ctx, client, url, jsonAccept, "")
	if err != nil {
		return Release{}, fmt.Errorf("latest release: %w", err)
	}
	if token != "" && retryable(resp.StatusCode) {
		resp.Body.Close()
		resp, err = getOnce(ctx, client, url, jsonAccept, token)
		if err != nil {
			return Release{}, fmt.Errorf("latest release: %w", err)
		}
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return Release{}, ErrNotFound
	case resp.StatusCode == http.StatusForbidden:
		return Release{}, fmt.Errorf("latest release: GitHub refused the request (%s), probably the API rate limit: set GITHUB_TOKEN (optional) to raise it", resp.Status)
	case resp.StatusCode != http.StatusOK:
		return Release{}, fmt.Errorf("latest release: %s", resp.Status)
	}
	var api apiRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&api); err != nil {
		return Release{}, fmt.Errorf("latest release: %w", err)
	}
	if api.TagName == "" {
		return Release{}, errors.New("latest release has no tag")
	}
	rel := Release{Tag: api.TagName, Version: strings.TrimPrefix(api.TagName, "v")}
	for _, a := range api.Assets {
		rel.Assets = append(rel.Assets, Asset{Name: a.Name, URL: a.URL, BrowserURL: a.BrowserDownloadURL, Size: a.Size})
	}
	return rel, nil
}

// AssetName is the archive goreleaser makes for a platform, for example
// groot_0.6.0_darwin_arm64.tar.gz. version has no leading v.
func AssetName(version, goos, goarch string) string {
	return fmt.Sprintf("groot_%s_%s_%s.tar.gz", version, goos, goarch)
}

// semver is a parsed MAJOR.MINOR.PATCH[-pre] version.
type semver struct {
	nums [3]int
	pre  []string
}

func parseVersion(v string) (semver, bool) {
	v = strings.TrimPrefix(v, "v")
	if i := strings.IndexByte(v, '+'); i >= 0 {
		v = v[:i]
	}
	core, pre, hasPre := strings.Cut(v, "-")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return semver{}, false
	}
	var s semver
	for i, p := range parts {
		n, ok := digits(p)
		if !ok {
			return semver{}, false
		}
		s.nums[i] = n
	}
	if hasPre {
		ids := strings.Split(pre, ".")
		for _, id := range ids {
			if id == "" {
				return semver{}, false
			}
		}
		s.pre = ids
	}
	return s, true
}

func digits(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	n, err := strconv.Atoi(s)
	return n, err == nil
}

// compare returns -1, 0 or 1. A release outranks its own pre-release.
func compare(a, b semver) int {
	for i := range a.nums {
		if a.nums[i] != b.nums[i] {
			if a.nums[i] < b.nums[i] {
				return -1
			}
			return 1
		}
	}
	switch {
	case len(a.pre) == 0 && len(b.pre) == 0:
		return 0
	case len(a.pre) == 0:
		return 1
	case len(b.pre) == 0:
		return -1
	}
	for i := 0; i < len(a.pre) && i < len(b.pre); i++ {
		if c := comparePreID(a.pre[i], b.pre[i]); c != 0 {
			return c
		}
	}
	switch {
	case len(a.pre) < len(b.pre):
		return -1
	case len(a.pre) > len(b.pre):
		return 1
	}
	return 0
}

func comparePreID(a, b string) int {
	na, aNum := digits(a)
	nb, bNum := digits(b)
	switch {
	case aNum && bNum:
		if na != nb {
			if na < nb {
				return -1
			}
			return 1
		}
		return 0
	case aNum:
		return -1
	case bNum:
		return 1
	}
	return strings.Compare(a, b)
}

// Newer reports whether latest is a newer version than current. Invalid
// versions are never newer.
func Newer(current, latest string) bool {
	c, okC := parseVersion(current)
	l, okL := parseVersion(latest)
	if !okC || !okL {
		return false
	}
	return compare(l, c) > 0
}

// Apply downloads the archive of rel for goos and goarch, checks it against
// checksums.txt, and replaces exePath with the groot binary inside it. Nothing
// is written until the checksum matches. It returns the installed version.
func Apply(ctx context.Context, client *http.Client, rel Release, exePath, goos, goarch, token string) (string, error) {
	archName := AssetName(rel.Version, goos, goarch)
	archive, ok := rel.asset(archName)
	if !ok {
		return "", fmt.Errorf("release v%s has no %s for %s/%s; upgrade by hand from the release page", rel.Version, archName, goos, goarch)
	}
	sums, ok := rel.asset(ChecksumsName)
	if !ok {
		return "", fmt.Errorf("release v%s has no %s", rel.Version, ChecksumsName)
	}

	sumBody, err := fetch(ctx, client, sums, token, maxChecksums)
	if err != nil {
		return "", err
	}
	want, err := checksumFor(sumBody, archName)
	if err != nil {
		return "", err
	}
	body, err := fetch(ctx, client, archive, token, maxArchive)
	if err != nil {
		return "", err
	}
	if got := sha256Of(body); got != want {
		return "", fmt.Errorf("checksum mismatch for %s: want %s, got %s; nothing was changed", archName, want, got)
	}
	bin, err := extractBinary(body)
	if err != nil {
		return "", fmt.Errorf("extract %s: %w", archName, err)
	}
	if err := replaceFile(exePath, bin); err != nil {
		return "", err
	}
	return rel.Version, nil
}

func (r Release) asset(name string) (Asset, bool) {
	for _, a := range r.Assets {
		if a.Name == name {
			return a, true
		}
	}
	return Asset{}, false
}

// fetch downloads an asset from its public download url without a token. A 403
// or 404 is retried once through the API url with the token, when one is set.
func fetch(ctx context.Context, client *http.Client, a Asset, token string, limit int64) ([]byte, error) {
	var resp *http.Response
	var err error
	if a.BrowserURL != "" {
		resp, err = getOnce(ctx, client, a.BrowserURL, "", "")
		if err != nil {
			return nil, fmt.Errorf("download %s: %w", a.Name, err)
		}
	}
	if (resp == nil || retryable(resp.StatusCode)) && token != "" && a.URL != "" {
		if resp != nil {
			resp.Body.Close()
		}
		resp, err = getOnce(ctx, client, a.URL, "application/octet-stream", token)
		if err != nil {
			return nil, fmt.Errorf("download %s: %w", a.Name, err)
		}
	}
	if resp == nil {
		return nil, fmt.Errorf("asset %s has no download url", a.Name)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("download %s: %w", a.Name, ErrNotFound)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: %s", a.Name, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", a.Name, err)
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("download %s: larger than %d bytes", a.Name, limit)
	}
	return body, nil
}

// checksumFor finds the sha256 of name in checksums.txt, whose lines are
// "<sha256>  <name>".
func checksumFor(sums []byte, name string) (string, error) {
	sc := bufio.NewScanner(bytes.NewReader(sums))
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == name {
			return strings.ToLower(fields[0]), nil
		}
	}
	if err := sc.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("%s has no checksum for %s", ChecksumsName, name)
}

// extractBinary returns the groot file of a tar.gz archive.
func extractBinary(archive []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil, errors.New("no groot binary in the archive")
		}
		if err != nil {
			return nil, err
		}
		if hdr.Typeflag != tar.TypeReg || path.Base(hdr.Name) != "groot" {
			continue
		}
		var buf bytes.Buffer
		if _, err := io.Copy(&buf, io.LimitReader(tr, maxBinary+1)); err != nil {
			return nil, err
		}
		if buf.Len() > maxBinary {
			return nil, errors.New("groot binary in the archive is too large")
		}
		return buf.Bytes(), nil
	}
}

// replaceFile writes data next to exePath with the same permissions and renames
// it over exePath, which is atomic on one filesystem. The temporary file is
// removed on every failure.
func replaceFile(exePath string, data []byte) (err error) {
	info, err := os.Stat(exePath)
	if err != nil {
		return fmt.Errorf("replace %s: %w", exePath, err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(exePath), ".groot-upgrade-*")
	if err != nil {
		return permissionError(exePath, err)
	}
	tmpName := tmp.Name()
	defer func() {
		if err != nil {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err = tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write %s: %w", tmpName, err)
	}
	if err = tmp.Chmod(info.Mode().Perm()); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod %s: %w", tmpName, err)
	}
	if err = tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync %s: %w", tmpName, err)
	}
	if err = tmp.Close(); err != nil {
		return fmt.Errorf("close %s: %w", tmpName, err)
	}
	if err = os.Rename(tmpName, exePath); err != nil {
		return permissionError(exePath, err)
	}
	return nil
}

func permissionError(exePath string, err error) error {
	if errors.Is(err, fs.ErrPermission) {
		return fmt.Errorf("cannot replace %s: permission denied; run groot upgrade with sudo, or install groot under ~/.local/bin and upgrade that copy (%w)", exePath, err)
	}
	return fmt.Errorf("replace %s: %w", exePath, err)
}

// sha256Of returns the lowercase hex sha256 of data.
func sha256Of(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
