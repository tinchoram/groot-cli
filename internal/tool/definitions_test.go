package tool

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// builtinRegistry registers every builtin the workspace can offer, built with
// fake dependencies. Only the definitions are read, never the tools run.
func builtinRegistry(t *testing.T) *Registry {
	t.Helper()
	dir := t.TempDir()
	mem := MemoryAccess{}
	m := &fakeMessenger{}
	ctl := flowController(t)
	all := []Tool{
		NewBash(dir), NewKubectl("kubectl", nil), NewFetch("v", FetchPolicy{}),
		NewCurrentTime(time.Now), NewReadFile(dir), NewWriteFile(dir), NewListDir(dir), NewGlob(dir),
		NewRemember(mem), NewRecall(mem), NewMemoryGet(mem), NewForget(mem), NewUpdateMemory(mem),
		NewSendMessage(m), NewReadInbox(m),
		NewTaskClassify(ctl), NewTaskOpen(ctl, m), NewTaskAdvance(ctl, m), NewTaskVerify(ctl), NewTaskStatus(ctl),
		NewFeedbackReport(nil, "", "", "v"), NewFeedbackList(nil), NewFeedbackUpdate(nil),
		NewReadChangelog(""),
	}
	r := NewRegistry()
	for _, tl := range all {
		if err := r.Register(tl); err != nil {
			t.Fatal(err)
		}
	}
	return r
}

// sentenceCount counts sentences by their ". " boundaries; the last sentence
// ends the text with its period and needs no space after it.
func sentenceCount(s string) int {
	return strings.Count(strings.TrimSpace(s), ". ") + 1
}

func TestEveryBuiltinDescriptionHasThreeSentencesAndDocumentsEachParameter(t *testing.T) {
	for _, def := range builtinRegistry(t).Definitions() {
		if n := sentenceCount(def.Description); n < 3 {
			t.Errorf("%s: description has %d sentences, want at least 3: %q", def.Name, n, def.Description)
		}
		if n := len(strings.Fields(def.Description)); n >= 90 {
			t.Errorf("%s: description has %d words, want under 90", def.Name, n)
		}
		var schema struct {
			Properties map[string]struct {
				Description string `json:"description"`
			} `json:"properties"`
		}
		if err := json.Unmarshal(def.InputSchema, &schema); err != nil {
			t.Fatalf("%s: schema: %v", def.Name, err)
		}
		for name, p := range schema.Properties {
			if strings.TrimSpace(p.Description) == "" {
				t.Errorf("%s: parameter %q has no description", def.Name, name)
			}
		}
	}
}
