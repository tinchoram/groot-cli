package admintool

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/tinchoram/groot-cli/internal/tool"
)

func (f *fakeManager) DeleteSpace(name string, confirm, purge bool) (string, error) {
	f.edits = append(f.edits, fmt.Sprintf("delete_space:%s:%t:%t", name, confirm, purge))
	return f.editReport, f.editErr
}

func (f *fakeManager) EditSpace(name, newName, path string) (string, error) {
	f.edits = append(f.edits, "edit_space:"+name+":"+newName+":"+path)
	return f.editReport, f.editErr
}

func (f *fakeManager) DeleteAgent(name string, confirm bool) (string, error) {
	f.edits = append(f.edits, fmt.Sprintf("delete_agent:%s:%t", name, confirm))
	return f.editReport, f.editErr
}

func (f *fakeManager) EditAgent(e AgentEdit) (string, error) {
	desc, prompt := "-", "-"
	if e.Description != nil {
		desc = *e.Description
	}
	if e.Prompt != nil {
		prompt = *e.Prompt
	}
	f.edits = append(f.edits, "edit_agent:"+e.Name+":"+e.NewName+":"+desc+":"+prompt)
	return f.editReport, f.editErr
}

func (f *fakeManager) MoveAgent(agent, from, to string, withMemory bool) (string, error) {
	f.edits = append(f.edits, fmt.Sprintf("move_agent:%s:%s:%s:%t", agent, from, to, withMemory))
	return f.editReport, f.editErr
}

func (f *fakeManager) CopyAgent(agent, newName, space string) (string, error) {
	f.edits = append(f.edits, "copy_agent:"+agent+":"+newName+":"+space)
	return f.editReport, f.editErr
}

func newEditFake() *fakeManager {
	return &fakeManager{editReport: "report from the workspace"}
}

func TestEditToolsAreRegisteredAsWrites(t *testing.T) {
	names := map[string]bool{}
	for _, tl := range All(newEditFake(), "/home/sre") {
		names[tl.Definition().Name] = true
	}
	for _, want := range []string{"delete_space", "edit_space", "delete_agent", "edit_agent", "move_agent", "copy_agent"} {
		if !names[want] {
			t.Fatalf("%s is not an admin tool", want)
		}
	}
}

func TestEditToolsRequireTheirNamesAndRejectUnknownFields(t *testing.T) {
	cases := []struct {
		name  string
		tl    tool.Tool
		input string
	}{
		{"delete_space", NewDeleteSpace(newEditFake()), `{}`},
		{"edit_space", NewEditSpace(newEditFake(), "/home/sre"), `{"name":"x"}`},
		{"delete_agent", NewDeleteAgent(newEditFake()), `{}`},
		{"edit_agent", NewEditAgent(newEditFake()), `{"name":"x"}`},
		{"move_agent", NewMoveAgent(newEditFake()), `{"agent":"x","from":"a"}`},
		{"copy_agent", NewCopyAgent(newEditFake()), `{"agent":"x"}`},
	}
	for _, c := range cases {
		if out, isErr := c.tl.Execute(context.Background(), c.input); !isErr {
			t.Fatalf("%s accepted %s: %q", c.name, c.input, out)
		}
	}
	bad := NewDeleteSpace(newEditFake())
	if out, isErr := bad.Execute(context.Background(), `{"name":"x","force":true}`); !isErr || !strings.Contains(out, "invalid input") {
		t.Fatalf("unknown field must be refused: %q (%v)", out, isErr)
	}
}

func TestDeleteSpacePreviewSaysWhetherTheCallReportsOrDeletes(t *testing.T) {
	tl := NewDeleteSpace(newEditFake())
	if tl.Risk() != tool.RiskWrite {
		t.Fatalf("risk = %v, want write", tl.Risk())
	}
	report := tl.Preview(`{"name":"payments"}`)
	if !strings.Contains(report, "report") || !strings.Contains(report, "nothing is changed") || strings.Contains(report, "from spaces.yaml") {
		t.Fatalf("report preview = %q", report)
	}
	del := tl.Preview(`{"name":"payments","confirm":true}`)
	if !strings.Contains(del, "delete space payments from spaces.yaml") || !strings.Contains(del, "kept") {
		t.Fatalf("delete preview = %q", del)
	}
	purge := tl.Preview(`{"name":"payments","confirm":true,"purge_data":true}`)
	if !strings.Contains(purge, "purge its .groot data") {
		t.Fatalf("purge preview = %q", purge)
	}
}

func TestDeleteSpaceForwardsConfirmAndPurgeOnlyWhenConfirmed(t *testing.T) {
	m := newEditFake()
	tl := NewDeleteSpace(m)
	calls := []string{
		`{"name":"payments"}`,
		`{"name":"payments","confirm":true}`,
		`{"name":"payments","confirm":true,"purge_data":true}`,
		`{"name":"payments","purge_data":true}`,
	}
	for _, in := range calls {
		out, isErr := tl.Execute(context.Background(), in)
		if isErr || out != "report from the workspace" {
			t.Fatalf("call %s = %q (%v)", in, out, isErr)
		}
	}
	want := []string{
		"delete_space:payments:false:false",
		"delete_space:payments:true:false",
		"delete_space:payments:true:true",
		"delete_space:payments:false:false",
	}
	if !slices.Equal(m.edits, want) {
		t.Fatalf("calls = %v, want %v", m.edits, want)
	}
	if !strings.Contains(string(tl.Definition().InputSchema), `"purge_data"`) {
		t.Fatal("the schema must declare purge_data")
	}
}

func TestDeleteSpaceReportsManagerErrors(t *testing.T) {
	m := newEditFake()
	m.editErr = errors.New(`space "root" is the home directory and cannot be deleted`)
	out, isErr := NewDeleteSpace(m).Execute(context.Background(), `{"name":"root"}`)
	if !isErr || !strings.Contains(out, "cannot be deleted") {
		t.Fatalf("manager error = %q (%v)", out, isErr)
	}
}

func TestEditSpaceNeedsAChangeAndExpandsHome(t *testing.T) {
	m := newEditFake()
	tl := NewEditSpace(m, "/home/sre")
	if out, isErr := tl.Execute(context.Background(), `{"name":"payments"}`); !isErr || !strings.Contains(out, "new_name or path") {
		t.Fatalf("no change = %q (%v)", out, isErr)
	}
	if out, isErr := tl.Execute(context.Background(), `{"name":"payments","path":"relative/dir"}`); !isErr {
		t.Fatalf("a relative path must be refused: %q", out)
	}
	in := `{"name":"payments","new_name":"billing","path":"~/src/bill"}`
	if got := tl.Preview(in); !strings.Contains(got, "rename space payments to billing") || !strings.Contains(got, "~/src/bill") {
		t.Fatalf("preview = %q", got)
	}
	if _, isErr := tl.Execute(context.Background(), in); isErr {
		t.Fatal("valid edit refused")
	}
	if len(m.edits) != 1 || m.edits[0] != "edit_space:payments:billing:/home/sre/src/bill" {
		t.Fatalf("calls = %v", m.edits)
	}
	if tl.Risk() != tool.RiskWrite {
		t.Fatal("edit_space must be a write")
	}
}

func TestDeleteAgentNeedsNameAndPreviews(t *testing.T) {
	m := newEditFake()
	tl := NewDeleteAgent(m)
	if tl.Risk() != tool.RiskWrite {
		t.Fatal("delete_agent must be a write")
	}
	if got := tl.Preview(`{"name":"reviewer"}`); !strings.Contains(got, "report") || !strings.Contains(got, "nothing is changed") {
		t.Fatalf("report preview = %q", got)
	}
	if got := tl.Preview(`{"name":"reviewer","confirm":true}`); !strings.Contains(got, "delete agent reviewer") {
		t.Fatalf("delete preview = %q", got)
	}
	if out, isErr := tl.Execute(context.Background(), `{"name":" "}`); !isErr || out != "name is required" {
		t.Fatalf("blank name = %q (%v)", out, isErr)
	}
	if out, isErr := tl.Execute(context.Background(), `{"name":"reviewer"}`); isErr || out != "report from the workspace" {
		t.Fatalf("execute = %q (%v)", out, isErr)
	}
	if _, isErr := tl.Execute(context.Background(), `{"name":"reviewer","confirm":true}`); isErr {
		t.Fatal("a confirmed delete refused")
	}
	if !slices.Equal(m.edits, []string{"delete_agent:reviewer:false", "delete_agent:reviewer:true"}) {
		t.Fatalf("calls = %v", m.edits)
	}
}

func TestEditAgentNeedsAChangeAndPassesFields(t *testing.T) {
	m := newEditFake()
	tl := NewEditAgent(m)
	if tl.Risk() != tool.RiskWrite {
		t.Fatal("edit_agent must be a write")
	}
	if out, isErr := tl.Execute(context.Background(), `{"name":"reviewer"}`); !isErr || !strings.Contains(out, "nothing to change") {
		t.Fatalf("no change = %q (%v)", out, isErr)
	}
	in := `{"name":"reviewer","new_name":"critic","description":"reviews diffs","prompt":"You review."}`
	if got := tl.Preview(in); !strings.Contains(got, "reviewer") || !strings.Contains(got, "critic") || !strings.Contains(got, "reviews diffs") {
		t.Fatalf("preview = %q", got)
	}
	if _, isErr := tl.Execute(context.Background(), in); isErr {
		t.Fatal("valid edit refused")
	}
	if _, isErr := tl.Execute(context.Background(), `{"name":"reviewer","description":""}`); isErr {
		t.Fatal("an empty description is a change and must be accepted")
	}
	if len(m.edits) != 2 || m.edits[0] != "edit_agent:reviewer:critic:reviews diffs:You review." || m.edits[1] != "edit_agent:reviewer:::-" {
		t.Fatalf("calls = %v", m.edits)
	}
}

func TestMoveAgentPreviewAndCall(t *testing.T) {
	m := newEditFake()
	tl := NewMoveAgent(m)
	if tl.Risk() != tool.RiskWrite {
		t.Fatal("move_agent must be a write")
	}
	in := `{"agent":"reviewer","from":"payments","to":"billing"}`
	if got := tl.Preview(in); got != "move agent reviewer from space payments to space billing" {
		t.Fatalf("preview = %q", got)
	}
	if _, isErr := tl.Execute(context.Background(), in); isErr {
		t.Fatal("valid move refused")
	}
	if len(m.edits) != 1 || m.edits[0] != "move_agent:reviewer:payments:billing:false" {
		t.Fatalf("calls = %v", m.edits)
	}
}

func TestMoveAgentWithMemoryCopiesAndSaysSo(t *testing.T) {
	m := newEditFake()
	tl := NewMoveAgent(m)
	in := `{"agent":"reviewer","from":"payments","to":"billing","with_memory":true}`
	got := tl.Preview(in)
	if !strings.Contains(got, "copy") || !strings.Contains(got, "keeps its copies") {
		t.Fatalf("preview = %q", got)
	}
	if _, isErr := tl.Execute(context.Background(), in); isErr {
		t.Fatal("valid move refused")
	}
	if len(m.edits) != 1 || m.edits[0] != "move_agent:reviewer:payments:billing:true" {
		t.Fatalf("calls = %v", m.edits)
	}
	if !strings.Contains(string(tl.Definition().InputSchema), `"with_memory"`) {
		t.Fatal("the schema must declare with_memory")
	}
}

func TestCopyAgentPreviewAndCall(t *testing.T) {
	m := newEditFake()
	tl := NewCopyAgent(m)
	if tl.Risk() != tool.RiskWrite {
		t.Fatal("copy_agent must be a write")
	}
	in := `{"agent":"reviewer","new_name":"reviewer-2","space":"billing"}`
	if got := tl.Preview(in); !strings.Contains(got, "copy agent reviewer to reviewer-2") || !strings.Contains(got, "billing") {
		t.Fatalf("preview = %q", got)
	}
	if _, isErr := tl.Execute(context.Background(), `{"agent":"reviewer","new_name":"reviewer-2"}`); isErr {
		t.Fatal("a copy without a space is valid")
	}
	if _, isErr := tl.Execute(context.Background(), in); isErr {
		t.Fatal("valid copy refused")
	}
	if len(m.edits) != 2 || m.edits[0] != "copy_agent:reviewer:reviewer-2:" || m.edits[1] != "copy_agent:reviewer:reviewer-2:billing" {
		t.Fatalf("calls = %v", m.edits)
	}
}
