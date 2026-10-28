package ui

import (
	"strings"
	"testing"

	"github.com/tinchoram/groot-cli/internal/gate"
	"github.com/tinchoram/groot-cli/internal/tool"
	"github.com/tinchoram/groot-cli/internal/workspace"
)

// SpaceNetwork answers with the fake's network setting.
func (f *fakeWorkspace) SpaceNetwork(string) (workspace.Setting, error) {
	return f.network, nil
}

// SetSpaceNetwork records the change and makes it the setting the fake reports.
func (f *fakeWorkspace) SetSpaceNetwork(name string, on bool) error {
	value := "off"
	if on {
		value = "on"
	}
	f.networkSet = append(f.networkSet, name+":"+value)
	f.network = workspace.Setting{Value: value, From: workspace.SourceSpace}
	return nil
}

func TestSpaceNetworkShowsTheSettingAndItsOrigin(t *testing.T) {
	m, ws := newTestModel(t, &fakeSession{model: "m"})
	ws.network = workspace.Setting{Value: "on", From: workspace.SourceGlobal}
	m = sendLine(t, m, "/space network")
	if !strings.Contains(stripANSI(m.transcriptText()), "network: on (global default)") {
		t.Fatalf("/space network must name the global default:\n%s", m.transcriptText())
	}
}

func TestSpaceNetworkSetsTheSpaceOverride(t *testing.T) {
	m, ws := newTestModel(t, &fakeSession{model: "m"})
	ws.network = workspace.Setting{Value: "on", From: workspace.SourceGlobal}
	m = sendLine(t, m, "/space network off")
	active, _ := ws.ActiveSpace()
	if len(ws.networkSet) != 1 || ws.networkSet[0] != active.Name+":off" {
		t.Fatalf("networkSet = %q", ws.networkSet)
	}
	if !strings.Contains(stripANSI(m.transcriptText()), "network: off (space)") {
		t.Fatalf("after the change the space override must show:\n%s", m.transcriptText())
	}
}

func TestSpaceNetworkRefusesOtherValues(t *testing.T) {
	m, ws := newTestModel(t, &fakeSession{model: "m"})
	m = sendLine(t, m, "/space network maybe")
	if len(ws.networkSet) != 0 {
		t.Fatalf("a bad value must change nothing: %q", ws.networkSet)
	}
	if !strings.Contains(stripANSI(m.transcriptText()), "usage: /space network [on|off]") {
		t.Fatalf("a bad value must show the usage:\n%s", m.transcriptText())
	}
}

func TestSpaceHelpListsTheNetworkCommand(t *testing.T) {
	found := false
	for _, e := range helpEntries {
		if strings.HasPrefix(e.usage, "/space network") {
			found = true
		}
	}
	if !found {
		t.Fatal("/help must list /space network")
	}
}

func TestApprovalShowsTheCredentialSets(t *testing.T) {
	m, ws := newTestModel(t, &fakeSession{model: "m"})
	key := ws.ConversationKey()
	reply := make(chan bool, 1)
	req := gate.Request{
		Tool: "bash", Risk: tool.RiskExec, Input: `{"command":"aws s3 ls"}`,
		Detail: "aws s3 ls", Credentials: []string{"aws"},
	}
	next, _ := m.Update(confirmMsg{key: key, req: req, reply: reply})
	m = next.(Model)
	if view := m.View(); !strings.Contains(view, "credentials: aws") {
		t.Fatalf("the approval must name the credential sets:\n%s", view)
	}
}

func TestCredentialsLineIsEmptyWithoutSets(t *testing.T) {
	if got := credentialsLine(gate.Request{Tool: "bash", Detail: "ls"}); got != "" {
		t.Fatalf("credentialsLine = %q, want none", got)
	}
	if got := credentialsLine(gate.Request{Credentials: []string{"aws", "kube"}}); got != "credentials: aws, kube" {
		t.Fatalf("credentialsLine = %q", got)
	}
}

func TestAgentDetailShowsTheCredentialSets(t *testing.T) {
	with := stripANSI(agentDetailBlock(workspace.AgentInfo{Name: "ops", Description: "d", Tools: []string{"bash"}, Credentials: []string{"aws", "kube"}}))
	if !strings.Contains(with, "credentials: aws, kube") {
		t.Fatalf("/agent show must list the sets:\n%s", with)
	}
	without := stripANSI(agentDetailBlock(workspace.AgentInfo{Name: "ops", Description: "d", Tools: []string{"bash"}}))
	if strings.Contains(without, "credentials") {
		t.Fatalf("an agent without sets must not mention them:\n%s", without)
	}
}
