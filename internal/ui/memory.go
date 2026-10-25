package ui

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/tinchoram/groot-cli/internal/memory"
)

// memoryTextRunes is how much of an entry's title or text a listing shows.
const memoryTextRunes = 80

// memoryUsage lists the forms of /memory.
const memoryUsage = "usage: /memory | /memory <query> | /memory show|pin|unpin|rm|reviewed <id> | /memory review"

// runMemoryCommand handles /memory and its forms: the entries in the prompt, a
// search, one entry in full, the pin, forget and review changes, and the
// review list. An unknown word is a search query; a known verb with bad
// arguments prints the usage.
func (m *Model) runMemoryCommand(arg string) {
	ctx := context.Background()
	fields := strings.Fields(arg)
	if len(fields) == 0 {
		m.listPreamble(ctx)
		return
	}
	switch fields[0] {
	case "show", "pin", "unpin", "rm", "reviewed":
		if len(fields) != 2 {
			m.append(styleDim.Render(memoryUsage))
			return
		}
		ref, err := parseMemoryRef(fields[1])
		if err != nil {
			m.append(styleError.Render("error: " + err.Error()))
			return
		}
		m.memoryAction(ctx, fields[0], ref)
	case "review":
		if len(fields) != 1 {
			m.append(styleDim.Render(memoryUsage))
			return
		}
		m.listReview(ctx)
	default:
		m.searchMemory(ctx, arg)
	}
}

// parseMemoryRef reads an entry id: a positive whole number for a space or agent
// entry, or g<number> for a global one. Ids are per database, so the g prefix is
// what selects the global entry when both databases use the number.
func parseMemoryRef(s string) (memory.Ref, error) {
	global := strings.HasPrefix(s, memoryGlobalPrefix)
	num := strings.TrimPrefix(s, memoryGlobalPrefix)
	id, err := strconv.ParseInt(num, 10, 64)
	if err != nil || id <= 0 {
		return memory.Ref{}, errors.New("id must be a positive number, got " + s)
	}
	return memory.Ref{ID: id, Global: global}, nil
}

// memoryGlobalPrefix marks a global id in /memory, as in g12.
const memoryGlobalPrefix = "g"

// refLabel is the id of an entry as /memory prints it: 12, or g12 for global.
func refLabel(r memory.Ref) string {
	if r.Global {
		return memoryGlobalPrefix + strconv.FormatInt(r.ID, 10)
	}
	return strconv.FormatInt(r.ID, 10)
}

// listPreamble prints the entries in the prompt and the counts of the agent.
func (m *Model) listPreamble(ctx context.Context) {
	entries, ov, err := m.session.PromptMemory(ctx)
	if err != nil {
		m.append(styleError.Render("error: " + err.Error()))
		return
	}
	m.appendMemoryLines(entries)
	m.append(styleDim.Render(fmt.Sprintf("%d visible · %d summaries · %d pinned · %d need review",
		ov.Visible, ov.Summaries, ov.Pinned, ov.NeedsReview)))
}

// searchMemory prints the entries that match query.
func (m *Model) searchMemory(ctx context.Context, query string) {
	entries, err := m.session.RecallMemory(ctx, query)
	if err != nil {
		m.append(styleError.Render("error: " + err.Error()))
		return
	}
	if len(entries) == 0 {
		m.append(styleDim.Render("no memory matches " + query))
		return
	}
	m.appendMemoryLines(entries)
}

// listReview prints the entries whose review date has passed.
func (m *Model) listReview(ctx context.Context) {
	entries, err := m.session.MemoryReview(ctx)
	if err != nil {
		m.append(styleError.Render("error: " + err.Error()))
		return
	}
	if len(entries) == 0 {
		m.append(styleDim.Render("nothing needs review"))
		return
	}
	m.appendMemoryLines(entries)
}

// memoryAction runs one of the verbs that change or show a single entry.
func (m *Model) memoryAction(ctx context.Context, verb string, ref memory.Ref) {
	var err error
	var done string
	switch verb {
	case "show":
		e, gerr := m.session.MemoryEntry(ctx, ref)
		if gerr != nil {
			m.append(styleError.Render("error: " + gerr.Error()))
			return
		}
		m.append(memoryLine(e, time.Now()))
		m.append(e.Text)
		return
	case "pin":
		err, done = m.session.MemoryPin(ctx, ref, true), "pinned"
	case "unpin":
		err, done = m.session.MemoryPin(ctx, ref, false), "unpinned"
	case "rm":
		err, done = m.session.MemoryForget(ctx, ref), "forgot"
	case "reviewed":
		err, done = m.session.MemoryReviewed(ctx, ref), "reviewed"
	}
	if err != nil {
		m.append(styleError.Render("error: " + err.Error()))
		return
	}
	m.append(styleDim.Render(fmt.Sprintf("%s #%s", done, refLabel(ref))))
}

func (m *Model) appendMemoryLines(entries []memory.Entry) {
	now := time.Now()
	for _, e := range entries {
		m.append(memoryLine(e, now))
	}
}

// memoryLine is "#<id> <kind> <scope> <flags> <age> <title or text>", on one
// line and cut to memoryTextRunes. The id of a global entry is g<id>. Flags: ★
// pinned, ? needs review, ! tainted, G global.
func memoryLine(e memory.Entry, now time.Time) string {
	text := e.Text
	if e.Title != "" {
		text = e.Title
	}
	text = strings.Join(strings.Fields(text), " ")
	if r := []rune(text); len(r) > memoryTextRunes {
		text = string(r[:memoryTextRunes]) + "…"
	}
	ref := memory.Ref{ID: e.ID, Global: e.Scope == memory.ScopeGlobal}
	return fmt.Sprintf("#%-4s %-15s %-6s %-4s %4s  %s", refLabel(ref), e.Kind, e.Scope, memoryFlags(e), memoryAge(now.Sub(e.CreatedAt)), text)
}

// memoryFlags is the marks of an entry, in a fixed order.
func memoryFlags(e memory.Entry) string {
	var sb strings.Builder
	if e.Pinned {
		sb.WriteString("★")
	}
	if e.State == memory.StateNeedsReview {
		sb.WriteString("?")
	}
	if e.Tainted {
		sb.WriteString("!")
	}
	if e.Scope == memory.ScopeGlobal {
		sb.WriteString("G")
	}
	return sb.String()
}

// memoryAge is a short age: minutes, hours, then days.
func memoryAge(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}
