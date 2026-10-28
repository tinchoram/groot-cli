package tool

import (
	"fmt"
	"slices"
	"strings"

	shellsandbox "github.com/tinchoram/groot-cli/internal/sandbox"
)

// credentialHint ties a credential set to the failures that a denied directory
// produces. The hint is for a failed command that could not read the directory
// because the agent does not hold the set.
type credentialHint struct {
	set     string
	dir     string
	failure func(lower string) bool
}

// credentialHints are checked in this order; the first set that matches wins.
var credentialHints = []credentialHint{
	{set: "ssh", dir: ".ssh", failure: func(s string) bool {
		return strings.Contains(s, "permission denied (publickey)") ||
			strings.Contains(s, "host key verification failed") ||
			strings.Contains(s, "could not read from remote repository") ||
			(strings.Contains(s, "no such file or directory") && strings.Contains(s, ".ssh/"))
	}},
	{set: "aws", dir: ".aws", failure: func(s string) bool {
		return strings.Contains(s, "unable to locate credentials") ||
			strings.Contains(s, "nocredentialproviders") ||
			strings.Contains(s, ".aws/credentials")
	}},
	{set: "kube", dir: ".kube", failure: func(s string) bool {
		return strings.Contains(s, ".kube/config") || strings.Contains(s, "no configuration has been provided")
	}},
}

// credentialHintFor returns the hint line for a failed command whose output
// names a directory of a denied credential set, or "" when none matches.
func credentialHintFor(out string, denied []string) string {
	lower := strings.ToLower(out)
	for _, h := range credentialHints {
		if slices.Contains(denied, h.set) && h.failure(lower) {
			return fmt.Sprintf("[sandbox: ~/%s is not readable by this agent; the user can grant it with credentials: [%s] in the agent file or grant_credentials %s]", h.dir, h.set, h.set)
		}
	}
	return ""
}

// deniedSets returns the credential sets the sandbox denies to the commands of
// b: every known set the agent does not hold, when a real sandbox enforces it.
// Without an enforcing sandbox nothing is reported as denied.
func (b *Bash) deniedSets() []string {
	if !b.sandboxEnforces() {
		return nil
	}
	var out []string
	for _, name := range shellsandbox.CredentialNames() {
		if !slices.Contains(b.Credentials, name) {
			out = append(out, name)
		}
	}
	return out
}
