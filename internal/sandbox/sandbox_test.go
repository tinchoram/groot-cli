package sandbox

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
)

// fakeHost returns a host whose binaries are the names and paths in present,
// and whose /var is a symlink to /private/var, as on macOS.
func fakeHost(goos, home string, present ...string) host {
	return host{
		goos: goos,
		home: home,
		tmp:  []string{"/var/folders/ab/T"},
		lookPath: func(name string) (string, error) {
			if slices.Contains(present, name) {
				return "/usr/bin/" + name, nil
			}
			return "", errors.New("not found")
		},
		exists: func(p string) bool { return slices.Contains(present, p) },
		// .netrc is the only file among the secret paths; everything else is a directory.
		isDir: func(p string) bool {
			return slices.Contains(present, p) && !strings.HasSuffix(p, ".netrc")
		},
		resolve: func(p string) string {
			if p == "/var" || strings.HasPrefix(p, "/var/") {
				return "/private" + p
			}
			return p
		},
	}
}

// The secret list is one exported slice shared by both backends and the docs.
func TestSecretDirsIsTheSharedList(t *testing.T) {
	want := []string{".ssh", ".aws", ".gnupg", ".groot", ".kube", ".netrc", ".docker", ".config/gh", ".config/gcloud", ".azure"}
	if !slices.Equal(SecretDirs, want) {
		t.Fatalf("SecretDirs = %q, want %q", SecretDirs, want)
	}
}

func TestDarwinDeniesEverySecretDir(t *testing.T) {
	p := profileDarwin(Spec{Dir: "/Users/u/proj", WritablePaths: []string{"/Users/u/proj"}}, fakeHost("darwin", "/Users/u"))
	allow := strings.Index(p, "(allow file-read*)")
	for _, d := range SecretDirs {
		want := `(subpath "/Users/u/` + d + `")`
		i := strings.Index(p, want)
		if i < 0 {
			t.Errorf("profile does not deny %s:\n%s", d, p)
			continue
		}
		if i < allow {
			t.Errorf("deny for %s comes before the broad read allow:\n%s", d, p)
		}
	}
}

func TestBwrapMasksEverySecretPathThatExists(t *testing.T) {
	var present []string
	for _, d := range SecretDirs {
		present = append(present, "/home/u/"+d)
	}
	present = append(present, "/home/u/proj", "/private/var/folders/ab/T")
	args := bwrapArgs(Spec{Dir: "/home/u/proj", WritablePaths: []string{"/home/u/proj"}}, fakeHost("linux", "/home/u", present...))
	for _, d := range SecretDirs {
		p := "/home/u/" + d
		if d == ".netrc" {
			if !slices.Contains(args, "/dev/null") || !containsSeq(args, "--ro-bind", "/dev/null", p) {
				t.Errorf("file %s must be masked with /dev/null: %q", p, args)
			}
			continue
		}
		if !containsSeq(args, "--tmpfs", p) {
			t.Errorf("directory %s must be masked with tmpfs: %q", p, args)
		}
	}
}

func containsSeq(args []string, seq ...string) bool {
	for i := 0; i+len(seq) <= len(args); i++ {
		if slices.Equal(args[i:i+len(seq)], seq) {
			return true
		}
	}
	return false
}

func TestDarwinProfileTable(t *testing.T) {
	h := fakeHost("darwin", "/Users/u")
	cases := []struct {
		name    string
		spec    Spec
		want    []string
		notWant []string
	}{
		{
			name: "space and tmp are writable, secret dirs are denied",
			spec: Spec{Dir: "/Users/u/proj", WritablePaths: []string{"/Users/u/proj"}},
			want: []string{
				`(allow file-write* (subpath "/Users/u/proj") (subpath "/private/var/folders/ab/T")`,
				`(deny file-read* file-write* (subpath "/Users/u/.ssh")`,
				`(subpath "/Users/u/.aws")`,
				`(subpath "/Users/u/.gnupg")`,
				`(subpath "/Users/u/.groot")`,
				`(deny file-write* (subpath "/Users/u/proj/.groot"))`,
				`(deny network*)`,
			},
			notWant: []string{`"/var/folders`, `(allow network*)`},
		},
		{
			name:    "network allowed",
			spec:    Spec{Dir: "/Users/u/proj", WritablePaths: []string{"/Users/u/proj"}, AllowNetwork: true},
			want:    []string{`(allow network*)`},
			notWant: []string{`(deny network*)`},
		},
		{
			name: "extra writable paths are added",
			spec: Spec{Dir: "/Users/u/proj", WritablePaths: []string{"/Users/u/proj", "/opt/cache"}},
			want: []string{`(subpath "/opt/cache")`},
		},
		{
			name: "quotes and backslashes in paths are escaped",
			spec: Spec{Dir: `/Users/u/my "proj"\x`, WritablePaths: []string{`/Users/u/my "proj"\x`}},
			want: []string{`(subpath "/Users/u/my \"proj\"\\x")`},
		},
		{
			name:    "a space under /var resolves to /private/var",
			spec:    Spec{Dir: "/var/work/proj", WritablePaths: []string{"/var/work/proj"}},
			want:    []string{`(subpath "/private/var/work/proj")`},
			notWant: []string{`"/var/work`},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := profileDarwin(tc.spec, h)
			if !strings.HasPrefix(p, "(version 1)\n(deny default)\n") {
				t.Fatalf("profile must start with version and deny default:\n%s", p)
			}
			for _, w := range tc.want {
				if !strings.Contains(p, w) {
					t.Errorf("profile lacks %q:\n%s", w, p)
				}
			}
			for _, w := range tc.notWant {
				if strings.Contains(p, w) {
					t.Errorf("profile must not contain %q:\n%s", w, p)
				}
			}
		})
	}
}

// The secret denies must come after the broad read allow: SBPL uses the last
// matching rule, so a deny placed before it would be overridden.
func TestDarwinSecretDenyComesAfterBroadRead(t *testing.T) {
	p := profileDarwin(Spec{Dir: "/Users/u/proj", WritablePaths: []string{"/Users/u/proj"}}, fakeHost("darwin", "/Users/u"))
	allow := strings.Index(p, "(allow file-read*)")
	deny := strings.Index(p, `(deny file-read* file-write* (subpath "/Users/u/.ssh")`)
	if allow < 0 || deny < 0 || deny < allow {
		t.Fatalf("secret deny must follow the read allow (allow=%d deny=%d):\n%s", allow, deny, p)
	}
}

func TestDarwinWrapRunsThroughSandboxExec(t *testing.T) {
	r := newSeatbelt(fakeHost("darwin", "/Users/u", "/usr/bin/sandbox-exec"))
	spec := Spec{Dir: "/Users/u/proj", Command: []string{"/bin/sh", "-c", "echo hi"}, WritablePaths: []string{"/Users/u/proj"}}
	cmd, err := r.Wrap(context.Background(), spec)
	if err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	if cmd.Path != "/usr/bin/sandbox-exec" {
		t.Fatalf("Path = %q", cmd.Path)
	}
	if len(cmd.Args) < 4 || cmd.Args[1] != "-p" || !strings.Contains(cmd.Args[2], "(deny default)") {
		t.Fatalf("args = %q, want -p <profile> before the command", cmd.Args)
	}
	if !slices.Equal(cmd.Args[3:], spec.Command) {
		t.Fatalf("command = %q, want %q", cmd.Args[3:], spec.Command)
	}
	if cmd.Dir != spec.Dir {
		t.Fatalf("Dir = %q, want %q", cmd.Dir, spec.Dir)
	}
}

func TestBwrapArgs(t *testing.T) {
	h := fakeHost("linux", "/home/u",
		"/home/u/.ssh", "/home/u/proj/.groot", "/home/u/proj", "/opt/cache", "/private/var/folders/ab/T")
	spec := Spec{Dir: "/home/u/proj", WritablePaths: []string{"/home/u/proj", "/opt/cache"}}
	args := bwrapArgs(spec, h)

	mustSeq := func(seq ...string) {
		t.Helper()
		for i := 0; i+len(seq) <= len(args); i++ {
			if slices.Equal(args[i:i+len(seq)], seq) {
				return
			}
		}
		t.Errorf("args lack %q:\n%q", seq, args)
	}
	mustSeq("--ro-bind", "/", "/")
	mustSeq("--bind", "/home/u/proj", "/home/u/proj")
	mustSeq("--bind", "/opt/cache", "/opt/cache")
	mustSeq("--bind", "/private/var/folders/ab/T", "/private/var/folders/ab/T")
	mustSeq("--dev", "/dev")
	mustSeq("--proc", "/proc")
	mustSeq("--unshare-net")
	mustSeq("--die-with-parent")
	mustSeq("--new-session")
	mustSeq("--tmpfs", "/home/u/.ssh")
	mustSeq("--ro-bind", "/home/u/proj/.groot", "/home/u/proj/.groot")

	if slices.Contains(args, "/home/u/.aws") {
		t.Errorf("a secret dir that does not exist must not be mounted over: %q", args)
	}
	if slices.Contains(args, "--") {
		t.Errorf("bwrapArgs must not include the separator; Wrap appends it")
	}
}

func TestBwrapArgsNetworkAllowed(t *testing.T) {
	args := bwrapArgs(Spec{Dir: "/home/u/proj", WritablePaths: []string{"/home/u/proj"}, AllowNetwork: true}, fakeHost("linux", "/home/u"))
	if slices.Contains(args, "--unshare-net") {
		t.Fatalf("network allowed must not unshare the net namespace: %q", args)
	}
}

func TestDetectPicksTheAvailableBackend(t *testing.T) {
	cases := []struct {
		name     string
		h        host
		wantName string
	}{
		{"darwin uses sandbox-exec", fakeHost("darwin", "/Users/u", "/usr/bin/sandbox-exec"), BackendSandboxExec},
		{"linux uses bwrap", fakeHost("linux", "/home/u", "bwrap"), BackendBwrap},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, w := detect(tc.h, Config{Mode: "auto"})
			if r.Name() != tc.wantName {
				t.Fatalf("Name = %q, want %q", r.Name(), tc.wantName)
			}
			if w != "" {
				t.Fatalf("a real backend must not warn, got %q", w)
			}
		})
	}
}

func TestDetectFallsBackToNoneWithWarning(t *testing.T) {
	cases := []struct {
		name string
		h    host
		want string
	}{
		{"linux without bwrap", fakeHost("linux", "/home/u"), "apt install bubblewrap"},
		{"darwin without sandbox-exec", fakeHost("darwin", "/Users/u"), "xcode-select --install"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, w := detect(tc.h, Config{Mode: "auto"})
			if r.Name() != BackendNone {
				t.Fatalf("Name = %q, want none", r.Name())
			}
			if !strings.Contains(string(w), tc.want) {
				t.Fatalf("warning %q should name what to install (%q)", w, tc.want)
			}
		})
	}
}

func TestDetectOffNeverWrapsAndDoesNotWarn(t *testing.T) {
	r, w := detect(fakeHost("linux", "/home/u"), Config{Mode: "off"})
	if r.Name() != BackendOff {
		t.Fatalf("Name = %q, want off", r.Name())
	}
	if w != "" {
		t.Fatalf("off must not warn, got %q", w)
	}
	cmd, err := r.Wrap(context.Background(), Spec{Dir: "/tmp", Command: []string{"/bin/true"}})
	if err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	if cmd.Path != "/bin/true" {
		t.Fatalf("off must run the command itself, got %q", cmd.Path)
	}
}

func TestStrictRefusesWithoutBackendAndNamesTheInstall(t *testing.T) {
	cases := []struct {
		name string
		h    host
		want string
	}{
		{"linux", fakeHost("linux", "/home/u"), "apt install bubblewrap"},
		{"darwin", fakeHost("darwin", "/Users/u"), "xcode-select --install"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, _ := detect(tc.h, Config{Mode: "strict"})
			_, err := r.Wrap(context.Background(), Spec{Dir: "/tmp", Command: []string{"/bin/true"}})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("strict Wrap error = %v, want it to name %q", err, tc.want)
			}
		})
	}
}

func TestStrictUsesTheBackendWhenPresent(t *testing.T) {
	r, w := detect(fakeHost("linux", "/home/u", "bwrap"), Config{Mode: "strict"})
	if r.Name() != BackendBwrap || w != "" {
		t.Fatalf("strict with bwrap: name=%q warning=%q", r.Name(), w)
	}
}
