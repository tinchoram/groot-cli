package ui

import (
	"context"
	"strings"
	"testing"

	"github.com/tinchoram/groot-cli/internal/provider"
)

func (f *fakeWorkspace) ListModels(context.Context) ([]provider.ModelInfo, error) {
	return f.models, f.modelErr
}

// CheckModel answers like the workspace: --force skips the check, and the fake's
// checkErr stands for a refusal.
func (f *fakeWorkspace) CheckModel(_ context.Context, id string, force bool) (string, error) {
	if force {
		return "", nil
	}
	return f.modelWarn, f.checkErr
}

func (f *fakeWorkspace) SaveAgentModel(agent, model string) error {
	if f.savedModels == nil {
		f.savedModels = map[string]string{}
	}
	f.savedModels[agent] = model
	return nil
}

func TestModelsCommandListsPricesAndMarksTheCurrentModel(t *testing.T) {
	chat := &fakeSession{model: "claude-sonnet-5-5"}
	m, ws := newTestModel(t, chat)
	ws.models = []provider.ModelInfo{
		{ID: "claude-sonnet-5-5", DisplayName: "Sonnet 5.5"},
		{ID: "mystery-model", DisplayName: "Mystery"},
	}
	m = sendLine(t, m, "/models")
	out := stripANSI(m.transcriptText())
	for _, want := range []string{"● claude-sonnet-5-5", "$2.00 in / $10.00 out per 1M", "mystery-model", "price unknown"} {
		if !strings.Contains(out, want) {
			t.Fatalf("/models lacks %q:\n%s", want, out)
		}
	}
}

func TestModelsCommandReportsErrors(t *testing.T) {
	m, ws := newTestModel(t, &fakeSession{model: "m"})
	ws.modelErr = context.DeadlineExceeded
	m = sendLine(t, m, "/models")
	if !strings.Contains(stripANSI(m.transcriptText()), "error:") {
		t.Fatalf("a failed listing must be reported:\n%s", m.transcriptText())
	}
}

func TestModelWithAgentFlagSwitchesAndSaves(t *testing.T) {
	chat := &fakeSession{model: "claude-sonnet-5-5"}
	m, ws := newTestModel(t, chat)
	ws.modelWarn = "model not checked against the provider list: offline"
	m = sendLine(t, m, "/model claude-haiku-5-5 --agent")

	if chat.model != "claude-haiku-5-5" {
		t.Fatalf("session model = %q", chat.model)
	}
	if got := ws.savedModels[ws.ActiveAgent()]; got != "claude-haiku-5-5" {
		t.Fatalf("saved models = %v", ws.savedModels)
	}
	out := stripANSI(m.transcriptText())
	if !strings.Contains(out, "saved to") || !strings.Contains(out, "offline") {
		t.Fatalf("the save and its warning must be in the transcript:\n%s", out)
	}
}

func TestModelWithoutFlagOnlySwitchesTheSession(t *testing.T) {
	chat := &fakeSession{model: "claude-sonnet-5-5"}
	m, ws := newTestModel(t, chat)
	m = sendLine(t, m, "/model claude-haiku-5-5")
	if chat.model != "claude-haiku-5-5" || len(ws.savedModels) != 0 {
		t.Fatalf("session = %q, saved = %v", chat.model, ws.savedModels)
	}
}
