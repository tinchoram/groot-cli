package workspace

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/tinchoram/groot-cli/internal/api"
	"github.com/tinchoram/groot-cli/internal/provider"
)

// longSession is a stored session of n alternating messages, user first.
func longSession(n int) []api.Message {
	msgs := make([]api.Message, 0, n)
	for i := 0; i < n; i++ {
		if i%2 == 0 {
			msgs = append(msgs, plain(api.RoleUser, fmt.Sprintf("u%d", i)))
		} else {
			msgs = append(msgs, plain(api.RoleAssistant, fmt.Sprintf("a%d", i)))
		}
	}
	return msgs
}

func TestResumeSummarizesTheMessagesOutOfContext(t *testing.T) {
	// 230 stored messages: the transcript keeps the last 200, so 30 are out of
	// context. Steps: the summary of those 30, the compaction that the restored
	// history triggers at its first send, then the answer to the new prompt.
	f := newFixture(t, text("DIGEST: checkout scaled to 4 replicas"), text("compacted"), text("ok"))
	seedStored(t, f, "s-long", longSession(230)...)
	if err := f.ws.SelectAgent("reader"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.ws.Chat().Send(t.Context(), "continue"); err != nil {
		t.Fatal(err)
	}
	if len(f.mock.Calls) != 3 {
		t.Fatalf("want the summary, the compaction and the answer, got %d calls", len(f.mock.Calls))
	}
	if !strings.Contains(f.mock.Calls[0].Messages[0].Content[0].Text, "every user request") {
		t.Fatal("the summary call must use the compaction rules")
	}
	// The compaction of the restored history reads the summary of the dropped
	// messages, which sits in front of the stored ones.
	compacted := lastUserText(f.mock.Calls[1])
	if !strings.Contains(compacted, "Summary of the 30 earlier messages") || !strings.Contains(compacted, "DIGEST: checkout scaled to 4 replicas") {
		t.Fatalf("the model must see the summary of the messages out of context, got:\n%s", compacted)
	}
	if got := len(f.mock.Calls[2].Messages); got >= 2+200+1 {
		t.Fatalf("the answer must read the compacted history, got %d messages", got)
	}
}

func TestResumeNotesTheDroppedMessagesWhenTheSummaryFails(t *testing.T) {
	f := newFixture(t, provider.Step{Err: errors.New("provider unavailable")}, text("compacted"), text("ok"))
	seedStored(t, f, "s-long", longSession(230)...)
	if err := f.ws.SelectAgent("reader"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.ws.Chat().Send(t.Context(), "continue"); err != nil {
		t.Fatal(err)
	}
	if note := lastUserText(f.mock.Calls[1]); !strings.Contains(note, "30 earlier messages are not in context.") {
		t.Fatalf("a failed summary must leave a note with the count, got:\n%s", note)
	}
}

func TestShortResumeSendsNoSummaryCall(t *testing.T) {
	f := newFixture(t, text("ok"))
	seedStored(t, f, "s-short", longSession(20)...)
	if err := f.ws.SelectAgent("reader"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.ws.Chat().Send(t.Context(), "continue"); err != nil {
		t.Fatal(err)
	}
	if len(f.mock.Calls) != 1 {
		t.Fatalf("a session within the cap needs no summary, got %d calls", len(f.mock.Calls))
	}
}
