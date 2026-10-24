package ui

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// completionMax is the most rows the completion list shows.
const completionMax = 6

// completionItem is one row of the completion list.
type completionItem struct {
	usage  string // as /help prints it, e.g. "/space new <name> [path]"
	insert string // the first line once completed, without the trailing space
	help   string
}

// firstToken is the command word of a usage line: "/space" for "/space new <name>".
func firstToken(usage string) string {
	token, _, _ := strings.Cut(usage, " ")
	return token
}

// subWord is the second-level word of a usage line, or "" when the second
// word is an argument or missing: "new" for "/space new <name>", "" for
// "/model [name]" and "/tasks [space]".
func subWord(usage string) string {
	parts := strings.Fields(usage)
	if len(parts) < 2 {
		return ""
	}
	for _, r := range parts[1] {
		if r < 'a' || r > 'z' {
			return ""
		}
	}
	return parts[1]
}

// helpFor returns the description of the first help entry whose command word
// is token, or "" when there is none.
func helpFor(token string) (helpEntry, bool) {
	for _, e := range helpEntries {
		if firstToken(e.usage) == token {
			return e, true
		}
	}
	return helpEntry{}, false
}

// completionOpen reports whether the completion list is on screen. It shows
// only while the first input line starts with "/", the cursor is on that line,
// no approval or paste box is up, and the input has focus.
func (m Model) completionOpen() bool { return len(m.completionCandidates()) > 0 }

// completionRows is how many rows the open list takes; zero while it is closed.
func (m Model) completionRows() int { return len(m.completionCandidates()) }

// completionCandidates returns the rows the list shows for the current input:
// command names while the first word is typed, sub-commands once a known
// command is followed by a space and a word without arguments yet.
func (m Model) completionCandidates() []completionItem {
	if m.pending != nil || m.focusSidebar || m.focusTerm || len(m.pastes) > 0 || !m.onFirstInputLine() {
		return nil
	}
	// A list dismissed with esc stays closed until the input changes.
	if m.input.Value() == m.compDismissed {
		return nil
	}
	line := m.firstLine()
	if !strings.HasPrefix(line, "/") {
		return nil
	}
	word, rest, spaced := strings.Cut(line[1:], " ")
	if !spaced {
		return m.commandNameItems(strings.ToLower(word))
	}
	if strings.Contains(rest, " ") {
		return nil
	}
	return subCommandItems(word, strings.ToLower(rest))
}

// firstLine is the input up to its first newline.
func (m Model) firstLine() string {
	line, _, _ := strings.Cut(m.input.Value(), "\n")
	return line
}

// commandNameItems lists the known commands whose name starts with prefix,
// in alphabetical order, at most completionMax.
func (m Model) commandNameItems(prefix string) []completionItem {
	names := make([]string, 0, len(knownCommands))
	for name := range knownCommands {
		if strings.HasPrefix(name, prefix) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	var out []completionItem
	for _, name := range names {
		e, ok := helpFor("/" + name)
		if !ok {
			continue
		}
		out = append(out, completionItem{usage: "/" + name, insert: "/" + name, help: e.description})
		if len(out) == completionMax {
			break
		}
	}
	return out
}

// subCommandItems lists the second-level commands of command whose word starts
// with prefix, in the order /help prints them, at most completionMax.
func subCommandItems(command, prefix string) []completionItem {
	if !knownCommands[command] {
		return nil
	}
	var out []completionItem
	for _, e := range helpEntries {
		word := subWord(e.usage)
		if firstToken(e.usage) != "/"+command || word == "" || !strings.HasPrefix(word, prefix) {
			continue
		}
		out = append(out, completionItem{usage: e.usage, insert: "/" + command + " " + word, help: e.description})
		if len(out) == completionMax {
			break
		}
	}
	return out
}

// moveCompletion moves the highlight of the open list by step, clamped to its rows.
func (m *Model) moveCompletion(step int) {
	n := m.completionRows()
	m.compSel = min(max(m.compSel+step, 0), max(n-1, 0))
}

// completeTo replaces the first input line with the item's text and a trailing
// space, and leaves the cursor at the end of that line. Other lines stay.
func (m Model) completeTo(item completionItem) Model {
	rest := ""
	if _, after, found := strings.Cut(m.input.Value(), "\n"); found {
		rest = "\n" + after
	}
	m.input.SetValue(item.insert + " " + rest)
	for m.input.Line() > 0 {
		m.input.CursorUp()
	}
	m.input.CursorEnd()
	m.compSel = 0
	m.compDismissed = ""
	return m
}

// completionKey handles the keys that the open list takes: tab completes the
// highlighted row, up and down move the highlight, esc closes the list, and
// enter completes when the typed prefix is not yet a whole command and one row
// remains. It reports false for every other key, which goes to the input.
func (m Model) completionKey(msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	items := m.completionCandidates()
	if len(items) == 0 {
		return m, nil, false
	}
	switch msg.Type {
	case tea.KeyTab:
		return m.completeTo(items[min(m.compSel, len(items)-1)]), nil, true
	case tea.KeyUp:
		m.moveCompletion(-1)
		return m, nil, true
	case tea.KeyDown:
		m.moveCompletion(1)
		return m, nil, true
	case tea.KeyEsc:
		m.compDismissed = m.input.Value()
		m.compSel = 0
		return m, nil, true
	case tea.KeyEnter:
		if isNewlineKey(msg) {
			return m, nil, false
		}
		if len(items) == 1 && strings.TrimSpace(m.firstLine()) != items[0].insert {
			return m.completeTo(items[0]), nil, true
		}
	}
	return m, nil, false
}

// completionLines renders the open list: one row per candidate, the usage and
// its help, truncated to the chat width. The highlighted row is in accent.
func (m Model) completionLines() []string {
	items := m.completionCandidates()
	sel := min(m.compSel, max(len(items)-1, 0))
	width := max(m.chatWidth()-2, 4)
	lines := make([]string, 0, len(items))
	for i, it := range items {
		text := it.usage
		if it.help != "" {
			text = fmt.Sprintf("%-26s %s", it.usage, it.help)
		}
		text = truncateCells(text, width)
		if i == sel {
			lines = append(lines, "  "+styleAccentBold.Render(text))
		} else {
			lines = append(lines, "  "+styleDim.Render(text))
		}
	}
	return lines
}
