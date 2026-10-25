package workspace

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/tinchoram/groot-cli/internal/feedback"
	"github.com/tinchoram/groot-cli/internal/inbox"
)

// feedbackSubjectRunes caps the part of the text that becomes the message subject.
const feedbackSubjectRunes = 60

// errDebugNotInstalled is returned when debug is assigned to no space: reports
// and the bare "debug" address have nowhere to go until it is installed.
var errDebugNotInstalled = errors.New("the debug agent is not installed: ask groot to install it (install_agent debug), or run /agent install debug")

// debugSpace is the space debug is assigned to: root when it is there, else
// the first space that has it, else "" when debug is installed nowhere.
// Reports and the bare "debug" address follow it when the user moves it.
func (w *Workspace) debugSpace() string {
	if sp, ok := w.opts.Spaces.Get(rootSpaceName); ok && slices.Contains(sp.Agents, debugAgent) {
		return rootSpaceName
	}
	for _, sp := range w.opts.Spaces.List() {
		if slices.Contains(sp.Agents, debugAgent) {
			return sp.Name
		}
	}
	return ""
}

// feedbackRecipient is where reports go: the debug agent, wherever it is
// assigned. Only debug writes the backlog. It fails when debug is not installed.
func (w *Workspace) feedbackRecipient() (string, error) {
	sp := w.debugSpace()
	if sp == "" {
		return "", errDebugNotInstalled
	}
	return sp + "/" + debugAgent, nil
}

// SendFeedback sends a bug or a feature request about groot to the debug agent,
// as a message from the given space and agent. Nothing is written to the backlog
// here. The context lines and the version travel in the body.
func (w *Workspace) SendFeedback(spaceName, agent, kind, text string, lines []string) (int64, string, error) {
	text = strings.TrimSpace(text)
	subject := kind + ": " + truncateRunes(firstLineOf(text), feedbackSubjectRunes)

	footer := fmt.Sprintf("\n\nversion: %s · space: %s · agent: %s", w.opts.Version, spaceName, agent)
	var body strings.Builder
	body.WriteString(text)
	if len(lines) > 0 {
		body.WriteString("\n\n## Context\n\n")
		body.WriteString(strings.Join(lines, "\n"))
	}
	// A report is never refused for its size: a long paste is cut and marked,
	// and the footer is kept so debug still knows where it came from.
	content := capBytes(body.String(), inbox.MaxBodyBytes-len(footer))
	to, err := w.feedbackRecipient()
	if err != nil {
		return 0, "", err
	}
	recipient, id, err := w.messengerFor(spaceName, agent).Send(context.Background(), to, subject, content+footer)
	if err != nil {
		return 0, "", err
	}
	return id, recipient, nil
}

// firstLineOf is the first line of s.
func firstLineOf(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// truncateRunes cuts s to n characters.
func truncateRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

// Backlog lists the items with the given status, or all of them when status is "".
func (w *Workspace) Backlog(status string) ([]feedback.Item, error) {
	return w.feedback.List(status)
}

// spaceNameFor returns the name of the space whose directory is root. A root
// that is no space's directory is returned unchanged.
func (w *Workspace) spaceNameFor(root string) string {
	for _, sp := range w.opts.Spaces.List() {
		if sp.Path == root {
			return sp.Name
		}
	}
	return root
}

// capBytes cuts s to at most n bytes on a rune boundary and marks the cut.
func capBytes(s string, n int) string {
	const mark = "\n[truncated]"
	if len(s) <= n {
		return s
	}
	limit := n - len(mark)
	if limit < 0 {
		limit = 0
	}
	for limit > 0 && !utf8.RuneStart(s[limit]) {
		limit--
	}
	return s[:limit] + mark
}
