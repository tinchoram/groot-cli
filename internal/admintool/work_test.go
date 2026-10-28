package admintool

import (
	"context"
	"strings"
	"testing"
)

func TestSetWorkModeSpaceWideAppliesToTheSpace(t *testing.T) {
	m := &fakeManager{}
	out, isErr := NewSetWorkMode(m).Execute(context.Background(), `{"space":"default","on":true,"space_wide":true}`)
	if isErr || !strings.Contains(out, "every agent") {
		t.Fatalf("result = %q (error %v)", out, isErr)
	}
	if len(m.workCalls) != 1 || m.workCalls[0] != "space:default:true" {
		t.Fatalf("calls = %v, want the space-wide call only", m.workCalls)
	}
}

func TestSetWorkModeKeepsThePerAgentCall(t *testing.T) {
	m := &fakeManager{}
	if _, isErr := NewSetWorkMode(m).Execute(context.Background(), `{"space":"default","agent":"reader","on":true}`); isErr {
		t.Fatal("a per-agent call is valid")
	}
	if len(m.workCalls) != 1 || m.workCalls[0] != "agent:default:reader:true" {
		t.Fatalf("calls = %v, want the per-agent call", m.workCalls)
	}
}

func TestSetWorkModeWithoutSpaceWideNeedsAnAgent(t *testing.T) {
	m := &fakeManager{}
	if _, isErr := NewSetWorkMode(m).Execute(context.Background(), `{"space":"default","on":true}`); !isErr {
		t.Fatal("a per-agent call needs the agent")
	}
	if len(m.workCalls) != 0 {
		t.Fatalf("calls = %v, want none", m.workCalls)
	}
}
