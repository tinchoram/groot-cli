package workspace

import (
	"context"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/tinchoram/groot-cli/internal/memory"
)

// sessionTitle is "Session YYYY-MM-DD HH:MM <agent>".
var sessionTitlePattern = regexp.MustCompile(`^Session \d{4}-\d{2}-\d{2} \d{2}:\d{2} reader$`)

func TestSummaryPromptAsksForTheFixedShape(t *testing.T) {
	for _, heading := range []string{"Goal:", "Done:", "Decisions:", "Next:", "Files:"} {
		if !strings.Contains(summaryPrompt, heading) {
			t.Fatalf("the summary prompt must ask for %q", heading)
		}
	}
	if !strings.Contains(summaryPrompt, "12 lines") {
		t.Fatal("the summary prompt must cap the summary at about twelve lines")
	}
}

func TestLocalSummaryUsesTheSameHeadings(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, text("done it"))
	_ = f.ws.SelectAgent("reader")
	conv := f.ws.Chat().(*Conversation)
	if _, err := conv.Send(ctx, "fix the dns record"); err != nil {
		t.Fatal(err)
	}
	got := conv.localSummary()
	for _, want := range []string{"Goal: fix the dns record", "Done: done it", "Decisions: none", "Next: unknown (fast close)", "Files: none"} {
		if !strings.Contains(got, want) {
			t.Fatalf("local summary %q is missing %q", got, want)
		}
	}
}

func TestLocalSummaryCapsTheGoalAtTwoHundredCharacters(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, text("ok"))
	_ = f.ws.SelectAgent("reader")
	conv := f.ws.Chat().(*Conversation)
	if _, err := conv.Send(ctx, strings.Repeat("q", 500)); err != nil {
		t.Fatal(err)
	}
	goal := strings.TrimPrefix(strings.Split(conv.localSummary(), "\n")[0], "Goal: ")
	if n := len([]rune(goal)); n > localGoalRunes+1 {
		t.Fatalf("goal kept %d characters, want at most %d", n, localGoalRunes)
	}
}

func TestSessionSummaryIsTitledWithTimeAndAgent(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, text("answer"), text("Goal: notes\nDone:\n- read one file"))
	_ = f.ws.SelectAgent("reader")
	conv := f.ws.Chat().(*Conversation)
	if _, err := conv.Send(ctx, "what is in notes"); err != nil {
		t.Fatal(err)
	}
	if err := conv.summarize(ctx, conv.SessionID(), conv.Transcript()); err != nil {
		t.Fatal(err)
	}
	st, err := memory.OpenSQLite(filepath.Join(f.dir, ".groot", "groot.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	entries, err := st.Preamble(ctx, memory.Reader{Agent: "reader"}, memory.Budget{})
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, e := range entries {
		if e.Kind == memory.KindSessionSummary && e.Scope == memory.ScopeAgent {
			found = true
			if !sessionTitlePattern.MatchString(e.Title) {
				t.Fatalf("title = %q, want Session <time> reader", e.Title)
			}
			if e.Source != "session:"+conv.SessionID() {
				t.Fatalf("source = %q", e.Source)
			}
		}
	}
	if !found {
		t.Fatal("no session summary stored")
	}
}
