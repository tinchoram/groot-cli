package admintool

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tinchoram/groot-cli/internal/agentimport"
	"github.com/tinchoram/groot-cli/internal/tool"
)

type fakeManager struct {
	// edits records the six edit calls; editReport and editErr answer them.
	edits      []string
	editReport string
	editErr    error
	library    []LibraryInfo
	installed  []string
	installErr error
	imported   []string
	importRes  ImportResult
	importErr  error
	spaces     []SpaceInfo
	agents     []AgentInfo
	created    []SpaceSpecSeen
	specs      []AgentSpec
	assigned   []string
	failCreate error
}

type SpaceSpecSeen struct{ Name, Path string }

func (f *fakeManager) SetWorkMode(space, agent string, on bool) error { return nil }

func (f *fakeManager) UpgradeAgent(name string) ([]string, error) {
	return []string{name + ": up to date"}, nil
}

func (f *fakeManager) ListSpaces() []SpaceInfo { return f.spaces }
func (f *fakeManager) CreateSpace(name, path string) error {
	if f.failCreate != nil {
		return f.failCreate
	}
	f.created = append(f.created, SpaceSpecSeen{name, path})
	return nil
}
func (f *fakeManager) AssignAgent(space, agent string) error {
	f.assigned = append(f.assigned, "assign:"+space+":"+agent)
	return nil
}
func (f *fakeManager) UnassignAgent(space, agent string) error {
	f.assigned = append(f.assigned, "unassign:"+space+":"+agent)
	return nil
}
func (f *fakeManager) ListAgents() []AgentInfo { return f.agents }
func (f *fakeManager) ListLibrary() ([]LibraryInfo, error) {
	return f.library, nil
}
func (f *fakeManager) InstallAgent(name, space string, overwrite bool) error {
	f.installed = append(f.installed, fmt.Sprintf("%s:%s:%v", name, space, overwrite))
	return f.installErr
}
func (f *fakeManager) ImportAgent(plan agentimport.Planned, overwrite bool) (ImportResult, error) {
	f.imported = append(f.imported, fmt.Sprintf("%s:%v", plan.Source, overwrite))
	return f.importRes, f.importErr
}
func (f *fakeManager) CreateAgent(spec AgentSpec) error {
	if f.failCreate != nil {
		return f.failCreate
	}
	f.specs = append(f.specs, spec)
	return nil
}

func TestToolsAreNamedAndRiskClassed(t *testing.T) {
	m := &fakeManager{}
	want := map[string]tool.Risk{
		"list_spaces":          tool.RiskRead,
		"create_space":         tool.RiskWrite,
		"assign_agent":         tool.RiskWrite,
		"unassign_agent":       tool.RiskWrite,
		"list_agents":          tool.RiskRead,
		"create_agent":         tool.RiskWrite,
		"import_agent":         tool.RiskWrite,
		"list_library":         tool.RiskRead,
		"install_agent":        tool.RiskWrite,
		"upgrade_agent":        tool.RiskWrite,
		"set_work_mode":        tool.RiskWrite,
		"list_models":          tool.RiskRead,
		"set_agent_model":      tool.RiskWrite,
		"grant_tool":           tool.RiskWrite,
		"grant_global_memory":  tool.RiskWrite,
		"revoke_global_memory": tool.RiskWrite,
		"revoke_tool":          tool.RiskWrite,
		"set_default_model":    tool.RiskWrite,
		"set_space_route":      tool.RiskWrite,
		"set_space_delegation": tool.RiskWrite,
		"set_space_model":      tool.RiskWrite,
		"set_space_budget":     tool.RiskWrite,
		"set_agent_budget":     tool.RiskWrite,
		"get_context":          tool.RiskRead,
		"get_cost":             tool.RiskRead,
		"list_tasks":           tool.RiskRead,
		"audit_query":          tool.RiskRead,
		"set_context_window":   tool.RiskWrite,
		"delete_space":         tool.RiskWrite,
		"edit_space":           tool.RiskWrite,
		"delete_agent":         tool.RiskWrite,
		"edit_agent":           tool.RiskWrite,
		"move_agent":           tool.RiskWrite,
		"copy_agent":           tool.RiskWrite,
	}
	for _, tl := range All(m, "/home/sre") {
		risk, ok := want[tl.Definition().Name]
		if !ok {
			t.Fatalf("unexpected tool %q", tl.Definition().Name)
		}
		if tl.Risk() != risk {
			t.Errorf("%s risk = %v want %v", tl.Definition().Name, tl.Risk(), risk)
		}
		if !strings.HasPrefix(string(tl.Definition().InputSchema), `{"type":"object"`) {
			t.Errorf("%s needs an object schema", tl.Definition().Name)
		}
		delete(want, tl.Definition().Name)
	}
	if len(want) != 0 {
		t.Fatalf("missing tools: %v", want)
	}
}

func TestListToolsReturnFormattedText(t *testing.T) {
	m := &fakeManager{
		spaces: []SpaceInfo{{Name: "root", Path: "/home/sre", Agents: []string{"groot"}}},
		agents: []AgentInfo{{Name: "devjunior", Description: "junior dev", Model: "", Tools: []string{"bash"}}},
	}
	out, isErr := NewListSpaces(m).Execute(context.Background(), "{}")
	if isErr || !strings.Contains(out, "root") || !strings.Contains(out, "/home/sre") || !strings.Contains(out, "groot") {
		t.Fatalf("list_spaces = %q (%v)", out, isErr)
	}
	out, isErr = NewListAgents(m).Execute(context.Background(), "{}")
	if isErr || !strings.Contains(out, "devjunior") || !strings.Contains(out, "junior dev") {
		t.Fatalf("list_agents = %q (%v)", out, isErr)
	}
}

func TestCreateSpaceExpandsHomeAndPreviews(t *testing.T) {
	m := &fakeManager{}
	tl := NewCreateSpace(m, "/home/sre")
	in := `{"name":"payments","path":"~/src/pay"}`
	if got := tl.Preview(in); got != "create space payments at ~/src/pay" {
		t.Fatalf("preview = %q", got)
	}
	out, isErr := tl.Execute(context.Background(), in)
	if isErr {
		t.Fatalf("unexpected error: %s", out)
	}
	if len(m.created) != 1 || m.created[0].Path != filepath.Join("/home/sre", "src", "pay") {
		t.Fatalf("created = %+v", m.created)
	}
}

func TestCreateSpaceRejectsBadInput(t *testing.T) {
	cases := map[string]string{
		"missing name":  `{"path":"/tmp"}`,
		"relative path": `{"name":"x","path":"projects/x"}`,
		"not json":      `{"name":`,
		"unknown field": `{"name":"x","path":"/tmp","extra":1}`,
	}
	for label, in := range cases {
		m := &fakeManager{}
		out, isErr := NewCreateSpace(m, "/home/sre").Execute(context.Background(), in)
		if !isErr || out == "" {
			t.Errorf("%s: want is_error with a message, got %q (%v)", label, out, isErr)
		}
		if len(m.created) != 0 {
			t.Errorf("%s: nothing should be created", label)
		}
	}
}

func TestCreateSpacePropagatesManagerError(t *testing.T) {
	m := &fakeManager{failCreate: errors.New(`space "root" already exists`)}
	out, isErr := NewCreateSpace(m, "/home/sre").Execute(context.Background(), `{"name":"root","path":"/tmp"}`)
	if !isErr || !strings.Contains(out, "already exists") {
		t.Fatalf("got %q (%v)", out, isErr)
	}
}

func TestAssignAndUnassignRequireBothNames(t *testing.T) {
	m := &fakeManager{}
	out, isErr := NewAssignAgent(m).Execute(context.Background(), `{"space":"root"}`)
	if !isErr || out == "" {
		t.Fatalf("missing agent must be an error, got %q (%v)", out, isErr)
	}
	out, isErr = NewAssignAgent(m).Execute(context.Background(), `{"space":"root","agent":"groot"}`)
	if isErr || out == "" {
		t.Fatalf("assign failed: %q (%v)", out, isErr)
	}
	if NewAssignAgent(m).Preview(`{"space":"root","agent":"groot"}`) != "assign groot to space root" {
		t.Fatal("assign preview wrong")
	}
	_, _ = NewUnassignAgent(m).Execute(context.Background(), `{"space":"root","agent":"groot"}`)
	if len(m.assigned) != 2 || m.assigned[0] != "assign:root:groot" || m.assigned[1] != "unassign:root:groot" {
		t.Fatalf("assigned = %v", m.assigned)
	}
}

func TestCreateAgentPreviewShowsSettingsAndPrompt(t *testing.T) {
	in := `{"name":"auditor","description":"checks merges","tools":["read_file","bash"],"prompt":"You audit merges.\nBe strict.\nSay what you checked.\nLast line."}`
	preview := NewCreateAgent(&fakeManager{}).Preview(in)
	for _, want := range []string{"auditor", "checks merges", "read_file, bash", "You audit merges.", "Be strict."} {
		if !strings.Contains(preview, want) {
			t.Errorf("preview missing %q:\n%s", want, preview)
		}
	}
	if strings.Contains(preview, "Last line.") {
		t.Error("preview should show only the first lines of the prompt")
	}
}

func TestCreateAgentPassesSpecToManager(t *testing.T) {
	m := &fakeManager{}
	in := `{"name":"auditor","description":"checks merges","tools":["read_file"],"prompt":"You audit.","model":"claude-haiku-5-5"}`
	out, isErr := NewCreateAgent(m).Execute(context.Background(), in)
	if isErr {
		t.Fatalf("create_agent failed: %s", out)
	}
	if len(m.specs) != 1 || m.specs[0].Name != "auditor" || m.specs[0].Model != "claude-haiku-5-5" || m.specs[0].Prompt != "You audit." {
		t.Fatalf("spec = %+v", m.specs)
	}
}

func TestCreateAgentRequiresNameAndTools(t *testing.T) {
	m := &fakeManager{}
	for _, in := range []string{`{"description":"x","tools":["bash"]}`, `{"name":"x","description":"y"}`} {
		if _, isErr := NewCreateAgent(m).Execute(context.Background(), in); !isErr {
			t.Errorf("input %s should be rejected", in)
		}
	}
	if len(m.specs) != 0 {
		t.Fatal("invalid input must not reach the manager")
	}
}

func writeAgentFile(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "scout.md")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestImportPreviewShowsMappedToolsWarningsAndPrompt(t *testing.T) {
	src := writeAgentFile(t, "---\nname: scout\ndescription: finds code\ntools: [Read, Grep]\n---\nLine one.\nLine two.\n")
	preview := NewImportAgent(&fakeManager{}).Preview(fmt.Sprintf(`{"source":%q}`, src))
	for _, want := range []string{"scout", "finds code", "read_file", "Grep", "Line two."} {
		if !strings.Contains(preview, want) {
			t.Errorf("preview missing %q:\n%s", want, preview)
		}
	}
}

func TestImportExecuteCallsManagerAndReportsWarnings(t *testing.T) {
	src := writeAgentFile(t, "---\nname: scout\ntools: [Read, Grep]\n---\nBody.\n")
	m := &fakeManager{importRes: ImportResult{Name: "scout", Warnings: []string{"Grep has no equivalent yet and was dropped"}}}
	out, isErr := NewImportAgent(m).Execute(context.Background(), fmt.Sprintf(`{"source":%q,"overwrite":true}`, src))
	if isErr {
		t.Fatalf("import failed: %s", out)
	}
	if len(m.imported) != 1 || m.imported[0] != src+":true" {
		t.Fatalf("manager got %v", m.imported)
	}
	if !strings.Contains(out, "scout") || !strings.Contains(out, "Grep") {
		t.Fatalf("result should name the agent and the warnings: %q", out)
	}
}

func TestImportRejectsBadInputWithoutCallingManager(t *testing.T) {
	m := &fakeManager{}
	cases := map[string]string{
		"missing source":  `{}`,
		"unknown field":   `{"source":"x.md","extra":1}`,
		"unreadable file": `{"source":"/nonexistent/agent.md"}`,
	}
	for label, in := range cases {
		out, isErr := NewImportAgent(m).Execute(context.Background(), in)
		if !isErr || out == "" {
			t.Errorf("%s: want is_error, got %q (%v)", label, out, isErr)
		}
	}
	if len(m.imported) != 0 {
		t.Fatal("invalid imports must not reach the manager")
	}
	if p := NewImportAgent(m).Preview(`{"source":"/nonexistent/agent.md"}`); !strings.HasPrefix(p, "import agent:") {
		t.Fatalf("preview of an unreadable source should say so: %q", p)
	}
}

func TestImportPropagatesManagerError(t *testing.T) {
	src := writeAgentFile(t, "---\nname: groot\ntools: [Read]\n---\nx\n")
	m := &fakeManager{importErr: errors.New("agent \"groot\" is managed and cannot be overwritten")}
	out, isErr := NewImportAgent(m).Execute(context.Background(), fmt.Sprintf(`{"source":%q}`, src))
	if !isErr || !strings.Contains(out, "managed") {
		t.Fatalf("got %q (%v)", out, isErr)
	}
}

func TestListLibraryShowsNamesToolsAndSource(t *testing.T) {
	m := &fakeManager{library: []LibraryInfo{{Name: "sre", Description: "Kubernetes diagnosis", Tools: []string{"kubectl"}, Source: "builtin"}}}
	out, isErr := NewListLibrary(m).Execute(context.Background(), "{}")
	if isErr || !strings.Contains(out, "sre") || !strings.Contains(out, "kubectl") || !strings.Contains(out, "builtin") {
		t.Fatalf("list_library = %q (%v)", out, isErr)
	}
}

func TestInstallAgentPassesSpaceAndOverwrite(t *testing.T) {
	m := &fakeManager{}
	out, isErr := NewInstallAgent(m).Execute(context.Background(), `{"name":"sre","space":"root","overwrite":true}`)
	if isErr {
		t.Fatalf("install failed: %s", out)
	}
	if len(m.installed) != 1 || m.installed[0] != "sre:root:true" {
		t.Fatalf("manager got %v", m.installed)
	}
}

func TestInstallAgentPreviewAndValidation(t *testing.T) {
	tl := NewInstallAgent(&fakeManager{})
	if got := tl.Preview(`{"name":"sre","space":"root"}`); got != "install library agent sre into the catalog and assign it to space root" {
		t.Fatalf("preview = %q", got)
	}
	if got := tl.Preview(`{"name":"sre"}`); got != "install library agent sre into the catalog" {
		t.Fatalf("preview without space = %q", got)
	}
	if out, isErr := tl.Execute(context.Background(), `{}`); !isErr || out == "" {
		t.Fatalf("missing name must be is_error, got %q (%v)", out, isErr)
	}
	if _, isErr := tl.Execute(context.Background(), `{"name":"sre","extra":1}`); !isErr {
		t.Fatal("unknown fields must be rejected")
	}
}

func TestInstallAgentReturnsManagerRefusal(t *testing.T) {
	m := &fakeManager{installErr: errors.New(`agent "sre" already exists; pass overwrite to replace it`)}
	out, isErr := NewInstallAgent(m).Execute(context.Background(), `{"name":"sre"}`)
	if !isErr || !strings.Contains(out, "already exists") {
		t.Fatalf("refusal = %q (%v)", out, isErr)
	}
}

func TestListLibraryGroupsInCatalogOrderWithAuthorAndLicense(t *testing.T) {
	m := &fakeManager{library: []LibraryInfo{
		{Name: "developer", Description: "Engineer", Tools: []string{"bash"}, Source: "builtin", Collection: "@tinchoram", Author: "@tinchoram"},
		{Name: "devops-engineer", Description: "Pipelines", Tools: []string{"bash"}, Source: "public", Collection: "VoltAgent", Author: "VoltAgent/awesome", License: "MIT", ValidatedBy: "@tinchoram"},
	}}
	out, isErr := NewListLibrary(m).Execute(context.Background(), "{}")
	if isErr {
		t.Fatalf("list failed: %s", out)
	}
	for _, want := range []string{"collection @tinchoram by @tinchoram", "collection VoltAgent by VoltAgent/awesome · license MIT · validated by @tinchoram", "developer", "devops-engineer"} {
		if !strings.Contains(out, want) {
			t.Errorf("listing missing %q:\n%s", want, out)
		}
	}
	if strings.Index(out, "@tinchoram") > strings.Index(out, "VoltAgent") {
		t.Errorf("the catalog order must be kept (own collection first):\n%s", out)
	}
}

func TestInstallPreviewNamesPublicSource(t *testing.T) {
	m := &fakeManager{library: []LibraryInfo{
		{Name: "developer", Description: "Engineer", Source: "public", Collection: "VoltAgent", Author: "VoltAgent/awesome", URL: "https://example.com/developer.md"},
	}}
	got := NewInstallAgent(m).Preview(`{"name":"developer"}`)
	if !strings.Contains(got, "https://example.com/developer.md") {
		t.Fatalf("preview should show the source URL: %q", got)
	}
	local := NewInstallAgent(&fakeManager{library: []LibraryInfo{{Name: "sre", Source: "builtin"}}}).Preview(`{"name":"sre"}`)
	if strings.Contains(local, "http") {
		t.Fatalf("builtin preview should not mention a URL: %q", local)
	}
}
