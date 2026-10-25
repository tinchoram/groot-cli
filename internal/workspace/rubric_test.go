package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const resumeRubric = "Work rubric: one test decides the size: could another agent resume the work from the request plus the git diff?"

func TestGrootPromptCarriesTheResumeRubric(t *testing.T) {
	prompt := grootPrompt
	if !strings.Contains(prompt, resumeRubric) {
		t.Fatalf("groot prompt lacks the resume rubric:\n%s", prompt)
	}
	if strings.Contains(prompt, "1-2 files") {
		t.Fatal("groot prompt still carries the old file-count rubric")
	}
}

func TestLibraryAgentsCarryTheResumeRubric(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "..", "library", "agents", "*.md"))
	if err != nil || len(files) == 0 {
		t.Fatalf("library agents: %v, %v", files, err)
	}
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		src := string(raw)
		name := filepath.Base(f)
		if strings.Contains(src, "1-2 files") {
			t.Errorf("%s still carries the old file-count rubric", name)
		}
		if !strings.Contains(src, "task_advance") {
			continue // agents outside the task flow carry no rubric
		}
		if !strings.Contains(src, resumeRubric) {
			t.Errorf("%s lacks the resume rubric", name)
		}
		if !strings.Contains(src, "task_verify") {
			t.Errorf("%s runs the task flow without task_verify", name)
		}
	}
}
