package sandbox

import (
	"context"
	"os/exec"
	"path/filepath"
)

// bwrap runs commands under bubblewrap: the whole filesystem is read-only,
// and the writable roots and the secret directories are bind-mounted over it.
type bwrap struct {
	bin string
	h   host
}

func newBwrap(bin string, h host) Runner {
	return bwrap{bin: bin, h: h}
}

func (bwrap) Name() string { return BackendBwrap }

func (b bwrap) Wrap(ctx context.Context, spec Spec) (*exec.Cmd, error) {
	if err := spec.validate(); err != nil {
		return nil, err
	}
	args := append(bwrapArgs(spec, b.h), "--")
	args = append(args, spec.Command...)
	cmd := exec.CommandContext(ctx, b.bin, args...)
	cmd.Dir = spec.Dir
	if spec.Env != nil {
		cmd.Env = spec.Env
	}
	return cmd, nil
}

// bwrapArgs returns the bubblewrap options that confine spec. The caller
// appends "--" and the command. Writable roots and secret directories that do
// not exist are skipped: a missing secret needs no mask, and a missing extra
// root has nothing to write to yet.
func bwrapArgs(spec Spec, h host) []string {
	args := []string{
		"--die-with-parent",
		"--new-session",
		"--ro-bind", "/", "/",
		"--dev", "/dev",
		"--proc", "/proc",
	}
	for _, p := range writableRoots(spec, h) {
		if h.exists(p) {
			args = append(args, "--bind", p, p)
		}
	}
	if groot := h.resolve(filepath.Join(spec.Dir, ".groot")); h.exists(groot) {
		args = append(args, "--ro-bind", groot, groot)
	}
	for _, p := range secretPaths(h) {
		switch {
		case !h.exists(p):
		case h.isDir(p):
			args = append(args, "--tmpfs", p)
		default:
			// tmpfs mounts only over directories; a file is masked by binding /dev/null over it.
			args = append(args, "--ro-bind", "/dev/null", p)
		}
	}
	if !spec.AllowNetwork {
		args = append(args, "--unshare-net")
	}
	return args
}
