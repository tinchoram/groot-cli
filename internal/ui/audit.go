package ui

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/tinchoram/groot-cli/internal/audit"
	"github.com/tinchoram/groot-cli/internal/workspace"
)

const (
	// auditDefaultLimit is how many records /audit shows without a count.
	auditDefaultLimit = 20
	// auditMaxLimit caps the count /audit accepts.
	auditMaxLimit = 200
	// auditFixedWidth is the width the audit table takes before the preview.
	auditFixedWidth = 84
	// auditSpaceWidth is the extra width of the SPACE column.
	auditSpaceWidth = 14
	// auditMinPreview keeps the preview readable on narrow terminals.
	auditMinPreview = 20
)

// auditUsage is the reply to a /audit command that does not parse.
const auditUsage = "usage: /audit [n] [all] [denied|guard|errors] [tool <name>] [agent <name>] [risk <r>] [decision allow|deny] [since <when>] [until <when>] [find <text>] | /audit stats [filters]"

// auditRequest is one parsed /audit command.
type auditRequest struct {
	Query audit.Query
	All   bool // every space, not the active one
	Stats bool // the counters block instead of the table
}

// parseAudit reads the words after /audit. Filters combine; a bare number is the
// count; "find" and the other values take the next word, or a quoted phrase.
func parseAudit(arg string, now time.Time) (auditRequest, error) {
	req := auditRequest{Query: audit.Query{Limit: auditDefaultLimit}}
	words, err := auditWords(arg)
	if err != nil {
		return req, err
	}
	for i := 0; i < len(words); i++ {
		w := words[i]
		value := func() (string, error) {
			if i+1 >= len(words) {
				return "", fmt.Errorf("%s needs a value", w)
			}
			i++
			return words[i], nil
		}
		switch {
		case isDigits(w):
			n, err := strconv.Atoi(w)
			if err != nil || n < 1 || n > auditMaxLimit {
				return req, fmt.Errorf("limit must be between 1 and %d", auditMaxLimit)
			}
			req.Query.Limit = n
		case w == "all":
			req.All = true
		case w == "stats":
			req.Stats = true
		case w == "denied":
			req.Query.Decision = "deny"
		case w == "guard":
			req.Query.GuardOnly = true
		case w == "errors":
			req.Query.ErrorsOnly = true
		case w == "tool", w == "agent", w == "risk", w == "find":
			v, err := value()
			if err != nil {
				return req, err
			}
			switch w {
			case "tool":
				req.Query.Tool = v
			case "agent":
				req.Query.Agent = v
			case "risk":
				req.Query.Risk = v
			case "find":
				req.Query.Find = v
			}
		case w == "decision":
			v, err := value()
			if err != nil {
				return req, err
			}
			if v != "allow" && v != "deny" {
				return req, fmt.Errorf("decision must be allow or deny, got %q", v)
			}
			req.Query.Decision = v
		case w == "since", w == "until":
			v, err := value()
			if err != nil {
				return req, err
			}
			t, err := audit.ParseWhen(v, now)
			if err != nil {
				return req, err
			}
			if w == "since" {
				req.Query.Since = t
			} else {
				req.Query.Until = t
			}
		default:
			return req, fmt.Errorf("unknown filter %q", w)
		}
	}
	return req, nil
}

// auditWords splits the argument on spaces. A double-quoted phrase is one word.
func auditWords(s string) ([]string, error) {
	var (
		out    []string
		cur    strings.Builder
		quoted bool
		inWord bool
	)
	for _, r := range s {
		switch {
		case r == '"':
			quoted = !quoted
			inWord = true
		case unicode.IsSpace(r) && !quoted:
			if inWord {
				out = append(out, cur.String())
				cur.Reset()
				inWord = false
			}
		default:
			cur.WriteRune(r)
			inWord = true
		}
	}
	if quoted {
		return nil, fmt.Errorf("unclosed quote")
	}
	if inWord {
		out = append(out, cur.String())
	}
	return out, nil
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// runAuditCommand handles /audit: the last records of the active space, the same
// across every space with "all", and the counters with "stats".
func (m *Model) runAuditCommand(arg string) {
	req, err := parseAudit(arg, time.Now())
	if err != nil {
		m.append(styleError.Render(err.Error()))
		m.append(styleDim.Render(auditUsage))
		return
	}
	ctx := context.Background()
	if req.Stats {
		var st audit.Stats
		if req.All {
			_, st, err = m.ws.AuditAll(ctx, req.Query)
		} else {
			_, st, err = m.ws.Audit(ctx, "", req.Query)
		}
		if err != nil {
			m.append(styleError.Render("error: " + err.Error()))
			return
		}
		m.append(workspace.FormatAuditStats(st))
		return
	}
	var recs []audit.Record
	if req.All {
		recs, _, err = m.ws.AuditAll(ctx, req.Query)
	} else {
		recs, err = m.ws.AuditRecords(ctx, "", req.Query)
	}
	if err != nil {
		m.append(styleError.Render("error: " + err.Error()))
		return
	}
	m.append(workspace.FormatAudit(recs, workspace.AuditFormat{Width: m.auditPreviewWidth(req.All), Space: req.All}))
}

// auditPreviewWidth is the room the preview column has after the fixed columns.
func (m Model) auditPreviewWidth(withSpace bool) int {
	w := m.contentWidth() - auditFixedWidth
	if withSpace {
		w -= auditSpaceWidth
	}
	if w < auditMinPreview {
		return auditMinPreview
	}
	return w
}

// showSinkWarnings adds the export sink warnings the workspace raised since the
// last call to the transcript. Each warning comes once.
func (m *Model) showSinkWarnings() {
	for _, w := range m.ws.SinkWarnings() {
		m.lines = append(m.lines, styleError.Render(w))
	}
}
