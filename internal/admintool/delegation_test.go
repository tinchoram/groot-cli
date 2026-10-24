package admintool

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/tinchoram/groot-cli/internal/tool"
)

// The shared fake satisfies the delegation method with a no-op.
func (f *fakeManager) SetSpaceDelegation(context.Context, string, string, string) error {
	return nil
}

// delegationManager records the delegation settings the tool saves.
type delegationManager struct {
	fakeManager
	saved map[string][2]string // space -> mode, manager
	err   error
}

func (m *delegationManager) SetSpaceDelegation(_ context.Context, space, mode, manager string) error {
	if m.err != nil {
		return m.err
	}
	if m.saved == nil {
		m.saved = map[string][2]string{}
	}
	m.saved[space] = [2]string{mode, manager}
	return nil
}

func TestSetSpaceDelegationSavesModeAndManager(t *testing.T) {
	m := &delegationManager{}
	tl := NewSetSpaceDelegation(m)
	if tl.Risk() != tool.RiskWrite {
		t.Fatalf("risk = %v, want write", tl.Risk())
	}
	out, isErr := tl.Execute(context.Background(), `{"space":"payments","mode":"hint","manager":"lead"}`)
	if isErr {
		t.Fatalf("unexpected error: %s", out)
	}
	if got := m.saved["payments"]; got != [2]string{"hint", "lead"} {
		t.Fatalf("saved = %v", m.saved)
	}
	if !strings.Contains(out, "payments") || !strings.Contains(out, "hint") {
		t.Fatalf("the result names what changed: %q", out)
	}
}

func TestSetSpaceDelegationOmittedFieldsAreUnchanged(t *testing.T) {
	m := &delegationManager{}
	out, isErr := NewSetSpaceDelegation(m).Execute(context.Background(), `{"space":"payments"}`)
	if isErr {
		t.Fatalf("an empty change is allowed, got %q", out)
	}
	if got := m.saved["payments"]; got != [2]string{"", ""} {
		t.Fatalf("omitted fields must be passed as unchanged, saved %v", got)
	}
}

func TestSetSpaceDelegationRejectsAnUnknownMode(t *testing.T) {
	m := &delegationManager{}
	out, isErr := NewSetSpaceDelegation(m).Execute(context.Background(), `{"space":"payments","mode":"sometimes"}`)
	if !isErr || !strings.Contains(out, "sometimes") {
		t.Fatalf("an unknown mode must be refused, got %q (%v)", out, isErr)
	}
	if len(m.saved) != 0 {
		t.Fatalf("nothing may be saved, saved %v", m.saved)
	}
}

func TestSetSpaceDelegationReportsAManagerThatIsNotAssigned(t *testing.T) {
	m := &delegationManager{err: errors.New(`agent "ghost" is not assigned to space "payments"`)}
	out, isErr := NewSetSpaceDelegation(m).Execute(context.Background(), `{"space":"payments","manager":"ghost"}`)
	if !isErr || !strings.Contains(out, "not assigned") {
		t.Fatalf("an unassigned manager must be reported, got %q (%v)", out, isErr)
	}
}
