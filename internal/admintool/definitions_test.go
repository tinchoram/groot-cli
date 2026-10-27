package admintool

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/tinchoram/groot-cli/internal/tool"
)

// definitionSentences counts sentences by their ". " boundaries; the last
// sentence ends the text with its period and needs no space after it.
func definitionSentences(s string) int {
	return strings.Count(strings.TrimSpace(s), ". ") + 1
}

func TestEveryAdminToolDescriptionHasThreeSentencesAndDocumentsEachParameter(t *testing.T) {
	r := tool.NewRegistry()
	for _, tl := range All(&fakeManager{}, t.TempDir()) {
		if err := r.Register(tl); err != nil {
			t.Fatal(err)
		}
	}
	if len(r.Names()) == 0 {
		t.Fatal("no admin tools registered")
	}
	for _, def := range r.Definitions() {
		if n := definitionSentences(def.Description); n < 3 {
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
