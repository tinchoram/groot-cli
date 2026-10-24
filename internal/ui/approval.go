package ui

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tinchoram/groot-cli/internal/gate"
)

// approvalPrompt is the one line that asks for an approval in the inline box.
// A write names its path and its size; other calls show their first line of
// detail, followed by an ellipsis when there is more. The full detail is one
// /diff away.
func approvalPrompt(req gate.Request) string {
	if req.Tool == "write_file" {
		added, removed := diffCounts(req.Detail)
		return fmt.Sprintf("allow write_file: %s (+%d −%d)?", inputPath(req.Input), added, removed)
	}
	detail := strings.TrimSpace(req.Detail)
	if detail == "" {
		return fmt.Sprintf("allow %s (%s)?", req.Tool, req.Risk)
	}
	line, more := detail, false
	if i := strings.IndexByte(detail, '\n'); i >= 0 {
		line, more = detail[:i], true
	}
	prompt := fmt.Sprintf("allow %s: %s", req.Tool, line)
	if more {
		prompt += " …"
	}
	return prompt + "?"
}

// inputPath is the "path" field of a tool input, or "" when there is none.
func inputPath(input string) string {
	var in struct {
		Path string `json:"path"`
	}
	_ = json.Unmarshal([]byte(input), &in)
	return in.Path
}

// diffCounts counts the added and removed lines of a unified diff, leaving out
// the file headers.
func diffCounts(diff string) (added, removed int) {
	for _, line := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "+++"), strings.HasPrefix(line, "---"):
		case strings.HasPrefix(line, "+"):
			added++
		case strings.HasPrefix(line, "-"):
			removed++
		}
	}
	return added, removed
}

// showDiff prints the full detail of the pending approval into the transcript,
// highlighted. It does nothing when no approval waits.
func (m *Model) showDiff() {
	if m.pending == nil {
		m.append(styleDim.Render("no pending approval"))
		return
	}
	detail := strings.TrimSpace(m.pending.req.Detail)
	if detail == "" {
		m.append(styleDim.Render("the pending call has no detail"))
		return
	}
	m.append(HighlightPayload(detail, m.contentWidth()))
}
