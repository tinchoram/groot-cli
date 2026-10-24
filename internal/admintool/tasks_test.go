package admintool

import (
	"context"
	"testing"

	"github.com/tinchoram/groot-cli/internal/tool"
)

// TaskReport is the fake's default; taskManager overrides it.
func (f *fakeManager) TaskReport(context.Context, string, bool) (string, error) { return "", nil }

// taskManager records what list_tasks asks for.
type taskManager struct {
	fakeManager
	report    string
	reportErr error
	asked     []string
	closed    []bool
}

func (m *taskManager) TaskReport(_ context.Context, space string, includeClosed bool) (string, error) {
	m.asked = append(m.asked, space)
	m.closed = append(m.closed, includeClosed)
	return m.report, m.reportErr
}

func TestListTasksReturnsTheTableOfEverySpaceByDefault(t *testing.T) {
	m := &taskManager{report: "SPACE  AGENT\ndefault  reader"}
	out, isErr := NewListTasks(m).Execute(context.Background(), `{}`)
	if isErr || out != m.report {
		t.Fatalf("list_tasks = %q (%v)", out, isErr)
	}
	if len(m.asked) != 1 || m.asked[0] != "" || m.closed[0] {
		t.Fatalf("asked = %v closed = %v, want every space and no closed tasks", m.asked, m.closed)
	}
}

func TestListTasksFiltersBySpaceAndIncludesClosed(t *testing.T) {
	m := &taskManager{report: "table"}
	if _, isErr := NewListTasks(m).Execute(context.Background(), `{"space":"payments","include_closed":true}`); isErr {
		t.Fatal("list_tasks with a space must not fail")
	}
	if m.asked[0] != "payments" || !m.closed[0] {
		t.Fatalf("asked = %v closed = %v, want payments with closed tasks", m.asked, m.closed)
	}
}

func TestListTasksRejectsUnknownFieldsAndReportsErrors(t *testing.T) {
	m := &taskManager{}
	if _, isErr := NewListTasks(m).Execute(context.Background(), `{"agent":"x"}`); !isErr {
		t.Fatal("an unknown field must be refused")
	}
	m.reportErr = context.DeadlineExceeded
	out, isErr := NewListTasks(m).Execute(context.Background(), `{"space":"nope"}`)
	if !isErr || out == "" {
		t.Fatalf("manager error = %q, %v; want an error result", out, isErr)
	}
}

func TestListTasksIsARead(t *testing.T) {
	if r := NewListTasks(&taskManager{}).Risk(); r != tool.RiskRead {
		t.Fatalf("list_tasks risk = %v, want read", r)
	}
}
