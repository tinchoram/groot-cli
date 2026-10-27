package compact

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/tinchoram/groot-cli/internal/api"
	"github.com/tinchoram/groot-cli/internal/provider"
)

func user(text string) api.Message {
	return api.Message{Role: api.RoleUser, Content: []api.Block{{Type: api.BlockText, Text: text}}}
}

func assistant(text string) api.Message {
	return api.Message{Role: api.RoleAssistant, Content: []api.Block{{Type: api.BlockText, Text: text}}}
}

func toolUse(id string) api.Message {
	return api.Message{Role: api.RoleAssistant, Content: []api.Block{{Type: api.BlockToolUse, ID: id, Name: "bash", Input: json.RawMessage(`{}`)}}}
}

func toolResult(id string) api.Message {
	return api.Message{Role: api.RoleUser, Content: []api.Block{{Type: api.BlockToolResult, ToolUseID: id, Content: "ok"}}}
}

func TestSafeSplitPoint(t *testing.T) {
	cases := []struct {
		name    string
		msgs    []api.Message
		desired int
		want    int
	}{
		{"plain split", []api.Message{user("a"), assistant("b"), user("c"), assistant("d")}, 2, 2},
		{"desired lands on assistant, back off to user", []api.Message{user("a"), assistant("b"), user("c"), assistant("d")}, 3, 2},
		{"never split before a tool_result", []api.Message{user("a"), toolUse("t1"), toolResult("t1"), assistant("done"), user("next")}, 2, 0},
		{"skip past tool pair to a later plain user", []api.Message{user("a"), toolUse("t1"), toolResult("t1"), assistant("done"), user("next")}, 4, 4},
		{"desired beyond length is clamped", []api.Message{user("a"), assistant("b"), user("c")}, 10, 2},
		{"single message has no split", []api.Message{user("a")}, 1, 0},
		{"zero desired returns zero", []api.Message{user("a"), assistant("b"), user("c")}, 0, 0},
		{"empty", nil, 3, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := SafeSplitPoint(c.msgs, c.desired); got != c.want {
				t.Fatalf("SafeSplitPoint(...,%d) = %d, want %d", c.desired, got, c.want)
			}
		})
	}
}

func TestNoneIsIdentity(t *testing.T) {
	msgs := []api.Message{user("a"), assistant("b")}
	out, err := None{}.Compact(context.Background(), msgs)
	if err != nil || len(out) != 2 {
		t.Fatalf("None changed the history: %v %d", err, len(out))
	}
}

func TestSlidingWindow(t *testing.T) {
	msgs := []api.Message{user("1"), assistant("2"), user("3"), assistant("4"), user("5"), assistant("6")}
	// Keep=4 -> desired index 2 is plain user "3", so the kept tail is msgs[2:].
	tests := []struct {
		keep     int
		wantLen  int
		wantText string // text of the first kept message
	}{
		{10, 6, "1"}, // keep >= length: nothing dropped
		{2, 2, "5"},
		{4, 4, "3"},
	}
	for _, tc := range tests {
		out, err := SlidingWindow{Keep: tc.keep}.Compact(context.Background(), msgs)
		if err != nil {
			t.Fatal(err)
		}
		if len(out) != tc.wantLen {
			t.Fatalf("keep=%d: len %d want %d", tc.keep, len(out), tc.wantLen)
		}
		if out[0].Content[0].Text != tc.wantText {
			t.Fatalf("keep=%d: first kept %q want %q", tc.keep, out[0].Content[0].Text, tc.wantText)
		}
	}
}

func TestSlidingWindowNeverSplitsToolPair(t *testing.T) {
	msgs := []api.Message{user("a"), toolUse("t1"), toolResult("t1"), assistant("done"), user("b"), assistant("c")}
	out, err := SlidingWindow{Keep: 3}.Compact(context.Background(), msgs)
	if err != nil {
		t.Fatal(err)
	}
	if out[0].Role != api.RoleUser || out[0].Content[0].Type != api.BlockText {
		t.Fatalf("window must start at a plain user message: %+v", out[0])
	}
}

type fakeProvider struct {
	calls   int
	reply   string
	err     error
	lastReq provider.Request
}

func (f *fakeProvider) Send(_ context.Context, req provider.Request) (api.Response, error) {
	f.calls++
	f.lastReq = req
	if f.err != nil {
		return api.Response{}, f.err
	}
	return api.Response{Content: []api.Block{{Type: api.BlockText, Text: f.reply}}, StopReason: api.StopEndTurn}, nil
}
func (f *fakeProvider) Model() string   { return "fake" }
func (f *fakeProvider) SetModel(string) {}

func TestSummarizeReplacesPrefixWithPair(t *testing.T) {
	var msgs []api.Message
	for i := 0; i < 8; i++ {
		if i%2 == 0 {
			msgs = append(msgs, user("question"))
		} else {
			msgs = append(msgs, assistant("answer"))
		}
	}
	fp := &fakeProvider{reply: "SUMMARY: cluster healthy"}
	out, err := Summarize{Provider: fp, Threshold: 4}.Compact(context.Background(), msgs)
	if err != nil {
		t.Fatal(err)
	}
	if fp.calls != 1 {
		t.Fatalf("summarizer called %d times, want 1", fp.calls)
	}
	if len(fp.lastReq.Messages) != 1 || !strings.Contains(fp.lastReq.Messages[0].Content[0].Text, "question") {
		t.Fatalf("summarizer must receive the dropped prefix as transcript")
	}
	if len(out) >= len(msgs) {
		t.Fatalf("history not shortened: %d -> %d", len(msgs), len(out))
	}
	if out[0].Role != api.RoleUser || !strings.Contains(out[0].Content[0].Text, "SUMMARY: cluster healthy") {
		t.Fatalf("first message should carry the summary: %+v", out[0])
	}
	if out[1].Role != api.RoleAssistant {
		t.Fatalf("second message should be the assistant acknowledgement")
	}
}

func TestSummarizeBelowThresholdIsNoop(t *testing.T) {
	fp := &fakeProvider{reply: "x"}
	msgs := []api.Message{user("a"), assistant("b")}
	out, err := Summarize{Provider: fp, Threshold: 10}.Compact(context.Background(), msgs)
	if err != nil || fp.calls != 0 || len(out) != 2 {
		t.Fatalf("below threshold must not call the model: calls=%d err=%v len=%d", fp.calls, err, len(out))
	}
}

func TestSummarizePropagatesProviderError(t *testing.T) {
	fp := &fakeProvider{err: errors.New("rate limited")}
	msgs := []api.Message{user("1"), assistant("2"), user("3"), assistant("4"), user("5"), assistant("6")}
	if _, err := (Summarize{Provider: fp, Threshold: 2}).Compact(context.Background(), msgs); err == nil {
		t.Fatal("provider error must surface")
	}
}

func TestSummaryPromptKeepsUserRequestsAndTheOpenTask(t *testing.T) {
	fp := &fakeProvider{reply: "SUMMARY"}
	msgs := []api.Message{user("scale checkout to 4 replicas"), user("ok")}
	if _, err := Digest(context.Background(), fp, msgs); err != nil {
		t.Fatal(err)
	}
	prompt := fp.lastReq.Messages[0].Content[0].Text
	for _, want := range []string{
		"every user request", "verbatim", "the open task", "decisions",
		"resource names", "commands that were run", "outcomes", "open problems",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("summary prompt misses %q:\n%s", want, prompt)
		}
	}
	if !strings.Contains(prompt, "scale checkout to 4 replicas") {
		t.Error("the summary request must carry the transcript")
	}
}
