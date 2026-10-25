package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/tinchoram/groot-cli/internal/memory"
)

// memoryTextRunes is how much of an entry's text a listing shows.
const memoryTextRunes = 100

// runMemoryCommand handles /memory, which lists the entries in the prompt of the
// active agent, and /memory <query>, which searches the entries it can read.
func (m *Model) runMemoryCommand(arg string) {
	ctx := context.Background()
	if arg != "" {
		entries, err := m.session.RecallMemory(ctx, arg)
		if err != nil {
			m.append(styleError.Render("error: " + err.Error()))
			return
		}
		if len(entries) == 0 {
			m.append(styleDim.Render("no memory matches " + arg))
			return
		}
		m.appendMemoryLines(entries)
		return
	}
	entries, ov, err := m.session.PromptMemory(ctx)
	if err != nil {
		m.append(styleError.Render("error: " + err.Error()))
		return
	}
	m.appendMemoryLines(entries)
	m.append(styleDim.Render(fmt.Sprintf("%d entries visible to this agent · %d session summaries", ov.Visible, ov.Summaries)))
}

func (m *Model) appendMemoryLines(entries []memory.Entry) {
	now := time.Now()
	for _, e := range entries {
		m.append(memoryLine(e, now))
	}
}

// memoryLine is "<id> <kind> <scope> <age> <text>", with the text on one line
// and cut to memoryTextRunes.
func memoryLine(e memory.Entry, now time.Time) string {
	text := strings.Join(strings.Fields(e.Text), " ")
	if r := []rune(text); len(r) > memoryTextRunes {
		text = string(r[:memoryTextRunes]) + "…"
	}
	return fmt.Sprintf("%d %s %s %s %s", e.ID, e.Kind, e.Scope, memoryAge(now.Sub(e.CreatedAt)), text)
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
