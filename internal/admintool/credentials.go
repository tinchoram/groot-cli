package admintool

import (
	"context"
	"fmt"
	"strings"

	"github.com/tinchoram/groot-cli/internal/tool"
)

const credentialSchema = `{"type":"object","properties":{"agent":{"type":"string","description":"Agent name, as list_agents shows it"},"set":{"type":"string","description":"Credential set: aws, kube, ssh, gcloud, azure, docker, gh, gnupg or netrc"}},"required":["agent","set"],"additionalProperties":false}`

// NewGrantCredentials returns grant_credentials: it lets an agent's commands
// read one credential set. The change is saved in the agent's file and applies
// to its open conversations at once. It is an admin change: work mode never
// approves it and a session grant cannot cover it.
func NewGrantCredentials(m Manager) tool.Tool {
	return credentialEdit{base{"grant_credentials",
		"Let an agent's commands read one credential set in the home directory (aws, kube, ssh, gcloud, azure, docker, gh, gnupg or netrc). The set is read-only inside the sandbox, and the change is saved in the agent's file. Use revoke_credentials to take it back. The user confirms every grant.",
		credentialSchema, tool.RiskWrite}, m, true}
}

// NewRevokeCredentials returns revoke_credentials: it takes one credential set
// away from an agent.
func NewRevokeCredentials(m Manager) tool.Tool {
	return credentialEdit{base{"revoke_credentials",
		"Take one credential set away from an agent, so its commands can no longer read it. The change is saved in the agent's file and applies to its open conversations at once. Use grant_credentials to give it back.",
		credentialSchema, tool.RiskWrite}, m, false}
}

type credentialInput struct {
	Agent string `json:"agent"`
	Set   string `json:"set"`
}

type credentialEdit struct {
	base
	m     Manager
	grant bool
}

func (t credentialEdit) Preview(input string) string {
	var in credentialInput
	if err := decode(input, &in); err != nil {
		return "credential change: invalid input"
	}
	if t.grant {
		return fmt.Sprintf("grant %s credentials to %s", in.Set, in.Agent)
	}
	return fmt.Sprintf("revoke %s credentials from %s", in.Set, in.Agent)
}

func (t credentialEdit) Execute(_ context.Context, input string) (string, bool) {
	var in credentialInput
	if err := decode(input, &in); err != nil {
		return err.Error(), true
	}
	if strings.TrimSpace(in.Agent) == "" || strings.TrimSpace(in.Set) == "" {
		return "agent and set are required", true
	}
	var add, remove []string
	if t.grant {
		add = []string{in.Set}
	} else {
		remove = []string{in.Set}
	}
	if err := t.m.EditAgentCredentials(in.Agent, add, remove); err != nil {
		return err.Error(), true
	}
	if t.grant {
		return fmt.Sprintf("granted %s to %s", in.Set, in.Agent), false
	}
	return fmt.Sprintf("revoked %s from %s", in.Set, in.Agent), false
}
