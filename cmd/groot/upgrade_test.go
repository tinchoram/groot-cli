package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tinchoram/groot-cli/internal/update"
)

const upgradeTestToken = "s3cret"

// fakeReleases serves a public latest release: every request answers without a token.
func fakeReleases(t *testing.T, version string, files map[string][]byte) {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/tinchoram/groot-cli/releases/latest":
			assets := []map[string]any{}
			for name, body := range files {
				assets = append(assets, map[string]any{
					"name":                 name,
					"url":                  srv.URL + "/assets/" + name,
					"browser_download_url": srv.URL + "/dl/" + name,
					"size":                 len(body),
				})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"tag_name": "v" + version, "assets": assets})
		case strings.HasPrefix(r.URL.Path, "/assets/"):
			body, ok := files[strings.TrimPrefix(r.URL.Path, "/assets/")]
			if !ok || r.Header.Get("Accept") != "application/octet-stream" {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write(body)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	old := update.APIBase
	update.APIBase = srv.URL
	t.Cleanup(func() { update.APIBase = old })
}

// releaseArchive is a tar.gz with a groot binary, and the checksums.txt line for it.
func releaseArchive(t *testing.T, name string, binary []byte) (archive []byte, sums string) {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "groot", Mode: 0o755, Size: int64(len(binary)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(binary); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(buf.Bytes())
	return buf.Bytes(), fmt.Sprintf("%s  %s\n", hex.EncodeToString(sum[:]), name)
}

func upgradeEnvFor(t *testing.T, version, exe string) upgradeEnv {
	t.Helper()
	return upgradeEnv{
		Version: version,
		Getenv: func(k string) string {
			if k == "GITHUB_TOKEN" {
				return upgradeTestToken
			}
			return ""
		},
		Client: update.NewClient(version),
		Exe:    func() (string, error) { return exe, nil },
		Goos:   "linux",
		Goarch: "amd64",
	}
}

func TestUpgradeCheckReportsANewerRelease(t *testing.T) {
	fakeReleases(t, "0.6.0", nil)
	var out, errOut bytes.Buffer
	env := upgradeEnvFor(t, "0.5.4-dev", "")

	if code := runUpgrade([]string{"--check"}, &out, &errOut, env); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	for _, want := range []string{"current: v0.5.4-dev", "latest: v0.6.0", "update available"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("output lacks %q:\n%s", want, out.String())
		}
	}
}

func TestUpgradeSaysWhenAlreadyLatest(t *testing.T) {
	fakeReleases(t, "0.6.0", nil)
	var out, errOut bytes.Buffer

	if code := runUpgrade(nil, &out, &errOut, upgradeEnvFor(t, "0.6.0", filepath.Join(t.TempDir(), "groot"))); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "groot v0.6.0 is the latest") {
		t.Fatalf("output:\n%s", out.String())
	}
}

func TestUpgradeReportsAMissingRelease(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	old := update.APIBase
	update.APIBase = srv.URL
	t.Cleanup(func() { update.APIBase = old })

	var out, errOut bytes.Buffer
	env := upgradeEnvFor(t, "0.5.4-dev", "")
	env.Getenv = func(string) string { return "" }

	if code := runUpgrade([]string{"--check"}, &out, &errOut, env); code != 1 {
		t.Fatalf("a missing release must fail: exit %d", code)
	}
	if !strings.Contains(errOut.String(), "release not found") {
		t.Fatalf("stderr should say the release was not found: %q", errOut.String())
	}
}

func TestUpgradeReplacesTheRunningBinary(t *testing.T) {
	name := update.AssetName("0.6.0", "linux", "amd64")
	archive, sums := releaseArchive(t, name, []byte("new groot"))
	fakeReleases(t, "0.6.0", map[string][]byte{
		name:                 archive,
		update.ChecksumsName: []byte(sums),
	})
	exe := filepath.Join(t.TempDir(), "groot")
	if err := os.WriteFile(exe, []byte("old groot"), 0o755); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer

	if code := runUpgrade(nil, &out, &errOut, upgradeEnvFor(t, "0.5.4-dev", exe)); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "upgraded to v0.6.0; restart groot") {
		t.Fatalf("output:\n%s", out.String())
	}
	body, _ := os.ReadFile(exe)
	if string(body) != "new groot" {
		t.Fatalf("binary = %q", body)
	}
}
