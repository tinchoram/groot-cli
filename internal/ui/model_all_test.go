package ui

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/tinchoram/groot-cli/internal/provider"
	"github.com/tinchoram/groot-cli/internal/workspace"
)

// setAll is the fake of Workspace.SetSpaceAgentsModel: it records the call,
// refuses an unlisted id unless forced (checkErr stands for the refusal), and
// reports one changed agent per member of the space.
func (f *fakeWorkspace) SetSpaceAgentsModel(_ context.Context, space, model string, force bool) ([]string, string, error) {
	f.allSpace, f.allModel, f.allForce = space, model, force
	if f.checkErr != nil && !force {
		return nil, "", f.checkErr
	}
	return f.allChanged, "", nil
}

func fakeAllSpace(ws *fakeWorkspace) {
	ws.active = "ops"
	ws.spaces = map[string]workspace.SpaceInfo{"ops": {Name: "ops", Agents: []string{"triage", "reporter"}, Active: true}}
	ws.allChanged = []string{"triage", "reporter"}
}

func TestModelAllSetsEveryAgentAndPrintsOneLinePerAgent(t *testing.T) {
	chat := &fakeSession{model: "claude-sonnet-5-5"}
	m, ws := newTestModel(t, chat)
	fakeAllSpace(ws)

	m = sendLine(t, m, "/model --all gemini-3.8-flash")
	if ws.allSpace != "ops" || ws.allModel != "gemini-3.8-flash" || ws.allForce {
		t.Fatalf("call = space %q model %q force %v", ws.allSpace, ws.allModel, ws.allForce)
	}
	out := stripANSI(m.transcriptText())
	for _, want := range []string{"triage", "reporter", "gemini-3.8-flash"} {
		if !strings.Contains(out, want) {
			t.Fatalf("transcript lacks %q:\n%s", want, out)
		}
	}
	if strings.Count(out, "model of ") != 2 {
		t.Fatalf("want one line per agent changed:\n%s", out)
	}
}

func TestModelAllAcceptsTheFlagAfterTheId(t *testing.T) {
	chat := &fakeSession{model: "claude-sonnet-5-5"}
	m, ws := newTestModel(t, chat)
	fakeAllSpace(ws)

	_ = sendLine(t, m, "/model gemini-3.8-flash --all")
	if ws.allModel != "gemini-3.8-flash" {
		t.Fatalf("model = %q, want the id with the trailing --all", ws.allModel)
	}
}

func TestModelAllRefusesAnUnknownIdAndChangesNothing(t *testing.T) {
	chat := &fakeSession{model: "claude-sonnet-5-5"}
	m, ws := newTestModel(t, chat)
	fakeAllSpace(ws)
	ws.checkErr = errors.New(`unknown model "gemini-3.8-flsh" · did you mean gemini-3.8-flash?`)

	m = sendLine(t, m, "/model --all gemini-3.8-flsh")
	if !strings.Contains(stripANSI(m.transcriptText()), "did you mean gemini-3.8-flash") {
		t.Fatalf("the refusal lists the suggestion:\n%s", m.transcriptText())
	}
	if strings.Contains(stripANSI(m.transcriptText()), "model of ") {
		t.Fatal("a refused id must change no agent")
	}
}

func TestModelAllForceSkipsTheCheck(t *testing.T) {
	chat := &fakeSession{model: "claude-sonnet-5-5"}
	m, ws := newTestModel(t, chat)
	fakeAllSpace(ws)
	ws.checkErr = errors.New("unknown model")

	_ = sendLine(t, m, "/model --all --force preview-model")
	if ws.allModel != "preview-model" || !ws.allForce {
		t.Fatalf("--all --force = model %q force %v", ws.allModel, ws.allForce)
	}
}

func TestModelAllNeedsAnActiveSpace(t *testing.T) {
	chat := &fakeSession{model: "claude-sonnet-5-5"}
	m, ws := newTestModel(t, chat)
	ws.allChanged = nil
	ws.spaces = map[string]workspace.SpaceInfo{}

	m = sendLine(t, m, "/model --all gemini-3.8-flash")
	if ws.allModel != "" {
		t.Fatalf("without an active space nothing is set: %q", ws.allModel)
	}
	if !strings.Contains(stripANSI(m.transcriptText()), "no active space") {
		t.Fatalf("want a clear message:\n%s", m.transcriptText())
	}
}

func TestModelAllIsCompletedOnceADashIsTyped(t *testing.T) {
	chat := &fakeSession{model: "claude-sonnet-5-5"}
	m, _ := newTestModel(t, chat)

	if got := completionInserts(typeText(m, "/model --")); len(got) != 1 || got[0] != "/model --all" {
		t.Fatalf("completions for /model -- = %v, want /model --all", got)
	}
	if got := completionInserts(typeText(m, "/model ")); len(got) != 0 {
		t.Fatalf("/model with no dash must not list flags, got %v", got)
	}
}

func TestModelsTagsEachEntryWithItsProvider(t *testing.T) {
	chat := &fakeSession{model: "claude-sonnet-5-5"}
	m, ws := newTestModel(t, chat)
	ws.models = []provider.ModelInfo{
		{ID: "claude-sonnet-5-5", DisplayName: "Sonnet 5.5", Provider: "anthropic"},
		{ID: "gemini-3.8-flash", DisplayName: "Gemini 3.8 Flash", Provider: "gemini"},
	}

	m = sendLine(t, m, "/models")
	out := stripANSI(m.transcriptText())
	for _, want := range []string{"anthropic  claude-sonnet-5-5", "gemini  gemini-3.8-flash"} {
		if !strings.Contains(out, want) {
			t.Fatalf("listing lacks %q:\n%s", want, out)
		}
	}
}
