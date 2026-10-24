package tool

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/tinchoram/groot-cli/internal/inbox"
)

// fakeMessenger records sends and serves a fixed inbox.
type fakeMessenger struct {
	sent     []string
	unread   []inbox.Message
	marked   []int64
	sendErr  error
	nextID   int64
	resolved string
}

func (f *fakeMessenger) Send(_ context.Context, to, subject, body string) (string, int64, error) {
	if f.sendErr != nil {
		return "", 0, f.sendErr
	}
	f.nextID++
	f.sent = append(f.sent, to+"|"+subject+"|"+body)
	return f.resolved, f.nextID, nil
}

func (f *fakeMessenger) Unread(context.Context) ([]inbox.Message, error) {
	return f.unread, nil
}

func (f *fakeMessenger) MarkRead(_ context.Context, ids []int64) error {
	f.marked = append(f.marked, ids...)
	return nil
}

func TestSendMessageDeliversThroughMessenger(t *testing.T) {
	f := &fakeMessenger{resolved: "root/groot"}
	out, isErr := NewSendMessage(f).Execute(context.Background(),
		`{"to":"groot","subject":"new space","body":"create payments"}`)
	if isErr {
		t.Fatalf("send failed: %s", out)
	}
	if out != "delivered to root/groot (#1)" {
		t.Fatalf("output = %q", out)
	}
	if len(f.sent) != 1 || f.sent[0] != "groot|new space|create payments" {
		t.Fatalf("sent = %v", f.sent)
	}
}

func TestSendMessageReportsRecipientErrors(t *testing.T) {
	f := &fakeMessenger{sendErr: errors.New("unknown recipient; use <space>/<agent>")}
	out, isErr := NewSendMessage(f).Execute(context.Background(), `{"to":"nobody","subject":"s","body":"b"}`)
	if !isErr || !strings.Contains(out, "unknown recipient") {
		t.Fatalf("out=%q isErr=%v", out, isErr)
	}
}

func TestSendMessageRejectsBadInput(t *testing.T) {
	f := &fakeMessenger{}
	for _, in := range []string{`{}`, `{"to":"groot"}`, `not json`} {
		if _, isErr := NewSendMessage(f).Execute(context.Background(), in); !isErr {
			t.Fatalf("input %s should be an error", in)
		}
	}
	if len(f.sent) != 0 {
		t.Fatal("invalid input must not deliver anything")
	}
}

func TestSendMessageRiskAndPreview(t *testing.T) {
	st := NewSendMessage(&fakeMessenger{})
	if st.Risk() != RiskWrite || st.RiskFor(`{}`) != RiskWrite {
		t.Fatal("send_message is a write")
	}
	p := st.Preview(`{"to":"payments/reviewer","subject":"review","body":"line1\nline2"}`)
	if !strings.HasPrefix(p, "message to payments/reviewer: review") || !strings.Contains(p, "line2") {
		t.Fatalf("preview = %q", p)
	}
}

func TestReadInboxFormatsAndMarksRead(t *testing.T) {
	at := time.Date(2026, 10, 24, 14, 30, 0, 0, time.UTC)
	f := &fakeMessenger{unread: []inbox.Message{
		{ID: 7, FromSpace: "root", FromAgent: "groot", Subject: "review", Body: "check the diff", CreatedAt: at},
	}}
	out, isErr := NewReadInbox(f).Execute(context.Background(), `{}`)
	if isErr {
		t.Fatalf("read failed: %s", out)
	}
	for _, want := range []string{"#7", "from root/groot", "Subject: review", "check the diff"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output %q misses %q", out, want)
		}
	}
	if len(f.marked) != 1 || f.marked[0] != 7 {
		t.Fatalf("marked = %v", f.marked)
	}
}

func TestReadInboxWithNothingUnread(t *testing.T) {
	f := &fakeMessenger{}
	out, isErr := NewReadInbox(f).Execute(context.Background(), `{}`)
	if isErr || out != "no unread messages" {
		t.Fatalf("out=%q isErr=%v", out, isErr)
	}
	if len(f.marked) != 0 {
		t.Fatal("nothing to mark")
	}
}

func TestReadInboxIsReadOnlyRisk(t *testing.T) {
	r := NewReadInbox(&fakeMessenger{})
	if r.Risk() != RiskRead || r.RiskFor(`{}`) != RiskRead {
		t.Fatal("read_inbox only reads the user's inbox")
	}
}
