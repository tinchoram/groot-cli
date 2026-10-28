package sandbox

import (
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestCredentialSetsAreTheNamedSecretDirs(t *testing.T) {
	want := map[string][]string{
		"aws":    {".aws"},
		"kube":   {".kube"},
		"ssh":    {".ssh"},
		"gcloud": {".config/gcloud"},
		"azure":  {".azure"},
		"docker": {".docker"},
		"gh":     {".config/gh"},
		"gnupg":  {".gnupg"},
		"netrc":  {".netrc"},
	}
	if !reflect.DeepEqual(CredentialSets, want) {
		t.Fatalf("CredentialSets = %v, want %v", CredentialSets, want)
	}
}

func TestEverySetEntryIsASecretDir(t *testing.T) {
	for name, dirs := range CredentialSets {
		for _, d := range dirs {
			if !slices.Contains(SecretDirs, d) {
				t.Errorf("set %s grants %s, which is not in SecretDirs", name, d)
			}
		}
	}
}

func TestGrootIsNeverGrantable(t *testing.T) {
	for name, dirs := range CredentialSets {
		if slices.Contains(dirs, ".groot") {
			t.Errorf("set %s grants .groot", name)
		}
	}
	if _, err := CredentialDirs([]string{".groot"}); err == nil {
		t.Fatal(".groot must not be accepted as a credential set")
	}
}

func TestCredentialDirsMapsNamesToDirs(t *testing.T) {
	got, err := CredentialDirs([]string{"kube", "aws", "aws"})
	if err != nil {
		t.Fatalf("CredentialDirs: %v", err)
	}
	if want := []string{".aws", ".kube"}; !slices.Equal(got, want) {
		t.Fatalf("CredentialDirs = %q, want %q (sorted, no duplicates)", got, want)
	}
	if got, err := CredentialDirs(nil); err != nil || len(got) != 0 {
		t.Fatalf("no sets must grant nothing, got %q, %v", got, err)
	}
}

func TestCredentialDirsNamesAnUnknownSet(t *testing.T) {
	_, err := CredentialDirs([]string{"aws", "vault"})
	if err == nil || !strings.Contains(err.Error(), "vault") || !strings.Contains(err.Error(), "aws") {
		t.Fatalf("error = %v, want it to name vault and list the known sets", err)
	}
}

func TestCredentialNamesAreSorted(t *testing.T) {
	names := CredentialNames()
	if !slices.IsSorted(names) || len(names) != len(CredentialSets) {
		t.Fatalf("CredentialNames = %q", names)
	}
}

func TestSeatbeltReadableSecretIsReadableNotWritable(t *testing.T) {
	h := fakeHost("darwin", "/Users/u")
	p := profileDarwin(Spec{
		Dir:             "/Users/u/proj",
		WritablePaths:   []string{"/Users/u/proj"},
		ReadableSecrets: []string{".aws"},
	}, h)

	denyRW := ""
	for _, line := range strings.Split(p, "\n") {
		if strings.HasPrefix(line, "(deny file-read* file-write* ") {
			denyRW = line
		}
	}
	if denyRW == "" {
		t.Fatalf("the read and write deny line is missing:\n%s", p)
	}
	if strings.Contains(denyRW, `"/Users/u/.aws"`) {
		t.Errorf("a readable set must leave the read deny line:\n%s", denyRW)
	}
	for _, d := range []string{".ssh", ".kube", ".groot", ".azure"} {
		if !strings.Contains(denyRW, `(subpath "/Users/u/`+d+`")`) {
			t.Errorf("%s must stay denied for reads and writes:\n%s", d, denyRW)
		}
	}

	writeDeny := `(deny file-write* (subpath "/Users/u/.aws"))`
	i := strings.Index(p, writeDeny)
	if i < 0 {
		t.Fatalf("the readable set needs an explicit write deny %q:\n%s", writeDeny, p)
	}
	if i < strings.Index(p, "(allow file-write* ") {
		t.Errorf("the write deny must come after the writable allow:\n%s", p)
	}
	if i < strings.Index(p, "(allow file-read*)") {
		t.Errorf("the write deny must not precede the read allow:\n%s", p)
	}
}

func TestSeatbeltWithoutSetsDeniesEverySecretForReadAndWrite(t *testing.T) {
	p := profileDarwin(Spec{Dir: "/Users/u/proj", WritablePaths: []string{"/Users/u/proj"}}, fakeHost("darwin", "/Users/u"))
	if strings.Contains(p, `(deny file-write* (subpath "/Users/u/.aws"))`) {
		t.Fatalf("no set is granted, so .aws needs no write-only rule:\n%s", p)
	}
	if !strings.Contains(denyRWLine(p), `(subpath "/Users/u/.aws")`) {
		t.Fatalf(".aws must be denied for reads and writes:\n%s", p)
	}
}

// denyRWLine returns the line that denies reads and writes of the secret dirs.
func denyRWLine(p string) string {
	for _, line := range strings.Split(p, "\n") {
		if strings.HasPrefix(line, "(deny file-read* file-write* ") {
			return line
		}
	}
	return ""
}

func TestSeatbeltProfileForAnAgentWithAwsCredentials(t *testing.T) {
	p := profileDarwin(Spec{
		Dir:             "/Users/u/proj",
		WritablePaths:   []string{"/Users/u/proj"},
		ReadableSecrets: []string{".aws"},
	}, fakeHost("darwin", "/Users/u"))
	want := []string{
		`(deny file-read* file-write* (subpath "/Users/u/.ssh") (subpath "/Users/u/.gnupg") (subpath "/Users/u/.groot") (subpath "/Users/u/.kube") (subpath "/Users/u/.netrc") (subpath "/Users/u/.docker") (subpath "/Users/u/.config/gh") (subpath "/Users/u/.config/gcloud") (subpath "/Users/u/.azure"))`,
		`(deny file-write* (subpath "/Users/u/.aws"))`,
	}
	for _, w := range want {
		if !strings.Contains(p, w+"\n") {
			t.Errorf("profile lacks the line %s:\n%s", w, p)
		}
	}
}

func TestBwrapBindsReadableSecretsReadOnly(t *testing.T) {
	var present []string
	for _, d := range SecretDirs {
		present = append(present, "/home/u/"+d)
	}
	present = append(present, "/home/u/proj", "/private/var/folders/ab/T")
	h := fakeHost("linux", "/home/u", present...)
	args := bwrapArgs(Spec{
		Dir:             "/home/u/proj",
		WritablePaths:   []string{"/home/u/proj"},
		ReadableSecrets: []string{".aws", ".netrc"},
	}, h)

	if !containsSeq(args, "--ro-bind", "/home/u/.aws", "/home/u/.aws") {
		t.Errorf("a readable set must be bound read-only: %q", args)
	}
	if !containsSeq(args, "--ro-bind", "/home/u/.netrc", "/home/u/.netrc") {
		t.Errorf("a readable file must be bound read-only: %q", args)
	}
	if containsSeq(args, "--tmpfs", "/home/u/.aws") || containsSeq(args, "--ro-bind", "/dev/null", "/home/u/.netrc") {
		t.Errorf("a readable set must not be masked: %q", args)
	}
	if !containsSeq(args, "--tmpfs", "/home/u/.kube") {
		t.Errorf(".kube was not granted and must stay masked: %q", args)
	}
	if !containsSeq(args, "--tmpfs", "/home/u/.groot") {
		t.Errorf(".groot must stay masked: %q", args)
	}
}

func TestDefaultCacheDirsPerPlatform(t *testing.T) {
	noEnv := func(string) string { return "" }
	env := func(m map[string]string) func(string) string {
		return func(k string) string { return m[k] }
	}
	cases := []struct {
		name string
		goos string
		home string
		get  func(string) string
		want []string
	}{
		{"darwin", "darwin", "/Users/u", noEnv,
			[]string{"/Users/u/.cache", "/Users/u/.npm", "/Users/u/Library/Caches", "/Users/u/go/pkg/mod"}},
		{"linux", "linux", "/home/u", noEnv,
			[]string{"/home/u/.cache", "/home/u/.npm", "/home/u/go/pkg/mod"}},
		{"xdg cache home replaces ~/.cache", "linux", "/home/u", env(map[string]string{"XDG_CACHE_HOME": "/data/xdg"}),
			[]string{"/data/xdg", "/home/u/.npm", "/home/u/go/pkg/mod"}},
		{"a relative xdg cache home is ignored", "linux", "/home/u", env(map[string]string{"XDG_CACHE_HOME": "cache"}),
			[]string{"/home/u/.cache", "/home/u/.npm", "/home/u/go/pkg/mod"}},
		{"GOCACHE is added when set", "linux", "/home/u", env(map[string]string{"GOCACHE": "/var/gocache"}),
			[]string{"/home/u/.cache", "/home/u/.npm", "/var/gocache", "/home/u/go/pkg/mod"}},
		{"a relative GOCACHE is ignored", "darwin", "/Users/u", env(map[string]string{"GOCACHE": "go"}),
			[]string{"/Users/u/.cache", "/Users/u/.npm", "/Users/u/Library/Caches", "/Users/u/go/pkg/mod"}},
		{"GOMODCACHE when absolute wins over GOPATH", "linux", "/home/u", env(map[string]string{"GOMODCACHE": "/mods", "GOPATH": "/gp"}),
			[]string{"/home/u/.cache", "/home/u/.npm", "/mods"}},
		{"GOPATH gives its first entry's pkg/mod when GOMODCACHE is unset", "linux", "/home/u", env(map[string]string{"GOPATH": "/gp:/other"}),
			[]string{"/home/u/.cache", "/home/u/.npm", "/gp/pkg/mod"}},
		{"a relative GOMODCACHE falls back to GOPATH", "linux", "/home/u", env(map[string]string{"GOMODCACHE": "mods", "GOPATH": "/gp"}),
			[]string{"/home/u/.cache", "/home/u/.npm", "/gp/pkg/mod"}},
		{"a relative GOPATH falls back to ~/go", "linux", "/home/u", env(map[string]string{"GOPATH": "gp"}),
			[]string{"/home/u/.cache", "/home/u/.npm", "/home/u/go/pkg/mod"}},
		{"no home and no module cache setting means no module root", "linux", "", noEnv, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DefaultCacheDirs(tc.goos, tc.home, tc.get)
			if !slices.Equal(got, tc.want) {
				t.Fatalf("DefaultCacheDirs = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSeatbeltAllowsCacheRootsWrite(t *testing.T) {
	h := fakeHost("darwin", "/Users/u")
	p := profileDarwin(Spec{
		Dir:           "/Users/u/proj",
		WritablePaths: []string{"/Users/u/proj", "/Users/u/.cache", "/Users/u/Library/Caches"},
	}, h)
	var allow string
	for _, line := range strings.Split(p, "\n") {
		if strings.HasPrefix(line, "(allow file-write* (subpath") {
			allow = line
		}
	}
	for _, root := range []string{"/Users/u/.cache", "/Users/u/Library/Caches"} {
		if !strings.Contains(allow, `(subpath "`+root+`")`) {
			t.Errorf("the writable allow line lacks %s:\n%s", root, allow)
		}
	}
}

func TestBwrapBindsCacheRootsReadWrite(t *testing.T) {
	h := fakeHost("linux", "/home/u", "/home/u/proj", "/home/u/.cache", "/private/var/folders/ab/T")
	args := bwrapArgs(Spec{
		Dir:           "/home/u/proj",
		WritablePaths: []string{"/home/u/proj", "/home/u/.cache"},
	}, h)
	if !containsSeq(args, "--bind", "/home/u/.cache", "/home/u/.cache") {
		t.Fatalf("the cache root must be bound read-write: %q", args)
	}
}

// The cache roots are plain writable roots: they never reopen a secret directory.
func TestSecretDirsStayDeniedEvenUnderACacheRoot(t *testing.T) {
	h := fakeHost("darwin", "/Users/u")
	p := profileDarwin(Spec{
		Dir:           "/Users/u/proj",
		WritablePaths: []string{"/Users/u/proj", "/Users/u"},
	}, h)
	if !strings.Contains(p, `(deny file-read* file-write* (subpath "/Users/u/.ssh")`) {
		t.Fatalf("a writable parent must not lift the secret deny:\n%s", p)
	}
	if strings.Index(p, `(deny file-read* file-write* (subpath "/Users/u/.ssh")`) < strings.Index(p, `(allow file-write* `) {
		t.Fatalf("the secret deny must follow the writable allow:\n%s", p)
	}
}
