package workspace

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/tinchoram/groot-cli/internal/api"
	"github.com/tinchoram/groot-cli/internal/catalog"
	"github.com/tinchoram/groot-cli/internal/inbox"
	"github.com/tinchoram/groot-cli/internal/provider"
	"github.com/tinchoram/groot-cli/internal/tool"
)

func sendCall(id, to, subject, body string) provider.Step {
	in, _ := json.Marshal(map[string]string{"to": to, "subject": subject, "body": body})
	return provider.Step{Response: api.Response{
		Content:    []api.Block{{Type: api.BlockToolUse, ID: id, Name: "send_message", Input: in}},
		StopReason: api.StopToolUse,
	}}
}

// addAgentToDefault assigns a second agent to the fixture's space without a catalog entry;
// recipients only need the assignment to receive messages.
func addAgentToDefault(t *testing.T, f *fixture, name string) {
	t.Helper()
	if err := f.spaces.Assign("default", name); err != nil {
		t.Fatal(err)
	}
}

func TestSendToAgentInSameSpace(t *testing.T) {
	f := newFixture(t)
	addAgentToDefault(t, f, "writer")
	msgr := f.ws.messengerFor("default", "reader")
	recipient, id, err := msgr.Send(context.Background(), "writer", "hi", "body")
	if err != nil || recipient != "default/writer" || id == 0 {
		t.Fatalf("Send = %q, %d, %v", recipient, id, err)
	}
	got, _ := f.ws.inbox.Unread(context.Background(), "default", "writer")
	if len(got) != 1 || got[0].FromAgent != "reader" || got[0].FromSpace != "default" {
		t.Fatalf("writer inbox = %+v", got)
	}
}

func TestSendQualifiedAddress(t *testing.T) {
	f := newFixture(t)
	addAgentToDefault(t, f, "writer")
	recipient, _, err := f.ws.messengerFor("default", "reader").Send(context.Background(), "default/writer", "s", "b")
	if err != nil || recipient != "default/writer" {
		t.Fatalf("Send = %q, %v", recipient, err)
	}
}

func TestSendToGrootResolvesToRootSpace(t *testing.T) {
	f := newFixture(t)
	if err := f.spaces.Add("root", t.TempDir(), t0); err != nil {
		t.Fatal(err)
	}
	if err := f.spaces.Assign("root", "groot"); err != nil {
		t.Fatal(err)
	}
	recipient, _, err := f.ws.messengerFor("default", "reader").Send(context.Background(), "groot", "spec", "build it")
	if err != nil || recipient != "root/groot" {
		t.Fatalf("Send = %q, %v", recipient, err)
	}
}

func TestSendRefusesUnassignedRecipient(t *testing.T) {
	f := newFixture(t)
	_, _, err := f.ws.messengerFor("default", "reader").Send(context.Background(), "nobody", "s", "b")
	if err == nil || !strings.Contains(err.Error(), "use <space>/<agent>") {
		t.Fatalf("err = %v", err)
	}
	_, _, err = f.ws.messengerFor("default", "reader").Send(context.Background(), "root/groot", "s", "b")
	if err == nil {
		t.Fatal("a space that does not exist must be refused")
	}
}

func TestSendMessageToolRoundTripBetweenAgents(t *testing.T) {
	f := newFixture(t, sendCall("tu_1", "writer", "review", "check notes.txt"), text("sent"))
	addAgentToDefault(t, f, "writer")
	if err := catalog.Save(f.home, catalog.AgentDef{Name: "reader", Description: "reads files",
		Tools: []string{"read_file", "send_message", "read_inbox"}}, "You read files carefully."); err != nil {
		t.Fatal(err)
	}
	if err := f.ws.reload(); err != nil {
		t.Fatal(err)
	}
	if err := f.ws.SelectAgent("reader"); err != nil {
		t.Fatal(err)
	}
	reply, err := f.ws.Chat().Send(context.Background(), "ask writer to review")
	if err != nil || reply != "sent" {
		t.Fatalf("Send = %q, %v", reply, err)
	}
	result := f.mock.Calls[1].Messages[len(f.mock.Calls[1].Messages)-1].Content[0]
	if result.Type != api.BlockToolResult || result.Content != "delivered to default/writer (#1)" {
		t.Fatalf("tool result = %+v", result)
	}

	// The receiving agent reads it through read_inbox with its own messenger.
	reader := f.ws.messengerFor("default", "writer")
	out, isErr := tool.NewReadInbox(reader).Execute(context.Background(), `{}`)
	if isErr || !strings.Contains(out, "Subject: review") || !strings.Contains(out, "check notes.txt") {
		t.Fatalf("read_inbox = %q (err %v)", out, isErr)
	}
	out, _ = tool.NewReadInbox(reader).Execute(context.Background(), `{}`)
	if out != "no unread messages" {
		t.Fatalf("messages must be marked read, got %q", out)
	}
}

func TestTurnStartsWithInboxNoticeWhenUnread(t *testing.T) {
	f := newFixture(t, text("ok"))
	if _, err := f.ws.inbox.Send(context.Background(), inbox.Message{
		FromSpace: "root", FromAgent: "groot", ToSpace: "default", ToAgent: "reader", Subject: "s", Body: "b",
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.ws.SelectAgent("reader"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.ws.Chat().Send(context.Background(), "what's up"); err != nil {
		t.Fatal(err)
	}
	first := f.mock.Calls[0].Messages[0].Content[0].Text
	want := "[inbox] from root/groot"
	if !strings.Contains(first, want) || !strings.HasSuffix(first, "what's up") {
		t.Fatalf("prompt = %q", first)
	}
	// The notice does not mark anything read.
	if n, _ := f.ws.inbox.CountUnread(context.Background(), "default", "reader"); n != 0 {
		t.Fatalf("the delivered message is marked read, unread = %d", n)
	}
}

func TestTurnWithoutUnreadHasNoNotice(t *testing.T) {
	f := newFixture(t, text("ok"))
	_ = f.ws.SelectAgent("reader")
	if _, err := f.ws.Chat().Send(context.Background(), "plain"); err != nil {
		t.Fatal(err)
	}
	if got := f.mock.Calls[0].Messages[0].Content[0].Text; !strings.HasPrefix(got, "Current time: ") || !strings.HasSuffix(got, "\n\nplain") {
		t.Fatalf("prompt = %q", got)
	}
}

func TestUnreadCountsKeyedBySpaceAndAgent(t *testing.T) {
	f := newFixture(t)
	_, _, _ = f.ws.messengerFor("default", "reader").Send(context.Background(), "reader", "self", "note")
	counts, err := f.ws.UnreadCounts(context.Background())
	if err != nil || counts["default/reader"] != 1 {
		t.Fatalf("counts = %v, %v", counts, err)
	}
}

func TestHandSendAndInboxListing(t *testing.T) {
	f := newFixture(t)
	_ = f.ws.SelectAgent("reader")
	if _, err := f.ws.SendByHand(context.Background(), "reader", "note", "from the user"); err != nil {
		t.Fatal(err)
	}
	msgs, err := f.ws.Inbox(context.Background(), 10)
	if err != nil || len(msgs) != 1 || msgs[0].Body != "from the user" || msgs[0].FromAgent != "user" {
		t.Fatalf("Inbox = %+v, %v", msgs, err)
	}
}
