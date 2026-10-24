package workspace

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tinchoram/groot-cli/internal/audit"
)

// writeAudit writes records into the audit log at path, the way a space does.
func writeAudit(t *testing.T, path string, recs ...audit.Record) {
	t.Helper()
	l, err := audit.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range recs {
		if err := l.Write(r); err != nil {
			t.Fatal(err)
		}
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestAuditReadsOneSpaceAndAggregatesAll(t *testing.T) {
	f := newFixture(t)
	other := t.TempDir()
	if err := f.spaces.Add("other", other, t0); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	writeAudit(t, filepath.Join(f.dir, ".groot", "audit.jsonl"),
		audit.Record{Time: now.Add(-3 * time.Minute), Space: "default", Agent: "reader", Tool: "read_file", Risk: "read", Decision: "allow"},
		audit.Record{Time: now.Add(-2 * time.Minute), Space: "default", Agent: "reader", Tool: "bash", Risk: "exec", Decision: "allow", IsError: true},
		audit.Record{Time: now.Add(-1 * time.Minute), Space: "default", Agent: "reader", Tool: "bash", Risk: "exec", Decision: "deny"},
	)
	otherLog := filepath.Join(other, ".groot", "audit.jsonl")
	writeAudit(t, otherLog,
		audit.Record{Time: now.Add(-90 * time.Second), Agent: "developer", Tool: "fetch_url", Risk: "network", Decision: "allow", Guard: []string{"override"}},
		audit.Record{Time: now.Add(-30 * time.Second), Agent: "developer", Tool: "write_file", Risk: "write", Decision: "allow"},
	)
	if err := appendRaw(otherLog, "not json\n"); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	active, st, err := f.ws.Audit(ctx, "", audit.Query{Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 3 || active[0].Tool != "bash" || st.Total != 3 || st.Denied != 1 || st.Errors != 1 {
		t.Fatalf("active space: %d records, stats %+v", len(active), st)
	}

	one, st1, err := f.ws.Audit(ctx, "other", audit.Query{Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(one) != 2 || one[0].Tool != "write_file" || st1.Skipped != 1 || st1.Guard != 1 {
		t.Fatalf("other space: %+v, stats %+v", one, st1)
	}

	all, stAll, err := f.ws.AuditAll(ctx, audit.Query{Limit: 4})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 4 {
		t.Fatalf("AuditAll limit 4 returned %d", len(all))
	}
	for i := 1; i < len(all); i++ {
		if all[i].Time.After(all[i-1].Time) {
			t.Fatalf("AuditAll must be newest first: %v before %v", all[i-1].Time, all[i].Time)
		}
	}
	if all[0].Space != "other" || all[0].Tool != "write_file" {
		t.Fatalf("newest record = %+v", all[0])
	}
	if stAll.Total != 5 || stAll.Denied != 1 || stAll.Guard != 1 || stAll.Errors != 1 || stAll.Skipped != 1 {
		t.Fatalf("AuditAll stats = %+v", stAll)
	}
	if stAll.ByRisk["exec"] != 2 || stAll.ByDecision["allow"] != 4 {
		t.Fatalf("AuditAll breakdowns = risk %v decision %v", stAll.ByRisk, stAll.ByDecision)
	}

	if _, _, err := f.ws.Audit(ctx, "missing", audit.Query{}); err == nil {
		t.Fatal("an unknown space must be an error")
	}
}

func TestAuditSkipsSpacesWithoutADirectory(t *testing.T) {
	f := newFixture(t)
	gone := filepath.Join(t.TempDir(), "gone")
	if err := os.Mkdir(gone, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := f.spaces.Add("gone", gone, t0); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(gone); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.ws.AuditAll(context.Background(), audit.Query{}); err != nil {
		t.Fatalf("a space without a directory must be skipped: %v", err)
	}
	if _, err := os.Stat(gone); err == nil {
		t.Fatal("reading the audit must not recreate a space directory")
	}
}

func TestAuditReportFormatsTheTable(t *testing.T) {
	f := newFixture(t)
	writeAudit(t, filepath.Join(f.dir, ".groot", "audit.jsonl"),
		audit.Record{Time: time.Date(2026, 10, 24, 10, 0, 0, 0, time.UTC), Agent: "reader", Tool: "bash", Risk: "exec", Decision: "deny", InputPreview: "rm -rf " + strings.Repeat("x", 80), IsError: true, DurationMS: 4, Sandbox: "none"},
	)
	out, err := f.ws.AuditReport(context.Background(), "", audit.Query{Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"TIME", "AGENT", "TOOL", "RISK", "DECISION", "DUR", "FLAGS", "PREVIEW", "reader", "bash", "exec", "deny", "4ms", "⛔ ✗ ∅"} {
		if !strings.Contains(out, want) {
			t.Fatalf("report lacks %q:\n%s", want, out)
		}
	}
	if none, err := f.ws.AuditReport(context.Background(), "", audit.Query{Tool: "nothing"}); err != nil || none != "no audit records" {
		t.Fatalf("empty report = %q, %v", none, err)
	}
}

func TestAuditFlagsMarkEachKind(t *testing.T) {
	cases := []struct {
		r    audit.Record
		want string
	}{
		{audit.Record{Decision: "allow"}, ""},
		{audit.Record{Decision: "allow", Guard: []string{"command"}}, "⚠"},
		{audit.Record{Decision: "deny"}, "⛔"},
		{audit.Record{Decision: "allow", IsError: true}, "✗"},
		{audit.Record{Decision: "allow", Sandbox: "none"}, "∅"},
		{audit.Record{Decision: "allow", Sandbox: "bwrap"}, ""},
		{audit.Record{Decision: "deny", IsError: true, Guard: []string{"override"}, Sandbox: "none"}, "⚠ ⛔ ✗ ∅"},
	}
	for _, c := range cases {
		if got := AuditFlags(c.r); got != c.want {
			t.Errorf("AuditFlags(%+v) = %q, want %q", c.r, got, c.want)
		}
	}
}

func TestAuditStatsReportNamesEveryCounter(t *testing.T) {
	st := audit.Stats{
		Total: 3, ByDecision: map[string]int{"allow": 2, "deny": 1}, ByRisk: map[string]int{"exec": 2, "read": 1},
		Denied: 1, Guard: 1, Errors: 1, Skipped: 2,
		Sinks: []audit.SinkStat{{Name: "http", Sent: 10, Dropped: 1, Failures: 3, LastError: "audit sink http: HTTP 503"}},
	}
	out := FormatAuditStats(st)
	for _, want := range []string{"3 records", "2 malformed", "allow 2", "deny 1", "exec 2", "read 1", "denied 1", "guard 1", "errors 1", "http", "sent 10", "dropped 1", "failures 3", "HTTP 503"} {
		if !strings.Contains(out, want) {
			t.Fatalf("stats lack %q:\n%s", want, out)
		}
	}
}

func TestSinkWarningsAreDrainedOnce(t *testing.T) {
	f := newFixture(t)
	f.ws.noteSinkWarning("audit sink http: 3 failures, last: HTTP 503")
	if got := f.ws.SinkWarnings(); len(got) != 1 {
		t.Fatalf("warnings = %q", got)
	}
	if got := f.ws.SinkWarnings(); len(got) != 0 {
		t.Fatalf("warnings must be drained, got %q", got)
	}
}

func appendRaw(path, s string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(s)
	return err
}
