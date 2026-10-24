package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tinchoram/groot-cli/internal/api"
	"github.com/tinchoram/groot-cli/internal/guard"
	"github.com/tinchoram/groot-cli/internal/inbox"
)

// Messenger is the port the messaging tools use. The workspace implements it
// for the running conversation, so the sender and the addressing rules stay
// out of this package.
type Messenger interface {
	// Send delivers a message to "to" and returns the resolved recipient
	// ("<space>/<agent>") and the message id.
	Send(ctx context.Context, to, subject, body string) (recipient string, id int64, err error)
	// Unread returns the sender's unread messages, oldest first.
	Unread(ctx context.Context) ([]inbox.Message, error)
	// MarkRead marks the given messages as read.
	MarkRead(ctx context.Context, ids []int64) error
}

// SendMessage delivers a note or specification to another agent or to groot.
type SendMessage struct {
	messenger Messenger
}

// NewSendMessage returns the send_message tool.
func NewSendMessage(m Messenger) *SendMessage { return &SendMessage{messenger: m} }

type sendMessageInput struct {
	To      string `json:"to"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

func (*SendMessage) Definition() api.ToolDef {
	return api.ToolDef{
		Name:        "send_message",
		Description: "Send a message to another agent or to groot. Use \"groot\" for groot, \"<agent>\" for an agent in this space, or \"<space>/<agent>\". Keep requests concrete: goal, constraints, definition of done.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"to":{"type":"string"},"subject":{"type":"string"},"body":{"type":"string"}},"required":["to","body"]}`),
	}
}

// Risk is RiskWrite: a message changes another agent's inbox.
func (*SendMessage) Risk() Risk { return RiskWrite }

// RiskFor is the static risk; every send is a write.
func (s *SendMessage) RiskFor(string) Risk { return s.Risk() }

const previewBodyLines = 5

func (*SendMessage) Preview(input string) string {
	var in sendMessageInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return "send_message: invalid input"
	}
	lines := strings.Split(in.Body, "\n")
	if len(lines) > previewBodyLines {
		lines = append(lines[:previewBodyLines], "...")
	}
	return Redact(fmt.Sprintf("message to %s: %s\n%s", in.To, in.Subject, strings.Join(lines, "\n")))
}

func (s *SendMessage) Execute(ctx context.Context, input string) (string, bool) {
	out, isErr := s.execute(ctx, input)
	return Redact(out), isErr
}

func (s *SendMessage) execute(ctx context.Context, input string) (string, bool) {
	var in sendMessageInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return "invalid input: " + err.Error(), true
	}
	if strings.TrimSpace(in.To) == "" {
		return "to is required", true
	}
	if strings.TrimSpace(in.Body) == "" {
		return "body is required", true
	}
	recipient, id, err := s.messenger.Send(ctx, in.To, in.Subject, in.Body)
	if err != nil {
		return err.Error(), true
	}
	return fmt.Sprintf("delivered to %s (#%d)", recipient, id), false
}

// ReadInbox returns the unread messages for the running agent and marks them read.
type ReadInbox struct {
	messenger Messenger
	// Guard envelopes each body with its sender as the source. Nil, or an off
	// guard, returns the bodies as they are.
	Guard *guard.Guard
}

// NewReadInbox returns the read_inbox tool.
func NewReadInbox(m Messenger) *ReadInbox { return &ReadInbox{messenger: m} }

func (*ReadInbox) Definition() api.ToolDef {
	return api.ToolDef{
		Name:        "read_inbox",
		Description: "Read your unread messages from other agents and from groot. They are marked read once returned.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{}}`),
	}
}

// Risk is RiskRead: the tool reads the agent's own inbox.
func (*ReadInbox) Risk() Risk { return RiskRead }

// Untrusted is true: a message body comes from another agent, which may be
// compromised. SelfEnveloped is true: each body is wrapped here with its
// sender as the source, so the agent only scans the output.
func (*ReadInbox) Untrusted() bool { return true }

// SelfEnveloped is true; see Untrusted.
func (*ReadInbox) SelfEnveloped() bool { return true }

// RiskFor is the static risk.
func (r *ReadInbox) RiskFor(string) Risk { return r.Risk() }

func (*ReadInbox) Preview(string) string { return "read_inbox" }

// body is the message text as the model reads it. The subject stays outside
// the envelope, so delegation subjects keep their plain form.
func (r *ReadInbox) body(m inbox.Message) string {
	return r.Guard.Wrap("message from "+m.FromSpace+"/"+m.FromAgent, m.Body)
}

func (r *ReadInbox) Execute(ctx context.Context, input string) (string, bool) {
	out, isErr := r.execute(ctx)
	return Redact(out), isErr
}

func (r *ReadInbox) execute(ctx context.Context) (string, bool) {
	msgs, err := r.messenger.Unread(ctx)
	if err != nil {
		return err.Error(), true
	}
	if len(msgs) == 0 {
		return "no unread messages", false
	}
	var sb strings.Builder
	ids := make([]int64, 0, len(msgs))
	for i, m := range msgs {
		if i > 0 {
			sb.WriteString("\n\n")
		}
		fmt.Fprintf(&sb, "#%d · from %s/%s · %s\nSubject: %s\n%s",
			m.ID, m.FromSpace, m.FromAgent, m.CreatedAt.Format("2006-01-02 15:04"), m.Subject, r.body(m))
		ids = append(ids, m.ID)
	}
	if err := r.messenger.MarkRead(ctx, ids); err != nil {
		return err.Error(), true
	}
	return sb.String(), false
}
