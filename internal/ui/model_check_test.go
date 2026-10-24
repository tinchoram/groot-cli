package ui

import (
	"errors"
	"strings"
	"testing"

	"github.com/tinchoram/groot-cli/internal/provider"
)

func TestModelListAndLsBehaveLikeModels(t *testing.T) {
	chat := &fakeSession{model: "claude-sonnet-5-5"}
	m, ws := newTestModel(t, chat)
	ws.models = []provider.ModelInfo{{ID: "claude-sonnet-5-5", DisplayName: "Sonnet 5.5"}}

	m = sendLine(t, m, "/model list")
	if !strings.Contains(stripANSI(m.transcriptText()), "● claude-sonnet-5-5") {
		t.Fatalf("/model list must list the models:\n%s", m.transcriptText())
	}
	if chat.model != "claude-sonnet-5-5" || len(chat.setModels) != 0 {
		t.Fatalf("/model list must not switch the model: %q, %v", chat.model, chat.setModels)
	}
	m = sendLine(t, m, "/model ls")
	if strings.Count(stripANSI(m.transcriptText()), "● claude-sonnet-5-5") < 2 {
		t.Fatalf("/model ls must list the models too:\n%s", m.transcriptText())
	}
}

func TestModelRefusesAnUnknownIdAndChangesNothing(t *testing.T) {
	chat := &fakeSession{model: "claude-sonnet-5-5"}
	m, ws := newTestModel(t, chat)
	ws.checkErr = errors.New(`unknown model "claude-sonet-5-5" · did you mean claude-sonnet-5-5?`)

	m = sendLine(t, m, "/model claude-sonet-5-5")
	if chat.model != "claude-sonnet-5-5" || len(chat.setModels) != 0 {
		t.Fatalf("a refused id must not become the model: %q", chat.model)
	}
	if !strings.Contains(stripANSI(m.transcriptText()), "did you mean claude-sonnet-5-5") {
		t.Fatalf("the refusal lists the suggestions:\n%s", m.transcriptText())
	}
}

func TestModelForceSkipsTheCheck(t *testing.T) {
	chat := &fakeSession{model: "claude-sonnet-5-5"}
	m, ws := newTestModel(t, chat)
	ws.checkErr = errors.New("unknown model")

	m = sendLine(t, m, "/model preview-model --force")
	if chat.model != "preview-model" {
		t.Fatalf("--force sets the model without the check: %q", chat.model)
	}
	_ = ws
}

func TestRefusedIdIsNotSavedToTheAgent(t *testing.T) {
	chat := &fakeSession{model: "claude-sonnet-5-5"}
	m, ws := newTestModel(t, chat)
	ws.checkErr = errors.New("unknown model")

	m = sendLine(t, m, "/model nope --agent")
	if len(ws.savedModels) != 0 || chat.model != "claude-sonnet-5-5" {
		t.Fatalf("saved = %v, model = %q", ws.savedModels, chat.model)
	}
}
