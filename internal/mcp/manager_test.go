package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/tinchoram/groot-cli/internal/config"
)

var fixtureTools = []string{"echo", "chart", "fail", "slow", "search_dashboards"}

func grafanaServer() config.MCPServer {
	return config.MCPServer{
		Transport: "http",
		URL:       "https://grafana.internal/mcp",
		Tools:     fixtureTools,
		ReadOnly:  []string{"echo", "chart"},
		Timeout:   5 * time.Second,
	}
}

// fakeClock lets the tests move the backoff clock without sleeping.
type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time { return c.now }

// newTestManager builds a manager for one server "grafana" whose sessions go to
// the fixture. Dials are counted through the returned pointer.
func newTestManager(t *testing.T, s config.MCPServer, clock *fakeClock) (*Manager, *fixture, *int) {
	t.Helper()
	f := newFixture(t)
	dials := 0
	base := f.dialer(t)
	opts := Options{
		Now: clock.Now,
		dial: func(ctx context.Context, name string, r resolved) (sdk.Transport, error) {
			dials++
			return base(ctx, name, r)
		},
	}
	return New(map[string]config.MCPServer{"grafana": s}, opts), f, &dials
}

func TestConnectIsLazyAndIdempotent(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1000, 0)}
	m, _, dials := newTestManager(t, grafanaServer(), clock)

	if *dials != 0 {
		t.Fatalf("New must not connect, dialed %d times", *dials)
	}
	if st := m.Status()[0]; st.Connected {
		t.Fatal("no session before the first use")
	}
	if _, err := m.Tools(t.Context(), "grafana"); err != nil {
		t.Fatal(err)
	}
	if err := m.Connect(t.Context(), "grafana"); err != nil {
		t.Fatal(err)
	}
	if *dials != 1 {
		t.Fatalf("a connected server must be reused, dialed %d times", *dials)
	}
	if st := m.Status()[0]; !st.Connected {
		t.Fatal("status must report the live session")
	}
}

func TestToolsFiltersByAllowlistAndReportsMissing(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1000, 0)}
	m, _, _ := newTestManager(t, grafanaServer(), clock)

	tools, err := m.Tools(t.Context(), "grafana")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, r := range tools {
		names = append(names, r.Name)
	}
	sort.Strings(names)
	want := []string{"chart", "echo", "fail", "slow"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("drop_table is offered but not allowlisted; want %v, got %v", want, names)
	}
	if got := m.Missing("grafana"); !reflect.DeepEqual(got, []string{"search_dashboards"}) {
		t.Fatalf("allowlisted but not offered tools must be reported, got %v", got)
	}
	for _, r := range tools {
		if r.Server != "grafana" {
			t.Fatalf("remote must name its server, got %q", r.Server)
		}
		if r.ReadOnly != (r.Name == "echo" || r.Name == "chart") {
			t.Fatalf("read_only flag wrong for %s: %v", r.Name, r.ReadOnly)
		}
	}
	st := m.Status()[0]
	if st.ToolCount != 4 || !reflect.DeepEqual(st.Missing, []string{"search_dashboards"}) {
		t.Fatalf("status must carry the count and the missing tools, got %+v", st)
	}
}

func TestCallReturnsTextContent(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1000, 0)}
	m, _, _ := newTestManager(t, grafanaServer(), clock)

	res, err := m.Call(t.Context(), "grafana", "echo", json.RawMessage(`{"text":"hi"}`))
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "echo: hi" || res.IsError || len(res.Omitted) != 0 {
		t.Fatalf("got %+v", res)
	}
}

func TestCallOmitsNonTextBlocksAndSaysSo(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1000, 0)}
	m, _, _ := newTestManager(t, grafanaServer(), clock)

	res, err := m.Call(t.Context(), "grafana", "chart", nil)
	if err != nil {
		t.Fatal(err)
	}
	wantOmitted := []string{"image/png", "resource: grafana://panel/1"}
	if !reflect.DeepEqual(res.Omitted, wantOmitted) {
		t.Fatalf("omitted = %v, want %v", res.Omitted, wantOmitted)
	}
	wantText := "rendered\n[non-text content omitted: image/png]\n[non-text content omitted: resource: grafana://panel/1]"
	if res.Text != wantText {
		t.Fatalf("text = %q, want %q", res.Text, wantText)
	}
}

func TestCallReportsAToolErrorWithoutAGoError(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1000, 0)}
	m, _, _ := newTestManager(t, grafanaServer(), clock)

	res, err := m.Call(t.Context(), "grafana", "fail", nil)
	if err != nil {
		t.Fatalf("a tool-level error is a result, not a transport error: %v", err)
	}
	if !res.IsError || res.Text != "query failed: bad metric" {
		t.Fatalf("got %+v", res)
	}
}

func TestCallTimesOutAtTheServerTimeout(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1000, 0)}
	s := grafanaServer()
	s.Timeout = 100 * time.Millisecond
	m, _, _ := newTestManager(t, s, clock)

	start := time.Now()
	_, err := m.Call(t.Context(), "grafana", "slow", nil)
	if err == nil || !strings.Contains(err.Error(), "timed out after 100ms") {
		t.Fatalf("want a timeout error, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("the call must return at the timeout, took %v", elapsed)
	}
}

func TestCallRefusesAToolOutsideTheAllowlistBeforeAnyNetworkCall(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1000, 0)}
	m, f, dials := newTestManager(t, grafanaServer(), clock)

	_, err := m.Call(t.Context(), "grafana", "drop_table", nil)
	if err == nil || err.Error() != "tool drop_table is not in the allowlist of server grafana" {
		t.Fatalf("got %v", err)
	}
	if *dials != 0 || f.callCount("drop_table") != 0 {
		t.Fatalf("a refused call must not connect or reach the server (dials=%d, calls=%d)", *dials, f.callCount("drop_table"))
	}
}

func TestCallUnknownServerIsAnError(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1000, 0)}
	m, _, _ := newTestManager(t, grafanaServer(), clock)

	if _, err := m.Call(t.Context(), "nope", "echo", nil); err == nil {
		t.Fatal("an unknown server must fail")
	}
}

func TestUnsetVariableFailsTheConnectBeforeDialing(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1000, 0)}
	s := grafanaServer()
	s.Headers = map[string]string{"Authorization": "Bearer ${GROOT_MCP_TEST_UNSET}"}
	m, _, dials := newTestManager(t, s, clock)
	t.Setenv("GROOT_MCP_TEST_UNSET", "")

	err := m.Connect(t.Context(), "grafana")
	if err == nil || err.Error() != "mcp server grafana: ${GROOT_MCP_TEST_UNSET} is not set" {
		t.Fatalf("got %v", err)
	}
	if *dials != 0 {
		t.Fatalf("an empty secret must never reach a dial, dialed %d times", *dials)
	}
}

func TestFailedConnectBacksOffForTenSeconds(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1000, 0)}
	f := newFixture(t)
	dials := 0
	healthy := f.dialer(t)
	opts := Options{
		Now: clock.Now,
		dial: func(ctx context.Context, name string, r resolved) (sdk.Transport, error) {
			dials++
			if dials == 1 {
				return nil, errors.New("boom")
			}
			return healthy(ctx, name, r)
		},
	}
	m := New(map[string]config.MCPServer{"grafana": grafanaServer()}, opts)

	if err := m.Connect(t.Context(), "grafana"); err == nil {
		t.Fatal("the first connect must fail")
	}
	clock.now = clock.now.Add(5 * time.Second)
	if err := m.Connect(t.Context(), "grafana"); err == nil {
		t.Fatal("inside the backoff the connect must fail without dialing")
	}
	if dials != 1 {
		t.Fatalf("inside the backoff there must be no dial, dialed %d times", dials)
	}
	if st := m.Status()[0]; !strings.Contains(st.LastError, "boom") {
		t.Fatalf("status must keep the last error, got %q", st.LastError)
	}

	clock.now = clock.now.Add(5 * time.Second)
	if err := m.Connect(t.Context(), "grafana"); err != nil {
		t.Fatalf("after the backoff the connect must retry: %v", err)
	}
	if dials != 2 {
		t.Fatalf("want exactly one retry, dialed %d times", dials)
	}
	if st := m.Status()[0]; !st.Connected || st.LastError != "" {
		t.Fatalf("a successful retry clears the error, got %+v", st)
	}
}

func TestCloseReleasesEverySession(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1000, 0)}
	m, _, _ := newTestManager(t, grafanaServer(), clock)

	if _, err := m.Tools(t.Context(), "grafana"); err != nil {
		t.Fatal(err)
	}
	if err := m.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if st := m.Status()[0]; st.Connected {
		t.Fatal("a closed manager must hold no live session")
	}
}

func TestStatusListsServersInNameOrder(t *testing.T) {
	m := New(map[string]config.MCPServer{
		"zeta":  {Transport: "stdio", Command: "z", Tools: []string{"a"}},
		"alpha": {Transport: "stdio", Command: "a", Tools: []string{"a"}},
	}, Options{})
	st := m.Status()
	if len(st) != 2 || st[0].Name != "alpha" || st[1].Name != "zeta" {
		t.Fatalf("status must be ordered by name, got %+v", st)
	}
	if st[0].Transport != "stdio" {
		t.Fatalf("transport must be reported, got %q", st[0].Transport)
	}
}
