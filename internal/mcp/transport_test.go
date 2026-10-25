package mcp

import (
	"context"
	"errors"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/tinchoram/groot-cli/internal/config"
)

func TestExportedTransportReplacesTheRealTransport(t *testing.T) {
	f := newFixture(t)
	base := f.dialer(t)
	var asked []string
	m := New(map[string]config.MCPServer{"grafana": grafanaServer()}, Options{
		Transport: func(ctx context.Context, server string) (sdk.Transport, error) {
			asked = append(asked, server)
			return base(ctx, server, resolved{})
		},
	})
	if _, err := m.Tools(t.Context(), "grafana"); err != nil {
		t.Fatal(err)
	}
	if len(asked) != 1 || asked[0] != "grafana" {
		t.Fatalf("Transport was asked for %v, want [grafana]", asked)
	}
}

func TestExportedTransportErrorIsReportedAndBackedOff(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1000, 0)}
	calls := 0
	m := New(map[string]config.MCPServer{"grafana": grafanaServer()}, Options{
		Now: clock.Now,
		Transport: func(context.Context, string) (sdk.Transport, error) {
			calls++
			return nil, errors.New("connection refused")
		},
	})
	if _, err := m.Tools(t.Context(), "grafana"); err == nil {
		t.Fatal("a failing transport must fail the listing")
	}
	st := m.Status()[0]
	if st.Connected || st.LastError == "" {
		t.Fatalf("status must report the failure, got %+v", st)
	}
	if _, err := m.Tools(t.Context(), "grafana"); err == nil || calls != 1 {
		t.Fatalf("a failed server is not re-dialed before the backoff: calls=%d err=%v", calls, err)
	}
}
