package workspace

import (
	"context"
	"slices"
	"testing"

	"github.com/tinchoram/groot-cli/internal/api"
	"github.com/tinchoram/groot-cli/internal/tool"
)

// fullTool implements every optional interface the agent inspects, with fixed answers.
type fullTool struct{}

func (fullTool) Definition() api.ToolDef                        { return api.ToolDef{Name: "full"} }
func (fullTool) Risk() tool.Risk                                { return tool.RiskExec }
func (fullTool) RiskFor(string) tool.Risk                       { return tool.RiskExec }
func (fullTool) Preview(string) string                          { return "full preview" }
func (fullTool) Execute(context.Context, string) (string, bool) { return "out", false }
func (fullTool) Untrusted() bool                                { return true }
func (fullTool) SelfEnveloped() bool                            { return true }
func (fullTool) External() bool                                 { return true }
func (fullTool) DestructiveFor(string) (tool.DestructiveKind, string, bool) {
	return "git", "git push --force", true
}
func (fullTool) CredentialSets() []string { return []string{"aws"} }
func (fullTool) SandboxName() string      { return "bwrap" }

// plainTool implements only tool.Tool: a wrapper of it must claim nothing.
type plainTool struct{}

func (plainTool) Definition() api.ToolDef                        { return api.ToolDef{Name: "plain"} }
func (plainTool) Risk() tool.Risk                                { return tool.RiskRead }
func (plainTool) RiskFor(string) tool.Risk                       { return tool.RiskRead }
func (plainTool) Preview(string) string                          { return "plain" }
func (plainTool) Execute(context.Context, string) (string, bool) { return "", false }

// wrappers are every type in this package that wraps a tool.Tool.
func wrappers(inner tool.Tool) map[string]tool.Tool {
	return map[string]tool.Tool{
		"recordingTool": recordingTool{Tool: inner},
		"trackedTool":   trackedTool{Tool: inner, log: &fileLog{}},
	}
}

// A wrapper keeps every optional interface of the tool it wraps, with the same answers.
func TestWrappersPreserveTheOptionalInterfaces(t *testing.T) {
	for name, w := range wrappers(fullTool{}) {
		t.Run(name, func(t *testing.T) {
			if u, ok := w.(tool.Untrusted); !ok || !u.Untrusted() {
				t.Error("Untrusted is lost")
			}
			if s, ok := w.(tool.SelfEnveloped); !ok || !s.SelfEnveloped() {
				t.Error("SelfEnveloped is lost")
			}
			if e, ok := w.(tool.External); !ok || !e.External() {
				t.Error("External is lost")
			}
			d, ok := w.(tool.Destructive)
			if !ok {
				t.Error("Destructive is lost")
			} else if kind, reason, bad := d.DestructiveFor("{}"); !bad || kind != "git" || reason != "git push --force" {
				t.Errorf("DestructiveFor = %q %q %v", kind, reason, bad)
			}
			if c, ok := w.(tool.Credentialed); !ok || !slices.Equal(c.CredentialSets(), []string{"aws"}) {
				t.Error("Credentialed is lost")
			}
			if s, ok := w.(interface{ SandboxName() string }); !ok || s.SandboxName() != "bwrap" {
				t.Error("SandboxName is lost")
			}
			if got := w.RiskFor("{}"); got != tool.RiskExec {
				t.Errorf("RiskFor = %v", got)
			}
			if got := w.Preview("{}"); got != "full preview" {
				t.Errorf("Preview = %q", got)
			}
		})
	}
}

// A wrapper of a plain tool claims no optional interface as true.
func TestWrappersOfAPlainToolClaimNothing(t *testing.T) {
	for name, w := range wrappers(plainTool{}) {
		t.Run(name, func(t *testing.T) {
			if u, ok := w.(tool.Untrusted); ok && u.Untrusted() {
				t.Error("Untrusted must not be claimed")
			}
			if e, ok := w.(tool.External); ok && e.External() {
				t.Error("External must not be claimed")
			}
			if s, ok := w.(tool.SelfEnveloped); ok && s.SelfEnveloped() {
				t.Error("SelfEnveloped must not be claimed")
			}
			if d, ok := w.(tool.Destructive); ok {
				if _, _, bad := d.DestructiveFor("{}"); bad {
					t.Error("a plain tool has no destructive command")
				}
			}
			if c, ok := w.(tool.Credentialed); ok && len(c.CredentialSets()) != 0 {
				t.Error("a plain tool has no credential sets")
			}
		})
	}
}
