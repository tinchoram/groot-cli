package ui

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/tinchoram/groot-cli/internal/audit"
)

// auditFake holds the records and sink warnings the /audit tests read. It
// returns every record it holds; filters are the workspace's job.
type auditFake struct {
	recs    []audit.Record
	warns   []string
	queries []string // "space|decision|limit" per call
}

// Audit answers the fake's records of the named space, with stats.
func (f *fakeWorkspace) Audit(ctx context.Context, name string, q audit.Query) ([]audit.Record, audit.Stats, error) {
	recs, err := f.AuditRecords(ctx, name, q)
	return recs, auditStatsOf(recs), err
}

// AuditRecords answers the fake's records of the named space, and records the call.
func (f *fakeWorkspace) AuditRecords(_ context.Context, name string, q audit.Query) ([]audit.Record, error) {
	f.audit.queries = append(f.audit.queries, fmt.Sprintf("%s|%s|%d", name, q.Decision, q.Limit))
	if name != "" && name != "default" && name != "other" {
		return nil, fmt.Errorf("unknown space %q", name)
	}
	if name == "" {
		name = "default" // the active space of the fixture
	}
	var out []audit.Record
	for _, r := range f.audit.recs {
		if r.Space == name && q.Matches(r) {
			out = append(out, r)
		}
	}
	return out, nil
}

// AuditAll answers the fake's records from every space that match q.
func (f *fakeWorkspace) AuditAll(_ context.Context, q audit.Query) ([]audit.Record, audit.Stats, error) {
	f.audit.queries = append(f.audit.queries, fmt.Sprintf("all|%s|%d", q.Decision, q.Limit))
	var out []audit.Record
	for _, r := range f.audit.recs {
		if q.Matches(r) {
			out = append(out, r)
		}
	}
	return out, auditStatsOf(out), nil
}

// SinkWarnings returns the warnings the fake holds, once.
func (f *fakeWorkspace) SinkWarnings() []string {
	w := f.audit.warns
	f.audit.warns = nil
	return w
}

func auditStatsOf(recs []audit.Record) audit.Stats {
	st := audit.Stats{Total: len(recs), ByDecision: map[string]int{}, ByRisk: map[string]int{}}
	for _, r := range recs {
		st.ByDecision[r.Decision]++
		st.ByRisk[r.Risk]++
	}
	return st
}

func sampleAudit() []audit.Record {
	at := time.Date(2026, 10, 24, 10, 15, 30, 0, time.UTC)
	return []audit.Record{
		{Time: at, Space: "default", Agent: "developer", Tool: "bash", Risk: "exec", Decision: "deny", InputPreview: `{"command":"rm -rf build"}`, IsError: true, Sandbox: "none"},
		{Time: at.Add(time.Minute), Space: "default", Agent: "reader", Tool: "fetch_url", Risk: "network", Decision: "allow", Guard: []string{"override"}, DurationMS: 340},
		{Time: at.Add(2 * time.Minute), Space: "other", Agent: "developer", Tool: "write_file", Risk: "write", Decision: "allow"},
	}
}

func TestParseAuditFilters(t *testing.T) {
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name     string
		arg      string
		limit    int
		decision string
		tool     string
		agent    string
		find     string
		guard    bool
		errs     bool
		all      bool
		stats    bool
		sinceSet bool
		wantErr  string
	}{
		{name: "default", arg: "", limit: 20},
		{name: "count", arg: "7", limit: 7},
		{name: "denied since and tool", arg: "denied since 1d tool bash", limit: 20, decision: "deny", tool: "bash", sinceSet: true},
		{name: "guard and errors", arg: "guard errors", limit: 20, guard: true, errs: true},
		{name: "agent and quoted find", arg: `agent developer find "rm -rf"`, limit: 20, agent: "developer", find: "rm -rf"},
		{name: "all with stats", arg: "all stats since 2h", limit: 20, all: true, stats: true, sinceSet: true},
		{name: "explicit decision", arg: "decision allow", limit: 20, decision: "allow"},
		{name: "bad limit", arg: "0", wantErr: "limit"},
		{name: "bad since", arg: "since yesterday", wantErr: "not a time"},
		{name: "missing value", arg: "tool", wantErr: "tool needs"},
		{name: "unknown word", arg: "bogus", wantErr: "unknown"},
		{name: "bad decision", arg: "decision maybe", wantErr: "allow or deny"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req, err := parseAudit(c.arg, now)
			if c.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), c.wantErr) {
					t.Fatalf("error = %v, want %q", err, c.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			q := req.Query
			if q.Limit != c.limit || q.Decision != c.decision || q.Tool != c.tool || q.Agent != c.agent ||
				q.Find != c.find || q.GuardOnly != c.guard || q.ErrorsOnly != c.errs || req.All != c.all ||
				req.Stats != c.stats || q.Since.IsZero() == c.sinceSet {
				t.Fatalf("request = %+v (limit %d), want %+v", req, q.Limit, c)
			}
			if c.sinceSet && now.Sub(q.Since) < time.Hour {
				t.Fatalf("since = %v, want a time at least an hour back", q.Since)
			}
		})
	}
}

func TestAuditCommandRendersFlagsOfTheActiveSpace(t *testing.T) {
	m, ws := newTestModel(t, &fakeSession{model: "m"})
	ws.audit.recs = sampleAudit()
	m = sendLine(t, m, "/audit denied")
	out := stripANSI(m.transcriptText())
	clock := sampleAudit()[0].Time.Local().Format("15:04:05")
	for _, want := range []string{"TIME", "AGENT", "TOOL", "DECISION", "DUR", "FLAGS", "PREVIEW", "bash", "deny", "⛔ ✗ ∅", clock} {
		if !strings.Contains(out, want) {
			t.Fatalf("/audit lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "write_file") {
		t.Fatalf("/audit must follow its filter:\n%s", out)
	}
	if got := ws.audit.queries[len(ws.audit.queries)-1]; got != "|deny|20" {
		t.Fatalf("/audit queried %q, want the active space, deny, 20", got)
	}
}

func TestAuditAllAddsTheSpaceColumnAndGuardFlag(t *testing.T) {
	m, ws := newTestModel(t, &fakeSession{model: "m"})
	ws.audit.recs = sampleAudit()
	m = sendLine(t, m, "/audit all guard")
	out := stripANSI(m.transcriptText())
	if !strings.Contains(out, "SPACE") || !strings.Contains(out, "fetch_url") || !strings.Contains(out, "⚠") {
		t.Fatalf("/audit all lacks the space column or the guard mark:\n%s", out)
	}
	if got := ws.audit.queries[len(ws.audit.queries)-1]; !strings.HasPrefix(got, "all|") {
		t.Fatalf("/audit all queried %q", got)
	}
}

func TestAuditStatsPrintsTheBlock(t *testing.T) {
	m, ws := newTestModel(t, &fakeSession{model: "m"})
	ws.audit.recs = sampleAudit()
	m = sendLine(t, m, "/audit stats")
	out := stripANSI(m.transcriptText())
	for _, want := range []string{"2 records", "decisions: allow 1, deny 1", "risk:", "exec 1", "network 1"} {
		if !strings.Contains(out, want) {
			t.Fatalf("/audit stats lacks %q:\n%s", want, out)
		}
	}
	m = sendLine(t, m, "/audit all stats")
	if out := stripANSI(m.transcriptText()); !strings.Contains(out, "3 records") {
		t.Fatalf("/audit all stats must count every space:\n%s", out)
	}
}

func TestAuditRejectsBadFiltersWithoutQuerying(t *testing.T) {
	m, ws := newTestModel(t, &fakeSession{model: "m"})
	before := len(ws.audit.queries)
	m = sendLine(t, m, "/audit since nonsense")
	if out := stripANSI(m.transcriptText()); !strings.Contains(out, "not a time") {
		t.Fatalf("bad filter must explain itself:\n%s", out)
	}
	if len(ws.audit.queries) != before {
		t.Fatal("a bad filter must not query the log")
	}
}

func TestSinkWarningsShowOnceInTheTranscript(t *testing.T) {
	m, ws := newTestModel(t, &fakeSession{model: "m"})
	ws.audit.warns = []string{"audit sink http: 3 failures, last: HTTP 503"}
	m.refreshUnread()
	m.refreshUnread()
	out := stripANSI(m.transcriptText())
	if n := strings.Count(out, "audit sink http: 3 failures, last: HTTP 503"); n != 1 {
		t.Fatalf("sink warning shown %d times, want once:\n%s", n, out)
	}
}

func TestAuditIsAKnownCommandWithHelp(t *testing.T) {
	if !knownCommands["audit"] {
		t.Fatal("/audit must be a known command")
	}
	help := helpBlock()
	for _, want := range []string{"/audit [n]", "/audit stats", "/audit all"} {
		if !strings.Contains(help, want) {
			t.Fatalf("help lacks %q", want)
		}
	}
}
