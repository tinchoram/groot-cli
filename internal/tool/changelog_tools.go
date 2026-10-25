package tool

import (
	"context"
	"encoding/json"
	"strings"

	changelog "github.com/tinchoram/groot-cli"
	"github.com/tinchoram/groot-cli/internal/api"
)

// ReadChangelog returns the release notes of groot itself: the whole
// changelog, or the section of one version.
type ReadChangelog struct{ text string }

// NewReadChangelog returns the read_changelog tool over the changelog text.
func NewReadChangelog(text string) *ReadChangelog { return &ReadChangelog{text: text} }

func (*ReadChangelog) Definition() api.ToolDef {
	return api.ToolDef{
		Name:        "read_changelog",
		Description: "Read the changelog of Groot itself. It is embedded in the program, so it does not depend on the space. Without version it returns the whole changelog; with version (for example 0.5.2) it returns that release's section.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"version":{"type":"string"}}}`),
	}
}

func (*ReadChangelog) Risk() Risk            { return RiskRead }
func (r *ReadChangelog) RiskFor(string) Risk { return r.Risk() }
func (*ReadChangelog) Preview(string) string { return "read_changelog" }

func (r *ReadChangelog) Execute(_ context.Context, input string) (string, bool) {
	var in struct {
		Version string `json:"version"`
	}
	if strings.TrimSpace(input) != "" {
		if err := json.Unmarshal([]byte(input), &in); err != nil {
			return "invalid input: " + err.Error(), true
		}
	}
	version := strings.TrimSpace(in.Version)
	if version == "" {
		return r.text, false
	}
	section, ok := changelog.SectionOf(r.text, version)
	if !ok {
		return "no changelog entry for " + version, false
	}
	return section, false
}
