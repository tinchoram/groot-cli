package ui

import (
	"regexp"
	"strings"
)

// caretToken returns the @token the caret is in, when the word before the caret
// starts with @ and holds only address characters. line is the logical line of
// the caret, start the rune index of the @, col the caret's rune index.
func (m Model) caretToken() (line, start, col int, token string, ok bool) {
	lines := strings.Split(m.input.Value(), "\n")
	line = m.input.Line()
	if line < 0 || line >= len(lines) {
		return 0, 0, 0, "", false
	}
	runes := []rune(lines[line])
	li := m.input.LineInfo()
	col = min(max(li.StartColumn+li.ColumnOffset, 0), len(runes))
	start = col
	for start > 0 && !isSpaceRune(runes[start-1]) {
		start--
	}
	word := string(runes[start:col])
	if !strings.HasPrefix(word, "@") {
		return 0, 0, 0, "", false
	}
	token = strings.TrimPrefix(word, "@")
	if !partialAddressRule.MatchString(token) {
		return 0, 0, 0, "", false
	}
	return line, start, col, token, true
}

// partialAddressRule is an address while it is typed: a name, or space/name with
// the second part still empty or unfinished.
var partialAddressRule = regexp.MustCompile(`^[A-Za-z0-9_-]*(/[A-Za-z0-9_-]*)?$`)

func isSpaceRune(r rune) bool { return r == ' ' || r == '\t' || r == '\n' }

// mentionCandidates is the @ list for the caret's word: the addresses whose
// text starts with what follows the @, ignoring case. It is off for a slash
// command, so an @ in a command's text never opens it. ok is false when the
// caret is not on an @word.
func (m Model) mentionCandidates() (items []completionItem, ok bool) {
	if strings.HasPrefix(strings.TrimSpace(m.input.Value()), "/") {
		return nil, false
	}
	_, _, _, token, found := m.caretToken()
	if !found {
		return nil, false
	}
	catalog := map[string]string{}
	for _, a := range m.ws.CatalogAgents() {
		catalog[a.Name] = a.Description
	}
	prefix := strings.ToLower(token)
	for _, a := range m.mentionLabels() {
		label := m.mentionLabel(a)
		// The prefix may match the whole address or the agent's own name, so
		// "rev" offers default/reviewer and explore/reviewer.
		if !strings.HasPrefix(strings.ToLower(label), prefix) && !strings.HasPrefix(strings.ToLower(a.agent), prefix) {
			continue
		}
		items = append(items, completionItem{
			usage:   "@" + label,
			insert:  "@" + label,
			help:    catalog[a.agent],
			mention: true,
		})
		if len(items) == mentionMax {
			break
		}
	}
	return items, true
}

// mentionExact reports whether the caret's word is already a whole address, so
// enter sends the message instead of completing it.
func (m Model) mentionExact() bool {
	_, _, _, token, ok := m.caretToken()
	if !ok {
		return false
	}
	for _, a := range m.mentionLabels() {
		if strings.EqualFold(m.mentionLabel(a), token) {
			return true
		}
	}
	return false
}

// completeMention replaces the caret's @word with the item's address and a
// trailing space, and leaves the caret after that space. The rest of the line
// and the other lines stay.
func (m Model) completeMention(item completionItem) Model {
	line, start, _, _, ok := m.caretToken()
	if !ok {
		return m
	}
	lines := strings.Split(m.input.Value(), "\n")
	runes := []rune(lines[line])
	col := m.caretColumn(runes)
	insert := []rune(item.insert + " ")
	joined := append(append(append([]rune{}, runes[:start]...), insert...), runes[col:]...)
	lines[line] = string(joined)
	m.input.SetValue(strings.Join(lines, "\n"))
	for i := 0; i < len(lines) && m.input.Line() > line; i++ {
		m.input.CursorUp()
	}
	m.input.SetCursor(start + len(insert))
	m.compSel = 0
	m.compDismissed = ""
	return m
}

// caretColumn is the caret's rune index in the logical line runes.
func (m Model) caretColumn(runes []rune) int {
	li := m.input.LineInfo()
	return min(max(li.StartColumn+li.ColumnOffset, 0), len(runes))
}
