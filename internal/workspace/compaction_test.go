package workspace

import (
	"testing"

	"github.com/tinchoram/groot-cli/internal/compact"
	"github.com/tinchoram/groot-cli/internal/config"
	"github.com/tinchoram/groot-cli/internal/provider"
)

// The default strategy summarizes; an explicit none keeps the whole history.
func TestCompactorFollowsTheStrategy(t *testing.T) {
	p := provider.NewMock()
	if _, ok := compactorFor(config.Default().Compaction, p).(compact.Summarize); !ok {
		t.Fatal("the default strategy must summarize")
	}
	if _, ok := compactorFor(config.CompactionConfig{Strategy: "none"}, p).(compact.None); !ok {
		t.Fatal("strategy none must not compact")
	}
}
