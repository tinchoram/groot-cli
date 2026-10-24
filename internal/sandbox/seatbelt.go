package sandbox

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// seatbelt runs commands under /usr/bin/sandbox-exec with a generated SBPL profile.
type seatbelt struct {
	bin string
	h   host
}

func newSeatbelt(h host) Runner {
	return seatbelt{bin: sandboxExecPath, h: h}
}

func (seatbelt) Name() string { return BackendSandboxExec }

func (s seatbelt) Wrap(ctx context.Context, spec Spec) (*exec.Cmd, error) {
	if err := spec.validate(); err != nil {
		return nil, err
	}
	// The profile goes inline with -p: the command is passed as argv, not as a
	// shell string, so no quoting of the profile is needed beyond SBPL escaping.
	args := []string{"-p", profileDarwin(spec, s.h)}
	args = append(args, spec.Command...)
	cmd := exec.CommandContext(ctx, s.bin, args...)
	cmd.Dir = spec.Dir
	if spec.Env != nil {
		cmd.Env = spec.Env
	}
	return cmd, nil
}

// profileDarwin builds the SBPL profile for spec. SBPL uses the last matching
// rule, so the denies come after the broad allows they override.
func profileDarwin(spec Spec, h host) string {
	var b strings.Builder
	line := func(s string) {
		b.WriteString(s)
		b.WriteByte('\n')
	}
	line("(version 1)")
	line("(deny default)")
	line("(allow process*)")
	line("(allow sysctl-read)")
	line("(allow mach-lookup)")
	line("(allow ipc-posix-shm*)")
	line("(allow signal (target self))")
	line("(allow file-ioctl)")
	line("(allow file-read*)")
	line(`(allow file-write* (literal "/dev/null") (literal "/dev/tty") (literal "/dev/stdout") (literal "/dev/stderr"))`)
	line("(allow file-write* " + subpaths(writableRoots(spec, h)) + ")")
	if secrets := secretPaths(h); len(secrets) > 0 {
		line("(deny file-read* file-write* " + subpaths(secrets) + ")")
	}
	// The space's .groot holds the memory database and the audit log: read-only.
	line("(deny file-write* " + subpaths([]string{h.resolve(filepath.Join(spec.Dir, ".groot"))}) + ")")
	if spec.AllowNetwork {
		line("(allow network*)")
	} else {
		line("(deny network*)")
	}
	return b.String()
}

// subpaths renders (subpath "p") for each path.
func subpaths(paths []string) string {
	parts := make([]string, 0, len(paths))
	for _, p := range paths {
		parts = append(parts, fmt.Sprintf("(subpath %s)", sbplQuote(p)))
	}
	return strings.Join(parts, " ")
}

// sbplQuote renders s as an SBPL string literal, escaping backslashes and quotes.
func sbplQuote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}
