package flow

import (
	"strings"
	"testing"
)

// emptyRouteRig is a controller whose routes name no model: the agent keeps its own.
func emptyRouteRig(t *testing.T, routes Routes) *rig {
	t.Helper()
	h := &rig{store: &fakeStore{}, clk: &clock{now: t0}, planOK: true, docs: map[string]string{}}
	ctl, err := NewController(ctx, Options{
		Space: "payments", Agent: "reviewer", Store: h.store, Mode: ModeStrict,
		Routes:     routes,
		Now:        h.clk.tick,
		PlanExists: func(string) bool { return true },
		ApplyRoute: func(r Route) func() {
			h.applied = append(h.applied, r)
			return func() { h.restored++ }
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	h.ctl = ctl
	return h
}

func TestEmptyRouteNeverSwitchesTheModel(t *testing.T) {
	h := emptyRouteRig(t, Routes{})
	h.open(t, SizeMedium)
	if len(h.applied) != 0 {
		t.Fatalf("an empty medium route must not apply a model, applied = %+v", h.applied)
	}
	h.ctl.Fail(ctx, "stop")

	h = emptyRouteRig(t, Routes{})
	h.open(t, SizeLarge)
	if len(h.applied) != 0 {
		t.Fatalf("an empty large route must not apply a model, applied = %+v", h.applied)
	}
	h.ctl.Fail(ctx, "stop")

	// Small opens no task at all, so no route is ever applied to it.
	h = emptyRouteRig(t, Routes{})
	if _, err := h.ctl.Classify(SizeSmall, true, "one line"); err != nil {
		t.Fatal(err)
	}
	if len(h.applied) != 0 {
		t.Fatalf("small work must not apply a model, applied = %+v", h.applied)
	}
}

func TestExplicitRouteModelStillApplies(t *testing.T) {
	h := emptyRouteRig(t, Routes{Medium: Route{Model: "opt-in-model"}})
	h.open(t, SizeMedium)
	if len(h.applied) != 1 || h.applied[0].Model != "opt-in-model" {
		t.Fatalf("an explicit route model must still apply, applied = %+v", h.applied)
	}
}

func TestClassifyNamesTheAgentModelWhenRouteIsEmpty(t *testing.T) {
	h := emptyRouteRig(t, Routes{})
	text, err := h.ctl.Classify(SizeMedium, true, "several files")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "route: agent model") {
		t.Fatalf("classification must say the agent model is kept: %s", text)
	}
}
