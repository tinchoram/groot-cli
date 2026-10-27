package gate

import (
	"context"
	"testing"

	"github.com/tinchoram/groot-cli/internal/tool"
)

// Background jobs run the same commands as bash, so work mode asks for the
// same always-ask commands for them.
func TestWorkBackgroundCommandsFollowTheAlwaysAskList(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		tool, command string
		asks          bool
	}{
		{"bash_background", "go test ./...", false},
		{"bash_background", "kubectl delete pod x", true},
		{"bash_background", "rm -rf /tmp/cache", true},
		{"monitor", "kubectl get pods -n prod", false},
		{"monitor", "terraform destroy", true},
	}
	for _, tt := range tests {
		t.Run(tt.tool+" "+tt.command, func(t *testing.T) {
			s, _ := newWork(t, true, nil, 0, 0)
			user := &asker{}
			g := Work{Inner: user, State: s}
			before := user.calls
			g.Decide(ctx, Request{Tool: tt.tool, Input: `{"command":"` + tt.command + `"}`, Risk: tool.RiskExec})
			if asked := user.calls > before; asked != tt.asks {
				t.Fatalf("asked the user = %v, want %v", asked, tt.asks)
			}
		})
	}
}
