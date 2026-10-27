package memory

import (
	"strings"
	"testing"
)

func entry(id int64, topic, text string) Entry {
	return Entry{ID: id, Kind: KindRunbook, Scope: ScopeAgent, TopicKey: topic, Text: text, State: StateActive}
}

func TestFirstBlockIsTheFullMemory(t *testing.T) {
	var s Shown
	out := s.Block([]Entry{entry(1, "drain", "drain the node first"), entry(2, "", "rotate keys monthly")})
	for _, want := range []string{
		"<memory>", "</memory>", "agent's stored memory for this space",
		"- [runbook] drain the node first", "- [runbook] rotate keys monthly",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("full block misses %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "<memory-update>") {
		t.Errorf("the first block is the full memory, not a delta:\n%s", out)
	}
}

func TestEmptyMemoryGivesNoBlockUntilSomethingIsRemembered(t *testing.T) {
	var s Shown
	if out := s.Block(nil); out != "" {
		t.Fatalf("no entries, no block; got %q", out)
	}
	out := s.Block([]Entry{entry(1, "drain", "drain the node first")})
	if !strings.Contains(out, "<memory-update>") || !strings.Contains(out, "drain the node first") {
		t.Fatalf("an entry added after an empty start is a delta, got:\n%s", out)
	}
}

func TestUnchangedMemoryGivesNoBlock(t *testing.T) {
	var s Shown
	entries := []Entry{entry(1, "drain", "drain the node first")}
	s.Block(entries)
	if out := s.Block(entries); out != "" {
		t.Fatalf("unchanged memory must add nothing to the turn, got:\n%s", out)
	}
}

func TestDeltaCarriesOnlyWhatChanged(t *testing.T) {
	var s Shown
	a := entry(1, "a", "UNCHANGED-ENTRY")
	b := entry(2, "b", "old wording")
	s.Block([]Entry{a, b})

	b2 := b
	b2.Text = "new wording"
	c := entry(3, "c", "ADDED-ENTRY")
	out := s.Block([]Entry{a, b2, c})

	if !strings.Contains(out, "<memory-update>") {
		t.Fatalf("expected a delta block:\n%s", out)
	}
	if strings.Contains(out, "UNCHANGED-ENTRY") {
		t.Errorf("the delta must not repeat unchanged entries:\n%s", out)
	}
	for _, want := range []string{"new wording", "ADDED-ENTRY"} {
		if !strings.Contains(out, want) {
			t.Errorf("delta misses %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "old wording") {
		t.Errorf("the delta must not carry the replaced text:\n%s", out)
	}
}

func TestForgottenEntriesAreNamedByTopicKey(t *testing.T) {
	var s Shown
	s.Block([]Entry{entry(1, "drain", "drain the node first"), entry(2, "", "untopic text")})
	out := s.Block([]Entry{entry(2, "", "untopic text")})
	if !strings.Contains(out, "Removed:") || !strings.Contains(out, "- drain") {
		t.Fatalf("a forgotten entry is named by its topic key:\n%s", out)
	}
	if strings.Contains(out, "drain the node first") {
		t.Errorf("a removed entry must not repeat its text:\n%s", out)
	}
}

func TestReviewStateChangeIsADelta(t *testing.T) {
	var s Shown
	e := entry(1, "a", "text")
	s.Block([]Entry{e})
	e.State = StateNeedsReview
	out := s.Block([]Entry{e})
	if !strings.Contains(out, "(review)") {
		t.Fatalf("an entry that now needs review is a change:\n%s", out)
	}
}

func TestTaintedEntriesDoNotChurnBetweenTurns(t *testing.T) {
	var s Shown
	e := entry(1, "a", "fetched text")
	e.Tainted = true
	s.Block([]Entry{e})
	if out := s.Block([]Entry{e}); out != "" {
		t.Fatalf("a tainted entry rendered again must not look changed, got:\n%s", out)
	}
}

func TestResetGivesTheFullBlockAgain(t *testing.T) {
	var s Shown
	entries := []Entry{entry(1, "drain", "drain the node first")}
	s.Block(entries)
	s.Reset()
	out := s.Block(entries)
	if !strings.Contains(out, "<memory>") {
		t.Fatalf("after a reset the full memory is sent again, got:\n%s", out)
	}
}
