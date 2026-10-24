package ui

import (
	"context"
	"strings"
	"testing"

	"github.com/tinchoram/groot-cli/internal/provider"
)

// The fake keeps the default in defaultModel and records what --default sets.
func (f *fakeWorkspace) DefaultModel() string { return f.defaultModel }

func (f *fakeWorkspace) ConfigFile() string { return "groot.yaml" }

func (f *fakeWorkspace) SetDefaultModel(_ context.Context, model string, force bool) (string, error) {
	if f.checkErr != nil && !force {
		return "", f.checkErr
	}
	f.defaultSet = model
	return "", nil
}

func TestModelsMarksTheDefaultAndTheSession(t *testing.T) {
	chat := &fakeSession{model: "claude-haiku-5-5"}
	m, ws := newTestModel(t, chat)
	ws.defaultModel = "claude-sonnet-5-5"
	ws.models = []provider.ModelInfo{
		{ID: "claude-sonnet-5-5", DisplayName: "Sonnet"},
		{ID: "claude-haiku-5-5", DisplayName: "Haiku"},
	}
	m = sendLine(t, m, "/models")
	out := stripANSI(m.transcriptText())
	for _, want := range []string{"claude-sonnet-5-5", "[default]", "claude-haiku-5-5", "[session]"} {
		if !strings.Contains(out, want) {
			t.Fatalf("/models lacks %q:\n%s", want, out)
		}
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "claude-sonnet-5-5") && strings.Contains(line, "[session]") {
			t.Fatalf("the default row is not the session row: %q", line)
		}
	}
}

func TestModelDefaultWritesTheDefaultAndKeepsTheSession(t *testing.T) {
	chat := &fakeSession{model: "claude-sonnet-5-5"}
	m, ws := newTestModel(t, chat)
	m = sendLine(t, m, "/model claude-haiku-5-5 --default")

	if ws.defaultSet != "claude-haiku-5-5" {
		t.Fatalf("the default was not written: %q", ws.defaultSet)
	}
	if chat.model != "claude-sonnet-5-5" {
		t.Fatalf("--default must not switch the open session: %q", chat.model)
	}
	out := stripANSI(m.transcriptText())
	if !strings.Contains(out, "default model set to claude-haiku-5-5 in groot.yaml") {
		t.Fatalf("the transcript says which file and what takes effect:\n%s", out)
	}
}

func TestModelDefaultRefusesAnUnknownId(t *testing.T) {
	chat := &fakeSession{model: "claude-sonnet-5-5"}
	m, ws := newTestModel(t, chat)
	ws.checkErr = errorString("unknown model \"nope\"")
	m = sendLine(t, m, "/model nope --default")
	if ws.defaultSet != "" || !strings.Contains(stripANSI(m.transcriptText()), "unknown model") {
		t.Fatalf("a refused id writes nothing: %q", ws.defaultSet)
	}
}

type errorString string

func (e errorString) Error() string { return string(e) }
