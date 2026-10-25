package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tinchoram/groot-cli/internal/update"
)

// writeUpdateCache stores a check result the way update.Check does.
func writeUpdateCache(t *testing.T, home, body string) {
	t.Helper()
	writeTestFile(t, filepath.Join(home, update.CacheFile), body)
}

func freshCache(latest string) string {
	return `{"checked_at":"` + time.Now().UTC().Format(time.RFC3339) + `","latest":"` + latest + `"}`
}

func TestDoctorReportsAvailableUpdateFromTheCacheOnly(t *testing.T) {
	home := t.TempDir()
	writeUpdateCache(t, home, freshCache("99.0.0"))
	env := fakeDoctorEnv(t, home)
	env.EnvKey = "sk-ant-doctor-test-1234"

	out, _ := runDoctorWith(t, env)
	if l := lineWith(out, "update:"); !strings.Contains(l, "v99.0.0 available") {
		t.Fatalf("update line %q", l)
	}
}

func TestDoctorReportsUpToDate(t *testing.T) {
	home := t.TempDir()
	env := fakeDoctorEnv(t, home)
	env.EnvKey = "sk-ant-doctor-test-1234"
	writeUpdateCache(t, home, freshCache(env.Version))

	out, _ := runDoctorWith(t, env)
	if l := lineWith(out, "update:"); l != "update: up to date" {
		t.Fatalf("update line %q", l)
	}
}

func TestDoctorReportsUnknownWhenNeverChecked(t *testing.T) {
	env := fakeDoctorEnv(t, t.TempDir())
	env.EnvKey = "sk-ant-doctor-test-1234"

	out, code := runDoctorWith(t, env)
	if code != 0 {
		t.Fatalf("an unknown update is not a startup block: exit %d", code)
	}
	if l := lineWith(out, "update:"); l != "update: unknown (not checked yet)" {
		t.Fatalf("update line %q", l)
	}
}

func TestStartUpdateNoticeOffMeansNoChannel(t *testing.T) {
	if ch := startUpdateNotice(false, t.TempDir(), "0.5.4-dev", ""); ch != nil {
		t.Fatal("updates.check: false must start nothing")
	}
}

func TestStartUpdateNoticeDeliversANewerCachedVersion(t *testing.T) {
	home := t.TempDir()
	writeUpdateCache(t, home, freshCache("99.0.0"))

	ch := startUpdateNotice(true, home, "0.5.4-dev", "")
	select {
	case v, ok := <-ch:
		if !ok || v != "99.0.0" {
			t.Fatalf("notice = %q, ok %v", v, ok)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the notice never arrived")
	}
}

func TestStartUpdateNoticeSendsNothingWhenUpToDate(t *testing.T) {
	home := t.TempDir()
	writeUpdateCache(t, home, freshCache("0.5.3"))

	ch := startUpdateNotice(true, home, "0.5.4-dev", "")
	select {
	case v, ok := <-ch:
		if ok {
			t.Fatalf("up to date must send nothing, got %q", v)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the channel never closed")
	}
}
