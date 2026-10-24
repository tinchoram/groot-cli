package gate

import (
	"context"
	"testing"

	"github.com/tinchoram/groot-cli/internal/tool"
)

func TestWouldMatchesTheDecisionWithoutCounting(t *testing.T) {
	s, _ := newWork(t, true, nil, 1, 0)
	req := Request{Tool: "write_file", Input: `{"path":"a.go"}`, Risk: tool.RiskWrite}
	if !s.Would(req) {
		t.Fatal("inside the space and within the limit, work mode would approve")
	}
	if !s.Would(req) {
		t.Fatal("Would must not count the call")
	}
	Work{Inner: &asker{}, State: s}.Decide(context.Background(), req)
	if s.Would(req) {
		t.Fatal("the turn limit is reached after one approval")
	}
	s.Set(false)
	if s.Would(Request{Tool: "write_file", Input: `{"path":"b"}`, Risk: tool.RiskWrite}) {
		t.Fatal("with work mode off nothing is auto-approved")
	}
}
