package workspace

import "github.com/tinchoram/groot-cli/internal/tool"

// The wrappers of this package embed tool.Tool, which carries only the required
// methods. The agent finds the optional ones by type assertion, so each wrapper
// forwards them here: Untrusted, SelfEnveloped, External, DestructiveFor, and in
// flow.go SandboxName and CredentialSets. A forwarder answers with the value of
// the wrapped tool, or the zero value when the wrapped tool has no such method,
// so a wrapper never claims a property the tool does not have.

// Untrusted forwards the wrapped tool's answer.
func (r recordingTool) Untrusted() bool {
	u, ok := r.Tool.(tool.Untrusted)
	return ok && u.Untrusted()
}

// SelfEnveloped forwards the wrapped tool's answer.
func (r recordingTool) SelfEnveloped() bool {
	s, ok := r.Tool.(tool.SelfEnveloped)
	return ok && s.SelfEnveloped()
}

// External forwards the wrapped tool's answer.
func (r recordingTool) External() bool {
	e, ok := r.Tool.(tool.External)
	return ok && e.External()
}

// DestructiveFor forwards the wrapped tool's classification of the input.
func (r recordingTool) DestructiveFor(input string) (tool.DestructiveKind, string, bool) {
	d, ok := r.Tool.(tool.Destructive)
	if !ok {
		return "", "", false
	}
	return d.DestructiveFor(input)
}

// Untrusted forwards the wrapped tool's answer.
func (t trackedTool) Untrusted() bool {
	u, ok := t.Tool.(tool.Untrusted)
	return ok && u.Untrusted()
}

// SelfEnveloped forwards the wrapped tool's answer.
func (t trackedTool) SelfEnveloped() bool {
	s, ok := t.Tool.(tool.SelfEnveloped)
	return ok && s.SelfEnveloped()
}

// External forwards the wrapped tool's answer.
func (t trackedTool) External() bool {
	e, ok := t.Tool.(tool.External)
	return ok && e.External()
}

// DestructiveFor forwards the wrapped tool's classification of the input.
func (t trackedTool) DestructiveFor(input string) (tool.DestructiveKind, string, bool) {
	d, ok := t.Tool.(tool.Destructive)
	if !ok {
		return "", "", false
	}
	return d.DestructiveFor(input)
}

// SandboxName forwards the sandbox of the wrapped tool, so the audit log still sees it.
func (t trackedTool) SandboxName() string {
	if s, ok := t.Tool.(interface{ SandboxName() string }); ok {
		return s.SandboxName()
	}
	return ""
}

// CredentialSets forwards the credential sets of the wrapped tool.
func (t trackedTool) CredentialSets() []string {
	if c, ok := t.Tool.(tool.Credentialed); ok {
		return c.CredentialSets()
	}
	return nil
}
