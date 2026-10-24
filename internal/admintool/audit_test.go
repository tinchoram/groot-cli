package admintool

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/tinchoram/groot-cli/internal/audit"
	"github.com/tinchoram/groot-cli/internal/tool"
)

func (f *fakeManager) AuditReport(context.Context, string, audit.Query) (string, error) {
	return "", nil
}

// auditManager records what audit_query asks for and answers with a fixed table.
type auditManager struct {
	fakeManager
	space string
	query audit.Query
	out   string
}

func (m *auditManager) AuditReport(_ context.Context, space string, q audit.Query) (string, error) {
	m.space, m.query = space, q
	return m.out, nil
}

func TestAuditQueryPassesTheFiltersThrough(t *testing.T) {
	m := &auditManager{out: "TIME  AGENT\n10:00:00  reader"}
	before := time.Now()
	out, isErr := NewAuditQuery(m).Execute(context.Background(), `{"space":"work","tool":"bash","decision":"deny","agent":"developer","guard_only":true,"errors_only":true,"find":"rm -rf","since":"2h","limit":5}`)
	if isErr {
		t.Fatalf("audit_query failed: %s", out)
	}
	if out != m.out {
		t.Fatalf("output = %q, want the manager's table", out)
	}
	q := m.query
	if m.space != "work" || q.Tool != "bash" || q.Decision != "deny" || q.Agent != "developer" ||
		!q.GuardOnly || !q.ErrorsOnly || q.Find != "rm -rf" || q.Limit != 5 {
		t.Fatalf("query = %+v in space %q", q, m.space)
	}
	if d := before.Add(-2 * time.Hour).Sub(q.Since); d > time.Minute || d < -time.Minute {
		t.Fatalf("since = %v, want about two hours back", q.Since)
	}
}

func TestAuditQueryDefaultsToTwentyRecordsOfTheActiveSpace(t *testing.T) {
	m := &auditManager{out: "no audit records"}
	if _, isErr := NewAuditQuery(m).Execute(context.Background(), `{}`); isErr {
		t.Fatal("an empty query is valid")
	}
	if m.space != "" || m.query.Limit != 20 {
		t.Fatalf("space %q limit %d, want the active space and 20", m.space, m.query.Limit)
	}
}

func TestAuditQueryRefusesBadInput(t *testing.T) {
	m := &auditManager{}
	for _, in := range []string{
		`{"since":"yesterday"}`,
		`{"limit":-1}`,
		`{"unknown":true}`,
	} {
		if _, isErr := NewAuditQuery(m).Execute(context.Background(), in); !isErr {
			t.Errorf("input %s must be refused", in)
		}
	}
}

func TestAuditQueryIsReadOnlyAndPreviewsTheSpace(t *testing.T) {
	tl := NewAuditQuery(&auditManager{})
	if name := tl.Definition().Name; name != "audit_query" || tl.RiskFor("") != tool.RiskRead {
		t.Fatalf("audit_query must be a read tool named audit_query, got %q risk %v", name, tl.RiskFor(""))
	}
	if p := tl.Preview(`{"space":"work"}`); !strings.Contains(p, "work") {
		t.Fatalf("preview = %q", p)
	}
}
