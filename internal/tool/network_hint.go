package tool

import "strings"

// networkHint is the line added to a failed command that could not reach the
// network because the space has it off.
const networkHint = "[sandbox: network is off for this space; the user can turn it on with /space network on]"

// networkFailureSignatures are the messages that name a network failure: name
// resolution and unreachable networks from curl, git, glibc, macOS and Go, and
// the denial that the macOS sandbox produces for an outbound connection. The
// list is conservative: a message that is not in it gets no hint.
var networkFailureSignatures = []string{
	"could not resolve host",
	"temporary failure in name resolution",
	"network is unreachable",
	"nodename nor servname provided",
	"no route to host",
}

// networkFailure reports whether out carries a network failure signature.
func networkFailure(out string) bool {
	lower := strings.ToLower(out)
	for _, sig := range networkFailureSignatures {
		if strings.Contains(lower, sig) {
			return true
		}
	}
	// Go reports a sandbox-denied connection as a dial error with this text.
	return strings.Contains(lower, "dial tcp") && strings.Contains(lower, "connect: operation not permitted")
}
