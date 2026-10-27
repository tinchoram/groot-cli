package flow

import (
	"slices"
	"testing"

	"github.com/tinchoram/groot-cli/internal/catalog"
)

// observedTable is the phase each builtin tool says the task is in. Bash is
// not listed: its phase depends on whether the call is read-only, and it is
// checked for both values below.
var observedTable = map[string]Phase{
	"read_file":    PhasePlanned,
	"list_dir":     PhasePlanned,
	"glob":         PhasePlanned,
	"recall":       PhasePlanned,
	"memory_get":   PhasePlanned,
	"fetch_url":    PhasePlanned,
	"kubectl":      PhasePlanned,
	"read_inbox":   PhasePlanned,
	"task_status":  PhasePlanned,
	"write_file":   PhaseImplement,
	"task_verify":  PhaseVerify,
	"send_message": PhaseDeliver,
}

// silentTools say nothing about the phase: flow, memory, feedback, catalog and
// admin tools.
var silentTools = []string{
	"task_classify", "task_open", "task_advance", "remember", "forget", "update_memory",
	"feedback_report", "feedback_list", "feedback_update", "read_changelog", "current_time",
	"list_spaces", "create_space", "assign_agent", "unassign_agent", "list_agents", "create_agent",
	"import_agent", "list_library", "install_agent", "upgrade_agent", "set_work_mode",
	"list_models", "set_agent_model", "set_space_route", "set_space_model", "set_space_delegation",
	"set_space_budget", "set_agent_budget", "get_context", "get_cost", "list_tasks", "audit_query",
	"set_context_window", "grant_tool", "revoke_tool", "set_default_model", "grant_global_memory",
	"revoke_global_memory", "delete_space", "edit_space", "delete_agent", "edit_agent",
	"move_agent", "copy_agent",
}

// TestInferPhaseCoversEveryBuiltinTool fails when a builtin tool has no decision
// in the table, so a new tool must say which phase it observes.
func TestInferPhaseCoversEveryBuiltinTool(t *testing.T) {
	for _, name := range catalog.KnownTools {
		if name == "bash" {
			continue
		}
		_, observed := observedTable[name]
		silent := slices.Contains(silentTools, name)
		if observed == silent {
			t.Errorf("tool %q must be in exactly one of observedTable and silentTools", name)
		}
	}
	for name := range observedTable {
		if !slices.Contains(catalog.KnownTools, name) {
			t.Errorf("observedTable names %q, which is not a builtin tool", name)
		}
	}
	for _, name := range silentTools {
		if !slices.Contains(catalog.KnownTools, name) {
			t.Errorf("silentTools names %q, which is not a builtin tool", name)
		}
	}
}

func TestInferPhaseForEveryBuiltinTool(t *testing.T) {
	for name, want := range observedTable {
		for _, readOnly := range []bool{true, false} {
			got, ok := InferPhase(name, readOnly)
			if !ok || got != want {
				t.Errorf("InferPhase(%q, %v) = %q, %v; want %q, true", name, readOnly, got, ok, want)
			}
		}
	}
	for _, name := range silentTools {
		if got, ok := InferPhase(name, true); ok {
			t.Errorf("InferPhase(%q) = %q; a silent tool must not observe a phase", name, got)
		}
	}
}

func TestInferPhaseBashFollowsReadOnly(t *testing.T) {
	if got, ok := InferPhase("bash", true); !ok || got != PhasePlanned {
		t.Errorf("read-only bash = %q, %v; want planned", got, ok)
	}
	if got, ok := InferPhase("bash", false); !ok || got != PhaseImplement {
		t.Errorf("write bash = %q, %v; want implement", got, ok)
	}
}

func TestInferPhaseIgnoresMCPTools(t *testing.T) {
	for _, name := range []string{"github__search", "fs__read_file", "github__write_file"} {
		for _, readOnly := range []bool{true, false} {
			if got, ok := InferPhase(name, readOnly); ok {
				t.Errorf("InferPhase(%q, %v) = %q; MCP tools say nothing about the phase", name, readOnly, got)
			}
		}
	}
}

func TestObserveFollowsToolActivity(t *testing.T) {
	h := newRig(t, ModeStrict)
	mustOpen(h, SizeMedium)
	if _, ok := h.ctl.Observed(); ok {
		t.Fatal("nothing observed before any tool call")
	}

	h.ctl.Observe("read_file", true)
	if got, ok := h.ctl.Observed(); !ok || got != PhasePlanned {
		t.Fatalf("after read_file: %q, %v; want planned", got, ok)
	}
	h.ctl.Observe("write_file", false)
	if got, ok := h.ctl.Observed(); !ok || got != PhaseImplement {
		t.Fatalf("after write_file: %q, %v; want implement", got, ok)
	}
	h.ctl.Observe("task_verify", false)
	if got, ok := h.ctl.Observed(); !ok || got != PhaseVerify {
		t.Fatalf("after task_verify: %q, %v; want verify", got, ok)
	}
	h.ctl.Observe("remember", false)
	if got, ok := h.ctl.Observed(); !ok || got != PhaseVerify {
		t.Fatalf("a silent tool must keep the last phase, got %q, %v", got, ok)
	}
}

func TestObserveIsClearedByAdvanceAndFail(t *testing.T) {
	h := newRig(t, ModeStrict)
	mustOpen(h, SizeMedium)
	h.ctl.Observe("write_file", false)
	h.ctl.RecordBash(ctx, `{"command":"go test ./..."}`, "ok", false)
	mustAdvance(t, h, PhaseVerify, "")
	if _, ok := h.ctl.Observed(); ok {
		t.Fatal("advance must clear the observed phase")
	}

	h.ctl.Observe("read_file", true)
	if _, err := h.ctl.Fail(ctx, "scope changed"); err != nil {
		t.Fatal(err)
	}
	if _, ok := h.ctl.Observed(); ok {
		t.Fatal("fail must clear the observed phase")
	}
}

func TestObserveNeedsAnActiveTask(t *testing.T) {
	h := newRig(t, ModeStrict)
	h.ctl.Observe("write_file", false)
	if _, ok := h.ctl.Observed(); ok {
		t.Fatal("without an open task nothing is observed")
	}

	mustOpen(h, SizeMedium)
	if _, ok := h.ctl.Observed(); ok {
		t.Fatal("opening a task starts with nothing observed")
	}
	h.ctl.Observe("read_file", true)
	if _, err := h.ctl.Fail(ctx, "stop"); err != nil {
		t.Fatal(err)
	}
	h.ctl.Observe("write_file", false)
	if _, ok := h.ctl.Observed(); ok {
		t.Fatal("after the task closed nothing is observed")
	}
}
