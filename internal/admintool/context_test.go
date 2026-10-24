package admintool

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/tinchoram/groot-cli/internal/tool"
)

// ContextReport and SetContextWindow are the fake's defaults; contextManager
// overrides them.
func (f *fakeManager) ContextReport(context.Context, string) (string, error) { return "", nil }

func (f *fakeManager) SetContextWindow(string, int) error { return nil }

// contextManager records what the context tools ask for.
type contextManager struct {
	fakeManager
	report     string
	reportErr  error
	asked      []string
	windows    map[string]int
	windowsErr error
}

func (m *contextManager) ContextReport(_ context.Context, space string) (string, error) {
	m.asked = append(m.asked, space)
	return m.report, m.reportErr
}

func (m *contextManager) SetContextWindow(model string, tokens int) error {
	if m.windowsErr != nil {
		return m.windowsErr
	}
	if m.windows == nil {
		m.windows = map[string]int{}
	}
	m.windows[model] = tokens
	return nil
}

func TestGetContextReturnsTheTableOfTheNamedSpace(t *testing.T) {
	m := &contextManager{report: "SPACE  AGENT\ndefault  reader"}
	out, isErr := NewGetContext(m).Execute(context.Background(), `{"space":"default"}`)
	if isErr || !strings.Contains(out, "reader") {
		t.Fatalf("get_context = %q, error=%v", out, isErr)
	}
	if len(m.asked) != 1 || m.asked[0] != "default" {
		t.Fatalf("manager asked for %v, want the named space", m.asked)
	}
}

func TestGetContextWithoutSpaceUsesTheActiveOne(t *testing.T) {
	m := &contextManager{report: "table"}
	if _, isErr := NewGetContext(m).Execute(context.Background(), `{}`); isErr {
		t.Fatal("get_context without a space must not fail")
	}
	if len(m.asked) != 1 || m.asked[0] != "" {
		t.Fatalf("manager asked for %v, want the active space (empty name)", m.asked)
	}
}

func TestGetContextReportsManagerErrors(t *testing.T) {
	m := &contextManager{reportErr: errors.New("unknown space \"nope\"")}
	out, isErr := NewGetContext(m).Execute(context.Background(), `{"space":"nope"}`)
	if !isErr || !strings.Contains(out, "unknown space") {
		t.Fatalf("get_context error = %q, %v", out, isErr)
	}
}

func TestSetContextWindowSavesTheOverride(t *testing.T) {
	m := &contextManager{}
	out, isErr := NewSetContextWindow(m).Execute(context.Background(), `{"model":"claude-haiku-5-5","tokens":128000}`)
	if isErr {
		t.Fatalf("set_context_window failed: %s", out)
	}
	if m.windows["claude-haiku-5-5"] != 128000 {
		t.Fatalf("saved windows = %v", m.windows)
	}
	if !strings.Contains(out, "128000") {
		t.Fatalf("reply should name the tokens: %q", out)
	}
}

func TestSetContextWindowRefusesBadInput(t *testing.T) {
	m := &contextManager{}
	cases := []string{
		`{"model":"claude-haiku-5-5","tokens":0}`,
		`{"model":"claude-haiku-5-5"}`,
		`{"tokens":1000}`,
	}
	for _, in := range cases {
		if _, isErr := NewSetContextWindow(m).Execute(context.Background(), in); !isErr {
			t.Errorf("set_context_window %s must be refused", in)
		}
	}
	if len(m.windows) != 0 {
		t.Fatalf("refused input must save nothing: %v", m.windows)
	}
}

func TestContextToolsRisk(t *testing.T) {
	m := &contextManager{}
	if r := NewGetContext(m).Risk(); r != tool.RiskRead {
		t.Fatalf("get_context risk = %v, want read", r)
	}
	if r := NewSetContextWindow(m).Risk(); r != tool.RiskWrite {
		t.Fatalf("set_context_window risk = %v, want write", r)
	}
}
