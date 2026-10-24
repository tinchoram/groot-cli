package audit

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// line encodes r the way Log.Write does, without the redaction step.
func line(t *testing.T, r Record) string {
	t.Helper()
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

var base = time.Date(2026, 10, 24, 10, 0, 0, 0, time.UTC)

func TestReverseLinesAcrossChunks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rev.txt")
	var want []string
	var sb strings.Builder
	for i := 0; i < 300; i++ {
		s := fmt.Sprintf("line %03d %s", i, strings.Repeat("x", i%37))
		want = append(want, s)
		sb.WriteString(s + "\n")
	}
	// An empty line and a missing final newline must not break the walk.
	sb.WriteString("\n")
	sb.WriteString("last without newline")
	want = append(want, "last without newline")
	writeFile(t, path, sb.String())

	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	// A tiny chunk forces most lines to span several reads.
	rr := newReverseLines(f, 7)
	var got []string
	for {
		b, ok, err := rr.Next()
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			break
		}
		got = append(got, string(b))
	}
	if len(got) != len(want) {
		t.Fatalf("got %d lines, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[len(want)-1-i] {
			t.Fatalf("line %d = %q, want %q", i, got[i], want[len(want)-1-i])
		}
	}
}

func TestReverseLinesEmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.txt")
	writeFile(t, path, "")
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, ok, err := newReverseLines(f, 4).Next(); ok || err != nil {
		t.Fatalf("empty file: ok=%v err=%v", ok, err)
	}
}

func TestReadNewestFirstWithLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	var sb strings.Builder
	for i := 0; i < 1000; i++ {
		sb.WriteString(line(t, Record{Time: base.Add(time.Duration(i) * time.Second), Tool: "bash", Decision: "allow", Risk: "exec"}) + "\n")
	}
	writeFile(t, path, sb.String())

	got, err := Read(path, Query{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 10 {
		t.Fatalf("limit 10 returned %d records", len(got))
	}
	for i, r := range got {
		if want := base.Add(time.Duration(999-i) * time.Second); !r.Time.Equal(want) {
			t.Fatalf("record %d at %v, want %v (newest first)", i, r.Time, want)
		}
	}

	def, err := Read(path, Query{})
	if err != nil {
		t.Fatal(err)
	}
	if len(def) != 50 {
		t.Fatalf("default limit should be 50, got %d", len(def))
	}
}

func TestReadMissingFileIsEmpty(t *testing.T) {
	got, err := Read(filepath.Join(t.TempDir(), "none.jsonl"), Query{})
	if err != nil || len(got) != 0 {
		t.Fatalf("missing file: %v, %d records", err, len(got))
	}
}

func TestReadSkipsMalformedLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	good1 := line(t, Record{Time: base, Tool: "kubectl", Decision: "allow"})
	good2 := line(t, Record{Time: base.Add(time.Minute), Tool: "bash", Decision: "deny"})
	writeFile(t, path, good1+"\nnot json at all\n"+good2+"\n{\"ts\": \"truncated\n")

	got, err := Read(path, Query{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Tool != "bash" || got[1].Tool != "kubectl" {
		t.Fatalf("malformed lines must be skipped, got %+v", got)
	}

	st, err := Summarize(path, Query{})
	if err != nil {
		t.Fatal(err)
	}
	if st.Skipped != 2 || st.Total != 2 {
		t.Fatalf("Summarize skipped=%d total=%d, want 2 and 2", st.Skipped, st.Total)
	}
}

func TestReadFilters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	recs := []Record{
		{Time: base, Session: "s1", Agent: "developer", Tool: "bash", Risk: "exec", Decision: "allow", InputPreview: `{"command":"go test ./..."}`, Sandbox: "bwrap"},
		{Time: base.Add(time.Minute), Session: "s1", Agent: "developer", Tool: "bash", Risk: "exec", Decision: "deny", InputPreview: `{"command":"rm -rf build"}`, IsError: true},
		{Time: base.Add(2 * time.Minute), Session: "s2", Agent: "reader", Tool: "read_file", Risk: "read", Decision: "allow", InputPreview: `{"path":"README.md"}`, Guard: []string{"override"}, Sandbox: "none"},
		{Time: base.Add(3 * time.Minute), Session: "s2", Agent: "reader", Tool: "fetch_url", Risk: "network", Decision: "allow", InputPreview: `{"url":"https://EXAMPLE.test"}`, Guard: []string{"command"}, IsError: true},
	}
	var sb strings.Builder
	for _, r := range recs {
		sb.WriteString(line(t, r) + "\n")
	}
	writeFile(t, path, sb.String())

	cases := []struct {
		name string
		q    Query
		want []string // tools, newest first
	}{
		{"tool", Query{Tool: "bash"}, []string{"bash", "bash"}},
		{"decision deny", Query{Decision: "deny"}, []string{"bash"}},
		{"risk", Query{Risk: "read"}, []string{"read_file"}},
		{"agent", Query{Agent: "reader"}, []string{"fetch_url", "read_file"}},
		{"session", Query{Session: "s1"}, []string{"bash", "bash"}},
		{"errors", Query{ErrorsOnly: true}, []string{"fetch_url", "bash"}},
		{"guard", Query{GuardOnly: true}, []string{"fetch_url", "read_file"}},
		{"sandbox none", Query{SandboxNone: true}, []string{"read_file"}},
		{"find is case-insensitive", Query{Find: "example.TEST"}, []string{"fetch_url"}},
		{"find in bash preview", Query{Find: "rm -rf"}, []string{"bash"}},
		{"since", Query{Since: base.Add(90 * time.Second)}, []string{"fetch_url", "read_file"}},
		{"until", Query{Until: base.Add(time.Minute)}, []string{"bash", "bash"}},
		{"combined", Query{Tool: "bash", Decision: "deny", ErrorsOnly: true}, []string{"bash"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := Read(path, c.q)
			if err != nil {
				t.Fatal(err)
			}
			var tools []string
			for _, r := range got {
				tools = append(tools, r.Tool)
			}
			if fmt.Sprint(tools) != fmt.Sprint(c.want) {
				t.Fatalf("tools = %v, want %v", tools, c.want)
			}
		})
	}
}

func TestSummarizeCounts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	recs := []Record{
		{Tool: "bash", Risk: "exec", Decision: "allow"},
		{Tool: "bash", Risk: "exec", Decision: "deny", IsError: true},
		{Tool: "read_file", Risk: "read", Decision: "allow", Guard: []string{"override"}},
		{Tool: "write_file", Risk: "write", Decision: "allow", IsError: true},
	}
	var sb strings.Builder
	for _, r := range recs {
		sb.WriteString(line(t, r) + "\n")
	}
	writeFile(t, path, sb.String()+"garbage\n")

	st, err := Summarize(path, Query{})
	if err != nil {
		t.Fatal(err)
	}
	if st.Total != 4 || st.Denied != 1 || st.Guard != 1 || st.Errors != 2 || st.Skipped != 1 {
		t.Fatalf("stats = %+v", st)
	}
	if st.ByDecision["allow"] != 3 || st.ByDecision["deny"] != 1 {
		t.Fatalf("by decision = %v", st.ByDecision)
	}
	if st.ByRisk["exec"] != 2 || st.ByRisk["read"] != 1 || st.ByRisk["write"] != 1 {
		t.Fatalf("by risk = %v", st.ByRisk)
	}
}

func TestParseWhen(t *testing.T) {
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	cases := map[string]time.Time{
		"2h":                   now.Add(-2 * time.Hour),
		"30m":                  now.Add(-30 * time.Minute),
		"1d":                   now.Add(-24 * time.Hour),
		"2026-10-23T08:00:00Z": time.Date(2026, 10, 23, 8, 0, 0, 0, time.UTC),
	}
	for in, want := range cases {
		got, err := ParseWhen(in, now)
		if err != nil || !got.Equal(want) {
			t.Errorf("ParseWhen(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "yesterday", "2x", "-1d"} {
		if _, err := ParseWhen(bad, now); err == nil {
			t.Errorf("ParseWhen(%q) should fail", bad)
		}
	}
}
