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

// testToken is what the fake private repository accepts.
const testToken = "s3cret"

// server is a fake GitHub release API for a private repository: the API and
// the asset api urls answer only with testToken, and the public download urls
// always answer 404, as they do for a private repository.
type server struct {
	*httptest.Server
	mu    sync.Mutex
	paths []string
}

func (s *server) requested() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.paths...)
}

func newReleaseServer(t *testing.T, version string, files map[string][]byte) *server {
	t.Helper()
	s := &server{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.paths = append(s.paths, r.URL.Path)
		s.mu.Unlock()
		authed := r.Header.Get("Authorization") == "Bearer "+testToken
		switch {
		case r.URL.Path == "/repos/tinchoram/groot-cli/releases/latest":
			if !authed {
				http.NotFound(w, r)
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
		case strings.HasPrefix(r.URL.Path, "/assets/"):
			body, ok := files[strings.TrimPrefix(r.URL.Path, "/assets/")]
			if !ok || !authed || r.Header.Get("Accept") != "application/octet-stream" {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write(body)
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

func TestLatestReadsTheReleaseWithTheTokenAndUserAgent(t *testing.T) {
	var gotAccept, gotUA string
	s := newReleaseServer(t, "0.6.0", map[string][]byte{"checksums.txt": []byte("x")})
	// Wrap the handler to observe the headers of the latest call.
	inner := s.Config.Handler
	s.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/tinchoram/groot-cli/releases/latest" {
			gotAccept, gotUA = r.Header.Get("Accept"), r.Header.Get("User-Agent")
		}
		inner.ServeHTTP(w, r)
	})

	rel, err := Latest(context.Background(), NewClient("9.9.9"), Repo, testToken)
	if err != nil {
		t.Fatal(err)
	}
	if rel.Tag != "v0.6.0" || rel.Version != "0.6.0" {
		t.Fatalf("release = %+v", rel)
	}
	if len(rel.Assets) != 1 || rel.Assets[0].Name != "checksums.txt" || rel.Assets[0].Size != 1 {
		t.Fatalf("assets = %+v", rel.Assets)
	}
	if gotAccept != "application/vnd.github+json" {
		t.Errorf("Accept = %q", gotAccept)
	}
	if gotUA != "groot/9.9.9" {
		t.Errorf("User-Agent = %q", gotUA)
	}
}

func TestLatestWithoutATokenOnAPrivateRepoNeedsAToken(t *testing.T) {
	newReleaseServer(t, "0.6.0", nil)

	_, err := Latest(context.Background(), NewClient("9.9.9"), Repo, "")
	if !errors.Is(err, ErrNeedsToken) {
		t.Fatalf("err = %v, want ErrNeedsToken", err)
	}
	if !strings.Contains(err.Error(), "GITHUB_TOKEN") || !strings.Contains(err.Error(), "GH_TOKEN") {
		t.Fatalf("message should name both variables: %q", err)
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
	// Private assets are fetched through the API url with the token.
	if !contains(s.requested(), "/assets/"+archName) {
		t.Fatalf("the archive was not fetched through the asset api url: %v", s.requested())
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

func TestApplyWithoutATokenOnAPrivateRepoNeedsAToken(t *testing.T) {
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

	exe := filepath.Join(t.TempDir(), "groot")
	_, err = Apply(context.Background(), NewClient("0.5.4-dev"), rel, exe, "darwin", "arm64", "")
	if !errors.Is(err, ErrNeedsToken) {
		t.Fatalf("err = %v, want ErrNeedsToken", err)
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
	newReleaseServer(t, "0.6.0", nil)
	home := t.TempDir()

	res := Check(context.Background(), home, "0.5.4-dev", Options{Repo: Repo, Token: ""})
	if res.Newer {
		t.Fatalf("an offline or private check must show nothing: %+v", res)
	}
	st := Known(home, "0.5.4-dev")
	if st.Newer || !strings.Contains(st.Reason, "GITHUB_TOKEN") {
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
