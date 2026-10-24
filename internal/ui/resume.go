package ui

import (
	"fmt"
	"strings"

	"github.com/tinchoram/groot-cli/internal/api"
	"github.com/tinchoram/groot-cli/internal/workspace"
)

// restoreTranscript draws the stored transcript of a resumed session the first
// time its conversation is shown in this run. Later visits keep the lines that
// are already on screen.
func (m *Model) restoreTranscript(key string) {
	if key == "" || m.restored[key] {
		return
	}
	m.restored[key] = true
	info, ok := m.session.Resumed()
	if !ok || len(m.lines) > 0 {
		return
	}
	m.appendStored(info)
}

// appendStored writes the resume line and the stored turns: each user message
// with its divider and ❯ echo, assistant text as Markdown, and tool calls as
// one line each. Tool results are not shown. Each turn's divider carries the
// time the message was written; an old message falls back to the session start.
func (m *Model) appendStored(info workspace.Resumed) {
	m.append(styleDim.Render(fmt.Sprintf("resumed session %s · %d messages · last activity %s",
		info.ID, info.Count, info.LastActivity.Local().Format("2006-01-02 15:04"))))
	for i, msg := range info.Messages {
		switch msg.Role {
		case api.RoleUser:
			text, ok := plainUserText(msg)
			if !ok {
				continue
			}
			prefix, typed := splitInjected(text)
			for _, line := range prefix {
				m.append(styleDim.Render(line))
			}
			if typed == "" {
				continue
			}
			at := info.Started
			if i < len(info.Times) && !info.Times[i].IsZero() {
				at = info.Times[i]
			}
			m.append("\n" + turnHeaderAt(m.vp.Width, at.Local()))
			m.append(stylePrompt.Render("❯ ") + stylePromptText.Render(typed) + "\n")
		case api.RoleAssistant:
			for _, b := range msg.Content {
				switch b.Type {
				case api.BlockText:
					if strings.TrimSpace(b.Text) != "" {
						m.addMarkdown(b.Text)
					}
				case api.BlockToolUse:
					m.append(styleTool.Render("[tool] " + b.Name))
				}
			}
		}
	}
}

// splitInjected separates what the workspace put in front of a user message
// (the inbox delivery, the task block) from what the user typed. The injected
// parts become one dim summary line each; the typed text is what is echoed.
func splitInjected(text string) (summaries []string, typed string) {
	chunks := strings.Split(text, "\n\n")
	i := 0
	for ; i < len(chunks) && isInjected(chunks[i]); i++ {
		summaries = append(summaries, summarizeInjected(chunks[i]))
	}
	return summaries, strings.Join(chunks[i:], "\n\n")
}

// isInjected reports whether a block of the prompt was added by the workspace.
func isInjected(chunk string) bool {
	return strings.HasPrefix(chunk, "[inbox] ") || strings.HasPrefix(chunk, "[task ") ||
		strings.HasPrefix(chunk, "[classified ")
}

// summarizeInjected is the one-line form of an injected block. A delivered
// message names its sender and never shows its body.
func summarizeInjected(chunk string) string {
	first, _, _ := strings.Cut(chunk, "\n")
	if rest, ok := strings.CutPrefix(first, "[inbox] from "); ok {
		from, _, _ := strings.Cut(rest, " · ")
		return "[inbox] message delivered from " + from
	}
	return first
}

// plainUserText returns the text of a user message made only of text blocks.
// A tool result is also a user message, and it reports false.
func plainUserText(msg api.Message) (string, bool) {
	parts := make([]string, 0, len(msg.Content))
	for _, b := range msg.Content {
		if b.Type != api.BlockText {
			return "", false
		}
		parts = append(parts, b.Text)
	}
	if len(parts) == 0 {
		return "", false
	}
	return strings.Join(parts, "\n"), true
}
