package admintool

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/tinchoram/groot-cli/internal/tool"
)

// The shared fake satisfies the new Manager methods with no-ops.
func (f *fakeManager) SetSpaceRoute(context.Context, string, string, string, string) (string, error) {
	return "", nil
}

func (f *fakeManager) SetSpaceModel(context.Context, string, string) (string, error) {
	return "", nil
}

// spaceManager records the space overrides the tools save.
type spaceManager struct {
	fakeManager
	routes map[string][2]string // "space/size" -> model, agent
	models map[string]string    // space -> model
	warn   string
	err    error
}

func (m *spaceManager) SetSpaceRoute(_ context.Context, space, size, model, agent string) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	if m.routes == nil {
		m.routes = map[string][2]string{}
	}
	m.routes[space+"/"+size] = [2]string{model, agent}
	return m.warn, nil
}

func (m *spaceManager) SetSpaceModel(_ context.Context, space, model string) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	if m.models == nil {
		m.models = map[string]string{}
	}
	m.models[space] = model
	return m.warn, nil
}

func TestSetSpaceRouteSavesAndReports(t *testing.T) {
	m := &spaceManager{warn: "model not checked against the provider list: offline"}
	tl := NewSetSpaceRoute(m)
	out, isErr := tl.Execute(context.Background(), `{"space":"payments","size":"medium","model":"claude-haiku-5-5","agent":"reviewer"}`)
	if isErr {
		t.Fatalf("unexpected error: %s", out)
	}
	if got := m.routes["payments/medium"]; got != [2]string{"claude-haiku-5-5", "reviewer"} {
		t.Fatalf("saved = %v", m.routes)
	}
	for _, want := range []string{"payments", "medium", "claude-haiku-5-5", "reviewer", "offline"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output lacks %q: %s", want, out)
		}
	}
	if tl.Risk() != tool.RiskWrite || !strings.Contains(tl.Preview(`{"space":"payments","size":"medium"}`), "payments") {
		t.Fatal("set_space_route is a write whose preview names the space")
	}
}

func TestSetSpaceRouteRejectsABadSizeBeforeSaving(t *testing.T) {
	m := &spaceManager{}
	out, isErr := NewSetSpaceRoute(m).Execute(context.Background(), `{"space":"payments","size":"huge","model":"x"}`)
	if !isErr || !strings.Contains(out, "small") {
		t.Fatalf("a bad size must be refused and name the sizes: %q (err=%v)", out, isErr)
	}
	if len(m.routes) != 0 {
		t.Fatalf("nothing may be saved: %v", m.routes)
	}
}

func TestSetSpaceRouteReportsAModelRefusal(t *testing.T) {
	m := &spaceManager{err: errors.New(`unknown model "nope"`)}
	out, isErr := NewSetSpaceRoute(m).Execute(context.Background(), `{"space":"payments","size":"small","model":"nope"}`)
	if !isErr || !strings.Contains(out, "unknown model") {
		t.Fatalf("a refused model must be reported: %q (err=%v)", out, isErr)
	}
}

func TestSetSpaceRouteWithoutModelOrAgentClearsTheRoute(t *testing.T) {
	m := &spaceManager{}
	out, isErr := NewSetSpaceRoute(m).Execute(context.Background(), `{"space":"payments","size":"large"}`)
	if isErr {
		t.Fatalf("unexpected error: %s", out)
	}
	if got, ok := m.routes["payments/large"]; !ok || got != [2]string{"", ""} {
		t.Fatalf("clear must save empty values: %v", m.routes)
	}
	if !strings.Contains(out, "cleared") {
		t.Fatalf("output must say the route was cleared: %s", out)
	}
}

func TestSetSpaceRouteNeedsSpaceAndSize(t *testing.T) {
	for _, in := range []string{`{"size":"small"}`, `{"space":"payments"}`} {
		if out, isErr := NewSetSpaceRoute(&spaceManager{}).Execute(context.Background(), in); !isErr {
			t.Fatalf("%s must be refused: %q", in, out)
		}
	}
	if out, isErr := NewSetSpaceRoute(&spaceManager{}).Execute(context.Background(), `{"space":"p","size":"small","extra":1}`); !isErr {
		t.Fatalf("unknown fields must be refused: %q", out)
	}
}

func TestSetSpaceModelSetsClearsAndReportsRefusals(t *testing.T) {
	m := &spaceManager{}
	tl := NewSetSpaceModel(m)
	if out, isErr := tl.Execute(context.Background(), `{"space":"payments","model":"claude-opus-5-5"}`); isErr {
		t.Fatalf("unexpected error: %s", out)
	}
	if m.models["payments"] != "claude-opus-5-5" {
		t.Fatalf("saved = %v", m.models)
	}
	out, isErr := tl.Execute(context.Background(), `{"space":"payments"}`)
	if isErr || !strings.Contains(out, "cleared") || m.models["payments"] != "" {
		t.Fatalf("clear failed: %q (err=%v) %v", out, isErr, m.models)
	}
	m.err = errors.New(`unknown model "nope"`)
	if out, isErr := tl.Execute(context.Background(), `{"space":"payments","model":"nope"}`); !isErr || !strings.Contains(out, "unknown model") {
		t.Fatalf("refusal must be reported: %q (err=%v)", out, isErr)
	}
	if tl.Risk() != tool.RiskWrite {
		t.Fatal("set_space_model is a write")
	}
}
