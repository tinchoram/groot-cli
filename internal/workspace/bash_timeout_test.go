package workspace

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/tinchoram/groot-cli/internal/config"
	"github.com/tinchoram/groot-cli/internal/tool"
)

func TestBashToolGetsConfiguredTimeouts(t *testing.T) {
	cfg, err := config.Load(filepath.Join(t.TempDir(), "missing.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.Bash.Timeout = 90 * time.Second
	cfg.Bash.MaxTimeout = 20 * time.Minute
	w := &Workspace{opts: Options{Config: cfg}}
	tl, err := w.newTool("bash", t.TempDir(), nil, nil, nil, nil, nil, "reader")
	if err != nil {
		t.Fatal(err)
	}
	b, ok := tl.(*tool.Bash)
	if !ok {
		t.Fatalf("bash tool is %T, want *tool.Bash", tl)
	}
	if b.Timeout != 90*time.Second || b.MaxTimeout != 20*time.Minute {
		t.Fatalf("bash timeouts = %v / %v, want 1m30s / 20m", b.Timeout, b.MaxTimeout)
	}
}
