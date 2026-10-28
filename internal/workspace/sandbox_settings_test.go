package workspace

import (
	"context"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/tinchoram/groot-cli/internal/admintool"
	"github.com/tinchoram/groot-cli/internal/audit"
	"github.com/tinchoram/groot-cli/internal/catalog"
	"github.com/tinchoram/groot-cli/internal/sandbox"
	"github.com/tinchoram/groot-cli/internal/space"
	"github.com/tinchoram/groot-cli/internal/tool"
)

// specRecorder is a sandbox runner that records every spec and runs the command unwrapped.
type specRecorder struct{ specs []sandbox.Spec }

func (r *specRecorder) Name() string { return sandbox.BackendBwrap }

func (r *specRecorder) Wrap(ctx context.Context, spec sandbox.Spec) (*exec.Cmd, error) {
	r.specs = append(r.specs, spec)
	return exec.CommandContext(ctx, spec.Command[0], spec.Command[1:]...), nil
}

func (r *specRecorder) last(t *testing.T) sandbox.Spec {
	t.Helper()
	if len(r.specs) == 0 {
		t.Fatal("no command was wrapped")
	}
	return r.specs[len(r.specs)-1]
}

// runBash runs the bash tool that a conversation of the space would get and
// returns the spec the runner received.
func runBash(t *testing.T, bash tool.Tool, rec *specRecorder) sandbox.Spec {
	t.Helper()
	if _, isErr := bash.Execute(context.Background(), `{"command":"true"}`); isErr {
		t.Fatal("bash failed")
	}
	return rec.last(t)
}

func TestSpaceNetworkFollowsTheGlobalSettingUnlessOverridden(t *testing.T) {
	f := newFixture(t)
	if got, err := f.ws.SpaceNetwork("default"); err != nil || got.Value != "on" || got.From != SourceGlobal {
		t.Fatalf("SpaceNetwork = %+v, %v; want on from the global default", got, err)
	}
	f.ws.opts.Config.Bash.Sandbox.Network = false
	if got, _ := f.ws.SpaceNetwork("default"); got.Value != "off" {
		t.Fatalf("a global off must show off, got %+v", got)
	}

	if err := f.ws.SetSpaceNetwork("default", true); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.ws.SpaceNetwork("default"); got.Value != "on" || got.From != SourceSpace {
		t.Fatalf("the space override must win over a global off: %+v", got)
	}
	if err := f.ws.SetSpaceNetwork("default", false); err != nil {
		t.Fatal(err)
	}
	f.ws.opts.Config.Bash.Sandbox.Network = true
	if got, _ := f.ws.SpaceNetwork("default"); got.Value != "off" || got.From != SourceSpace {
		t.Fatalf("a space off must win over a global on: %+v", got)
	}
}

func TestSpaceNetworkIsSavedInTheSpacesFile(t *testing.T) {
	f := newFixture(t)
	if err := f.ws.SetSpaceNetwork("default", false); err != nil {
		t.Fatal(err)
	}
	reopened, err := space.Open(filepath.Join(f.home, "spaces.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	sp, _ := reopened.Get("default")
	if sp.Network == nil || *sp.Network {
		t.Fatalf("spaces.yaml network = %v, want false", sp.Network)
	}
	if err := f.ws.SetSpaceNetwork("ghost", true); err == nil {
		t.Fatal("an unknown space must be refused")
	}
}

func TestBashOfAConversationCarriesTheEffectiveNetwork(t *testing.T) {
	f := newFixture(t)
	rec := &specRecorder{}
	f.ws.opts.Sandbox = rec
	bash := func() tool.Tool {
		t.Helper()
		reg, err := f.ws.buildTools(f.dir, []string{"bash"}, nil, nil, nil, nil, nil, "reader")
		if err != nil {
			t.Fatal(err)
		}
		bt, _ := reg.Get("bash")
		return bt
	}
	if spec := runBash(t, bash(), rec); !spec.AllowNetwork {
		t.Fatal("the global default is network on, and the bash must carry it")
	}
	if err := f.ws.SetSpaceNetwork("default", false); err != nil {
		t.Fatal(err)
	}
	if spec := runBash(t, bash(), rec); spec.AllowNetwork {
		t.Fatal("a space with network off must run its bash without network")
	}
}

func TestSetSpaceNetworkReachesTheOpenConversation(t *testing.T) {
	f := newFixture(t, text("ok"))
	rec := &specRecorder{}
	f.ws.opts.Sandbox = rec
	if err := f.ws.EditAgentTools("reader", []string{"bash"}, nil); err != nil {
		t.Fatal(err)
	}
	_ = f.ws.SelectAgent("reader")
	conv := f.ws.Chat().(*Conversation)
	session := conv.SessionID()

	if err := f.ws.SetSpaceNetwork("default", false); err != nil {
		t.Fatal(err)
	}
	bt, ok := conv.Agent.Tools.Get("bash")
	if !ok {
		t.Fatal("the open conversation lost its bash tool")
	}
	if spec := runBash(t, bt, rec); spec.AllowNetwork {
		t.Fatal("the open conversation must get the new network setting at once")
	}
	if f.ws.Chat().(*Conversation) != conv || conv.SessionID() != session {
		t.Fatal("the open conversation must be kept with its session")
	}
}

func TestBashCarriesTheCredentialSetsOfItsAgent(t *testing.T) {
	f := newFixture(t)
	rec := &specRecorder{}
	f.ws.opts.Sandbox = rec
	if err := f.ws.EditAgentTools("reader", []string{"bash"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := f.ws.EditAgentCredentials("reader", []string{"aws", "kube"}, nil); err != nil {
		t.Fatalf("EditAgentCredentials: %v", err)
	}
	reg, err := f.ws.buildTools(f.dir, []string{"bash"}, nil, nil, nil, nil, nil, "reader")
	if err != nil {
		t.Fatal(err)
	}
	bt, _ := reg.Get("bash")
	spec := runBash(t, bt, rec)
	if !slices.Equal(spec.ReadableSecrets, []string{".aws", ".kube"}) {
		t.Fatalf("ReadableSecrets = %q", spec.ReadableSecrets)
	}
	if cs, ok := bt.(tool.Credentialed); !ok || !slices.Equal(cs.CredentialSets(), []string{"aws", "kube"}) {
		t.Fatal("the bash tool must name its credential sets")
	}
}

func TestEditAgentCredentialsSavesTheFileAndRefusesUnknownSets(t *testing.T) {
	f := newFixture(t)
	if err := f.ws.EditAgentCredentials("reader", []string{"aws"}, nil); err != nil {
		t.Fatal(err)
	}
	defs, err := catalog.Load(f.home)
	if err != nil {
		t.Fatal(err)
	}
	var reader catalog.AgentDef
	for _, d := range defs {
		if d.Name == "reader" {
			reader = d
		}
	}
	if !slices.Equal(reader.Credentials, []string{"aws"}) {
		t.Fatalf("saved Credentials = %q", reader.Credentials)
	}

	for _, bad := range []string{"vault", ".groot"} {
		if err := f.ws.EditAgentCredentials("reader", []string{bad}, nil); err == nil {
			t.Fatalf("granting %q must be refused", bad)
		}
	}
	if got, _ := f.ws.Agent("reader"); !slices.Equal(got.Credentials, []string{"aws"}) {
		t.Fatalf("a refused grant changed the agent: %q", got.Credentials)
	}

	if err := f.ws.EditAgentCredentials("reader", nil, []string{"aws"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.ws.Agent("reader"); len(got.Credentials) != 0 {
		t.Fatalf("revoked credentials = %q, want none", got.Credentials)
	}
}

func TestGrantCredentialsReachesTheOpenConversationsBash(t *testing.T) {
	f := newFixture(t, text("ok"))
	rec := &specRecorder{}
	f.ws.opts.Sandbox = rec
	if err := f.ws.EditAgentTools("reader", []string{"bash"}, nil); err != nil {
		t.Fatal(err)
	}
	_ = f.ws.SelectAgent("reader")
	conv := f.ws.Chat().(*Conversation)

	if err := f.ws.EditAgentCredentials("reader", []string{"aws"}, nil); err != nil {
		t.Fatal(err)
	}
	bt, _ := conv.Agent.Tools.Get("bash")
	if spec := runBash(t, bt, rec); !slices.Equal(spec.ReadableSecrets, []string{".aws"}) {
		t.Fatalf("the open conversation's bash must see the grant: %q", spec.ReadableSecrets)
	}
	if err := f.ws.EditAgentCredentials("reader", nil, []string{"aws"}); err != nil {
		t.Fatal(err)
	}
	bt, _ = conv.Agent.Tools.Get("bash")
	if spec := runBash(t, bt, rec); len(spec.ReadableSecrets) != 0 {
		t.Fatalf("the revoke must reach the open conversation: %q", spec.ReadableSecrets)
	}
}

func TestAgentInfoListsItsCredentialSets(t *testing.T) {
	f := newFixture(t)
	if err := f.ws.EditAgentCredentials("reader", []string{"kube", "aws"}, nil); err != nil {
		t.Fatal(err)
	}
	info, err := f.ws.Agent("reader")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(info.Credentials, []string{"kube", "aws"}) {
		t.Fatalf("AgentInfo.Credentials = %q", info.Credentials)
	}
}

func TestCredentialAdminToolsAreKeptOnGrootAndEditTheFile(t *testing.T) {
	for _, name := range []string{"grant_credentials", "revoke_credentials"} {
		if !slices.Contains(grootDef().Tools, name) || !slices.Contains(requiredTools(managedAgent), name) {
			t.Errorf("groot must carry %s and keep it", name)
		}
		if !slices.Contains(catalog.KnownTools, name) {
			t.Errorf("%s is not a known tool", name)
		}
	}

	f := newFixture(t)
	ctx := context.Background()
	out, isErr := admintool.NewGrantCredentials(f.ws.Manager()).Execute(ctx, `{"agent":"reader","set":"aws"}`)
	if isErr || out != "granted aws to reader" {
		t.Fatalf("grant_credentials = %q (error %v)", out, isErr)
	}
	if got, _ := f.ws.Agent("reader"); !slices.Equal(got.Credentials, []string{"aws"}) {
		t.Fatalf("the grant is not saved: %q", got.Credentials)
	}
	out, isErr = admintool.NewRevokeCredentials(f.ws.Manager()).Execute(ctx, `{"agent":"reader","set":"aws"}`)
	if isErr || out != "revoked aws from reader" {
		t.Fatalf("revoke_credentials = %q (error %v)", out, isErr)
	}
	if got, _ := f.ws.Agent("reader"); len(got.Credentials) != 0 {
		t.Fatalf("the revoke is not saved: %q", got.Credentials)
	}
}

func TestEditSpaceSetsTheNetworkThroughTheAdminManager(t *testing.T) {
	f := newFixture(t)
	if err := f.ws.Manager().SetSpaceNetwork("default", false); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.ws.SpaceNetwork("default"); got.Value != "off" {
		t.Fatalf("network = %+v, want off", got)
	}
}

func TestAuditLineNamesTheCredentialSets(t *testing.T) {
	out := FormatAudit([]audit.Record{{
		Agent: "reader", Tool: "bash", Risk: "exec", Decision: "allow",
		InputPreview: "aws s3 ls", Credentials: []string{"aws"},
	}}, AuditFormat{Width: 80})
	if !strings.Contains(out, "credentials: aws") {
		t.Fatalf("audit line lacks the credential sets:\n%s", out)
	}
	plain := FormatAudit([]audit.Record{{Agent: "reader", Tool: "bash", Risk: "exec", Decision: "allow", InputPreview: "ls"}}, AuditFormat{Width: 80})
	if strings.Contains(plain, "credentials") {
		t.Fatalf("a call without sets must not name them:\n%s", plain)
	}
}

func TestRecordingToolForwardsTheCredentialSets(t *testing.T) {
	b := tool.NewBash(t.TempDir())
	b.Credentials = []string{"gh"}
	var wrapped tool.Tool = recordingTool{Tool: b}
	cs, ok := wrapped.(tool.Credentialed)
	if !ok || !slices.Equal(cs.CredentialSets(), []string{"gh"}) {
		t.Fatal("the recording wrapper must pass the credential sets of bash through")
	}
}
