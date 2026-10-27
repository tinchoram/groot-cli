package admintool

import (
	"context"
	"strings"
	"time"

	"github.com/tinchoram/groot-cli/internal/audit"
	"github.com/tinchoram/groot-cli/internal/tool"
)

// auditDefaultLimit is how many records audit_query returns when limit is unset.
const auditDefaultLimit = 20

// auditMaxLimit caps the records audit_query returns.
const auditMaxLimit = 200

// NewAuditQuery returns audit_query: the audit records of a space, newest first,
// as the same table /audit prints.
func NewAuditQuery(m Manager) tool.Tool {
	return auditQuery{base{"audit_query",
		"Read the audit log of a space: one row per tool call, newest first, with time, agent, tool, risk, decision, duration, flags (guard findings, denied, error, no sandbox) and a preview of the input. Use it to find what a tool did or was denied, and omit space for the active space. Filters combine: since takes a duration back from now (\"2h\", \"1d\") or a time, and find is a case-insensitive substring of the preview. limit defaults to 20 and is at most 200.",
		`{"type":"object","properties":{"space":{"type":"string","description":"space name; omit for the active space"},"since":{"type":"string","description":"a duration back from now (2h, 90m, 1d) or a time (2026-10-24 or RFC 3339)"},"tool":{"type":"string","description":"only this tool"},"decision":{"type":"string","enum":["allow","deny"],"description":"allow or deny"},"agent":{"type":"string","description":"only this agent"},"guard_only":{"type":"boolean","description":"only calls whose result had guard findings"},"errors_only":{"type":"boolean","description":"only failed calls"},"find":{"type":"string","description":"case-insensitive text in the input preview"},"limit":{"type":"integer","minimum":1,"maximum":200,"description":"records to return; default 20"}},"additionalProperties":false}`,
		tool.RiskRead}, m}
}

type auditQuery struct {
	base
	m Manager
}

type auditInput struct {
	Space      string `json:"space"`
	Since      string `json:"since"`
	Tool       string `json:"tool"`
	Decision   string `json:"decision"`
	Agent      string `json:"agent"`
	GuardOnly  bool   `json:"guard_only"`
	ErrorsOnly bool   `json:"errors_only"`
	Find       string `json:"find"`
	Limit      int    `json:"limit"`
}

func (t auditQuery) Preview(input string) string {
	var in auditInput
	if err := decode(input, &in); err != nil || in.Space == "" {
		return "read the audit log of the active space"
	}
	return "read the audit log of space " + in.Space
}

func (t auditQuery) Execute(ctx context.Context, input string) (string, bool) {
	var in auditInput
	if err := decode(input, &in); err != nil {
		return err.Error(), true
	}
	q := audit.Query{
		Tool:       strings.TrimSpace(in.Tool),
		Decision:   strings.TrimSpace(in.Decision),
		Agent:      strings.TrimSpace(in.Agent),
		GuardOnly:  in.GuardOnly,
		ErrorsOnly: in.ErrorsOnly,
		Find:       in.Find,
		Limit:      in.Limit,
	}
	if in.Since != "" {
		since, err := audit.ParseWhen(in.Since, time.Now())
		if err != nil {
			return err.Error(), true
		}
		q.Since = since
	}
	switch {
	case in.Limit < 0 || in.Limit > auditMaxLimit:
		return "limit must be between 1 and 200", true
	case in.Limit == 0:
		q.Limit = auditDefaultLimit
	}
	out, err := t.m.AuditReport(ctx, strings.TrimSpace(in.Space), q)
	if err != nil {
		return err.Error(), true
	}
	return out, false
}
