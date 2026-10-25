package sandbox

import (
	"strings"
	"testing"
)

func TestInstallHintByDistro(t *testing.T) {
	generic := "Install bubblewrap (apt install bubblewrap) to enable the sandbox."
	cases := []struct {
		name      string
		goos      string
		osRelease string
		want      string
	}{
		{"ubuntu", "linux", "NAME=Ubuntu\nID=ubuntu\nID_LIKE=debian\n", "sudo apt install bubblewrap"},
		{"debian", "linux", "ID=debian\n", "sudo apt install bubblewrap"},
		{"fedora", "linux", "ID=fedora\n", "sudo dnf install bubblewrap"},
		{"rhel through ID_LIKE", "linux", "ID=\"rocky\"\nID_LIKE=\"rhel centos fedora\"\n", "sudo dnf install bubblewrap"},
		{"arch", "linux", "ID=arch\n", "sudo pacman -S bubblewrap"},
		{"opensuse through ID_LIKE", "linux", "ID=opensuse-tumbleweed\nID_LIKE=\"suse opensuse\"\n", "sudo zypper install bubblewrap"},
		{"unknown distro keeps the generic hint", "linux", "ID=alpine\n", generic},
		{"no os-release keeps the generic hint", "linux", "", generic},
		{"darwin", "darwin", "", "xcode-select --install"},
		{"other platform", "plan9", "", "No sandbox backend exists for plan9 yet."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := InstallHint(tc.goos, tc.osRelease)
			if !strings.Contains(got, tc.want) {
				t.Fatalf("InstallHint(%q) = %q, want it to contain %q", tc.goos, got, tc.want)
			}
		})
	}
}

// The warning that Detect returns carries the distro-specific command.
func TestDetectWarningNamesTheDistroCommand(t *testing.T) {
	h := fakeHost("linux", "/home/u")
	h.osRelease = "ID=fedora\n"
	_, w := detect(h, Config{Mode: "auto"})
	if !strings.Contains(string(w), "sudo dnf install bubblewrap") {
		t.Fatalf("warning %q should name the Fedora command", w)
	}
}
