package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// testToken is the optional token the fake GitHub accepts.
const testToken = "s3cret"

// mode is how the fake GitHub treats a request that carries no token.
type mode int

const (
	// modePublic answers every request without a token.
	modePublic mode = iota
	// modeGated answers 404 to every request without a token, as a repository
	// that only a signed-in reader can see does.
	modeGated
	// modeLimited answers 403 to the release API without a token, as an
	// exhausted anonymous rate limit does. Downloads stay public.
	modeLimited
)

// request is one call the fake GitHub received.
type request struct {
	path   string
	authed bool
}

// server is a fake GitHub release API.
type server struct {
	*httptest.Server
	mu   sync.Mutex
	reqs []request
}

func (s *server) log() []request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]request(nil), s.reqs...)
}

func (s *server) requested() []string {
	var paths []string
	for _, r := range s.log() {
		paths = append(paths, r.path)
	}
	return paths
}

// newFakeGitHub serves one release of version with the given assets, in mode m.
// The asset api urls answer only with testToken.
func newFakeGitHub(t *testing.T, m mode, version string, files map[string][]byte) *server {
	t.Helper()
	s := &server{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authed := r.Header.Get("Authorization") == "Bearer "+testToken
		s.mu.Lock()
		s.reqs = append(s.reqs, request{path: r.URL.Path, authed: authed})
		s.mu.Unlock()
		send := func(name string) {
			body, ok := files[name]
			if !ok {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write(body)
		}
		gated := m == modeGated && !authed
		switch {
		case r.URL.Path == "/repos/tinchoram/groot-cli/releases/latest":
			if gated {
				http.NotFound(w, r)
				return
			}
			if m == modeLimited && !authed {
				http.Error(w, "rate limit exceeded", http.StatusForbidden)
				return
			}
			var assets []map[string]any
			for name, body := range files {
				assets = append(assets, map[string]any{
					"name":                 name,
					"url":                  s.URL + "/assets/" + name,
					"browser_download_url": s.URL + "/dl/" + name,
					"size":                 len(body),
				})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"tag_name": "v" + version, "assets": assets})
		case strings.HasPrefix(r.URL.Path, "/dl/"):
			if gated {
				http.NotFound(w, r)
				return
			}
			send(strings.TrimPrefix(r.URL.Path, "/dl/"))
		case strings.HasPrefix(r.URL.Path, "/assets/"):
			if !authed || r.Header.Get("Accept") != "application/octet-stream" {
				http.NotFound(w, r)
				return
			}
			send(strings.TrimPrefix(r.URL.Path, "/assets/"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(s.Close)
	old := APIBase
	APIBase = s.URL
	t.Cleanup(func() { APIBase = old })
	return s
}

// newReleaseServer is a public release: everything answers without a token.
func newReleaseServer(t *testing.T, version string, files map[string][]byte) *server {
	t.Helper()
	return newFakeGitHub(t, modePublic, version, files)
}

func targz(t *testing.T, name string, body []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// sumLine is one line of checksums.txt for the archive, in goreleaser's format.
func sumLine(archive []byte, name string) string {
	return fmt.Sprintf("%s  %s\n", sha256Of(archive), name)
}

func TestNewerComparesSemverish(t *testing.T) {
	cases := []struct {
		current, latest string
		want            bool
	}{
		{"0.5.4-dev", "0.5.4", true},
		{"0.5.4", "0.5.4-dev", false},
		{"0.5.4", "0.5.10", true},
		{"0.5.10", "0.5.4", false},
		{"0.5.4", "0.5.4", false},
		{"0.5.3", "v0.5.4", true},
		{"0.9.0", "1.0.0", true},
		{"0.5.4", "garbage", false},
		{"garbage", "0.5.4", false},
		{"0.5.4", "", false},
		{"0.5.4", "0.5", false},
	}
	for _, tc := range cases {
		if got := Newer(tc.current, tc.latest); got != tc.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", tc.current, tc.latest, got, tc.want)
		}
	}
}

func TestAssetNameMatchesTheGoreleaserArchive(t *testing.T) {
	if got := AssetName("0.5.4", "darwin", "arm64"); got != "groot_0.5.4_darwin_arm64.tar.gz" {
		t.Fatalf("AssetName = %q", got)
	}
}

func TestLatestReadsTheReleaseWithTheUserAgent(t *testing.T) {
	var gotAccept, gotUA string
	s := newReleaseServer(t, "0.6.0", map[string][]byte{"checksums.txt": []byte("x")})
	inner := s.Config.Handler
	s.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/tinchoram/groot-cli/releases/latest" {
			gotAccept, gotUA = r.Header.Get("Accept"), r.Header.Get("User-Agent")
		}
		inner.ServeHTTP(w, r)
	})

	rel, err := Latest(context.Background(), NewClient("9.9.9"), Repo, "")
	if err != nil {
		t.Fatal(err)
	}
	if rel.Tag != "v0.6.0" || rel.Version != "0.6.0" {
		t.Fatalf("release = %+v", rel)
	}
	if len(rel.Assets) != 1 || rel.Assets[0].Name != "checksums.txt" || rel.Assets[0].Size != 1 {
		t.Fatalf("assets = %+v", rel.Assets)
	}
	if gotAccept != jsonAccept {
		t.Errorf("Accept = %q", gotAccept)
	}
	if gotUA != "groot/9.9.9" {
		t.Errorf("User-Agent = %q", gotUA)
	}
}

func TestLatestWithATokenNeverSendsItOnA200(t *testing.T) {
	s := newReleaseServer(t, "0.6.0", nil)

	if _, err := Latest(context.Background(), NewClient("9.9.9"), Repo, testToken); err != nil {
		t.Fatal(err)
	}
	reqs := s.log()
	if len(reqs) != 1 || reqs[0].authed {
		t.Fatalf("a 200 must be answered by one unauthenticated request, got %+v", reqs)
	}
}

func TestLatestRetriesWithTheTokenAfterARateLimit(t *testing.T) {
	s := newFakeGitHub(t, modeLimited, "0.6.0", nil)

	rel, err := Latest(context.Background(), NewClient("9.9.9"), Repo, testToken)
	if err != nil {
		t.Fatalf("the token retry should succeed: %v", err)
	}
	if rel.Version != "0.6.0" {
		t.Fatalf("release = %+v", rel)
	}
	reqs := s.log()
	if len(reqs) != 2 || reqs[0].authed || !reqs[1].authed {
		t.Fatalf("want one unauthenticated request then one with the token, got %+v", reqs)
	}
}

func TestLatestRetriesWithTheTokenAfterA404(t *testing.T) {
	s := newFakeGitHub(t, modeGated, "0.6.0", nil)

	if _, err := Latest(context.Background(), NewClient("9.9.9"), Repo, testToken); err != nil {
		t.Fatalf("the token retry should succeed: %v", err)
	}
	reqs := s.log()
	if len(reqs) != 2 || reqs[0].authed || !reqs[1].authed {
		t.Fatalf("want one unauthenticated request then one with the token, got %+v", reqs)
	}
}

func TestLatestWithoutATokenReportsAMissingRelease(t *testing.T) {
	newFakeGitHub(t, modeGated, "0.6.0", nil)

	_, err := Latest(context.Background(), NewClient("9.9.9"), Repo, "")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	const want = "release not found: check the version and https://github.com/tinchoram/groot-cli/releases"
	if err.Error() != want {
		t.Fatalf("message = %q, want %q", err, want)
	}
}

func TestLatestWithoutATokenNamesTheOptionalTokenOnARateLimit(t *testing.T) {
	newFakeGitHub(t, modeLimited, "0.6.0", nil)

	_, err := Latest(context.Background(), NewClient("9.9.9"), Repo, "")
	if err == nil || !strings.Contains(err.Error(), "GITHUB_TOKEN (optional)") {
		t.Fatalf("a rate limit should name the optional GITHUB_TOKEN, got %v", err)
	}
}

func TestTokenReadsGithubTokenBeforeGhToken(t *testing.T) {
	env := map[string]string{"GH_TOKEN": "gh", "GITHUB_TOKEN": "github"}
	getenv := func(k string) string { return env[k] }
	if got := Token(getenv); got != "github" {
		t.Fatalf("Token = %q, want GITHUB_TOKEN first", got)
	}
	delete(env, "GITHUB_TOKEN")
	if got := Token(getenv); got != "gh" {
		t.Fatalf("Token = %q, want GH_TOKEN as fallback", got)
	}
}

func TestApplyReplacesTheBinaryAfterTheChecksumMatches(t *testing.T) {
	archName := AssetName("0.6.0", "darwin", "arm64")
	archive := targz(t, "groot", []byte("new binary"))
	s := newReleaseServer(t, "0.6.0", map[string][]byte{
		archName:      archive,
		ChecksumsName: []byte(sumLine(archive, archName)),
	})
	rel, err := Latest(context.Background(), NewClient("0.5.4-dev"), Repo, testToken)
	if err != nil {
		t.Fatal(err)
	}

	exe := filepath.Join(t.TempDir(), "groot")
	if err := os.WriteFile(exe, []byte("old binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := Apply(context.Background(), NewClient("0.5.4-dev"), rel, exe, "darwin", "arm64", testToken)
	if err != nil {
		t.Fatal(err)
	}
	if got != "0.6.0" {
		t.Fatalf("Apply returned %q, want the installed version", got)
	}
	body, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "new binary" {
		t.Fatalf("binary = %q", body)
	}
	fi, err := os.Stat(exe)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o755 {
		t.Fatalf("mode = %v, want 0755", fi.Mode().Perm())
	}
	if !contains(s.requested(), "/dl/"+archName) {
		t.Fatalf("the public archive url should serve a public release: %v", s.requested())
	}
	for _, r := range s.log() {
		if r.authed {
			t.Fatalf("a public download must not send the token: %+v", s.log())
		}
	}
}

func TestApplyRetriesTheAssetAPIWithTheTokenWhenTheDownloadIsGated(t *testing.T) {
	archName := AssetName("0.6.0", "darwin", "arm64")
	archive := targz(t, "groot", []byte("new binary"))
	s := newFakeGitHub(t, modeGated, "0.6.0", map[string][]byte{
		archName:      archive,
		ChecksumsName: []byte(sumLine(archive, archName)),
	})
	rel, err := Latest(context.Background(), NewClient("0.5.4-dev"), Repo, testToken)
	if err != nil {
		t.Fatal(err)
	}

	exe := filepath.Join(t.TempDir(), "groot")
	if err := os.WriteFile(exe, []byte("old binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(context.Background(), NewClient("0.5.4-dev"), rel, exe, "darwin", "arm64", testToken); err != nil {
		t.Fatal(err)
	}
	if !contains(s.requested(), "/assets/"+archName) {
		t.Fatalf("a refused download must retry through the asset api url: %v", s.requested())
	}
}

func TestApplyWithoutATokenReportsAGatedDownloadAsMissing(t *testing.T) {
	archName := AssetName("0.6.0", "darwin", "arm64")
	archive := targz(t, "groot", []byte("new binary"))
	newFakeGitHub(t, modeGated, "0.6.0", map[string][]byte{
		archName:      archive,
		ChecksumsName: []byte(sumLine(archive, archName)),
	})
	rel, err := Latest(context.Background(), NewClient("0.5.4-dev"), Repo, testToken)
	if err != nil {
		t.Fatal(err)
	}

	exe := filepath.Join(t.TempDir(), "groot")
	_, err = Apply(context.Background(), NewClient("0.5.4-dev"), rel, exe, "darwin", "arm64", "")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestApplyRefusesAChecksumMismatchAndKeepsTheBinary(t *testing.T) {
	archName := AssetName("0.6.0", "darwin", "arm64")
	archive := targz(t, "groot", []byte("new binary"))
	wrong := fmt.Sprintf("%064d  %s\n", 0, archName)
	newReleaseServer(t, "0.6.0", map[string][]byte{
		archName:      archive,
		ChecksumsName: []byte(wrong),
	})
	rel, err := Latest(context.Background(), NewClient("0.5.4-dev"), Repo, testToken)
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	exe := filepath.Join(dir, "groot")
	if err := os.WriteFile(exe, []byte("old binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err = Apply(context.Background(), NewClient("0.5.4-dev"), rel, exe, "darwin", "arm64", testToken)
	if err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("err = %v, want a checksum error", err)
	}
	body, _ := os.ReadFile(exe)
	if string(body) != "old binary" {
		t.Fatalf("a failed upgrade changed the binary: %q", body)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("a failed upgrade left files behind: %d entries", len(entries))
	}
}

func TestApplyNamesAMissingPlatformArchive(t *testing.T) {
	archive := targz(t, "groot", []byte("linux binary"))
	name := AssetName("0.6.0", "linux", "amd64")
	newReleaseServer(t, "0.6.0", map[string][]byte{
		name:          archive,
		ChecksumsName: []byte(sumLine(archive, name)),
	})
	rel, err := Latest(context.Background(), NewClient("0.5.4-dev"), Repo, testToken)
	if err != nil {
		t.Fatal(err)
	}

	exe := filepath.Join(t.TempDir(), "groot")
	_, err = Apply(context.Background(), NewClient("0.5.4-dev"), rel, exe, "darwin", "arm64", testToken)
	if err == nil || !strings.Contains(err.Error(), "darwin_arm64") {
		t.Fatalf("err = %v, want the missing platform named", err)
	}
}

func TestApplyPermissionErrorNamesThePathAndSudo(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	archName := AssetName("0.6.0", "darwin", "arm64")
	archive := targz(t, "groot", []byte("new binary"))
	newReleaseServer(t, "0.6.0", map[string][]byte{
		archName:      archive,
		ChecksumsName: []byte(sumLine(archive, archName)),
	})
	rel, err := Latest(context.Background(), NewClient("0.5.4-dev"), Repo, testToken)
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	exe := filepath.Join(dir, "groot")
	if err := os.WriteFile(exe, []byte("old binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	_, err = Apply(context.Background(), NewClient("0.5.4-dev"), rel, exe, "darwin", "arm64", testToken)
	if err == nil {
		t.Fatal("expected a permission error")
	}
	if !strings.Contains(err.Error(), exe) || !strings.Contains(err.Error(), "sudo") || !strings.Contains(err.Error(), "~/.local/bin") {
		t.Fatalf("message should name the path, sudo and ~/.local/bin: %q", err)
	}
}

func TestCheckCallsTheAPIAtMostOncePerDay(t *testing.T) {
	archName := AssetName("0.6.0", "darwin", "arm64")
	s := newReleaseServer(t, "0.6.0", map[string][]byte{archName: []byte("x")})
	home := t.TempDir()
	now := time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC)
	opt := Options{Repo: Repo, Token: testToken, Now: func() time.Time { return now }}

	res := Check(context.Background(), home, "0.5.4-dev", opt)
	if !res.Newer || res.Latest != "0.6.0" {
		t.Fatalf("first check = %+v", res)
	}
	if _, err := os.Stat(filepath.Join(home, CacheFile)); err != nil {
		t.Fatalf("the check must record the cache file: %v", err)
	}

	now = now.Add(time.Hour)
	res = Check(context.Background(), home, "0.5.4-dev", opt)
	if !res.Newer || res.Latest != "0.6.0" {
		t.Fatalf("cached check = %+v", res)
	}
	if n := countLatest(s); n != 1 {
		t.Fatalf("within 24h the API was called %d times, want 1", n)
	}

	now = now.Add(25 * time.Hour)
	Check(context.Background(), home, "0.5.4-dev", opt)
	if n := countLatest(s); n != 2 {
		t.Fatalf("after 24h the API was called %d times, want 2", n)
	}
}

func TestCheckSwallowsErrorsAndKnownReportsThem(t *testing.T) {
	newFakeGitHub(t, modeGated, "0.6.0", nil)
	home := t.TempDir()

	res := Check(context.Background(), home, "0.5.4-dev", Options{Repo: Repo, Token: ""})
	if res.Newer {
		t.Fatalf("a missing release must show nothing: %+v", res)
	}
	st := Known(home, "0.5.4-dev")
	if st.Newer || !strings.Contains(st.Reason, "release not found") {
		t.Fatalf("Known after a failed check = %+v", st)
	}
}

func TestKnownReadsOnlyTheCache(t *testing.T) {
	home := t.TempDir()
	if st := Known(home, "0.5.4-dev"); st.Reason != "not checked yet" || st.Newer {
		t.Fatalf("Known without a cache = %+v", st)
	}
	if err := writeCache(home, Cache{CheckedAt: time.Now(), Latest: "99.0.0"}); err != nil {
		t.Fatal(err)
	}
	st := Known(home, "0.5.4-dev")
	if !st.Newer || st.Latest != "99.0.0" {
		t.Fatalf("Known with a newer cached release = %+v", st)
	}
	if err := writeCache(home, Cache{CheckedAt: time.Now(), Latest: "0.5.4-dev"}); err != nil {
		t.Fatal(err)
	}
	if st := Known(home, "0.5.4-dev"); st.Newer || st.Reason != "" {
		t.Fatalf("Known when up to date = %+v", st)
	}
}

func countLatest(s *server) int {
	n := 0
	for _, p := range s.requested() {
		if p == "/repos/tinchoram/groot-cli/releases/latest" {
			n++
		}
	}
	return n
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
