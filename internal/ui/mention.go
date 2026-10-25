package ui

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// An @mention names an agent ("@devjunior") or an agent in a space
// ("@explore/search-specialist"). The operator's name and "groot" are the two
// names the transcript highlights.

// addressRule is a whole address: a name, or space/name.
var addressRule = regexp.MustCompile(`^[A-Za-z0-9_-]+(/[A-Za-z0-9_-]+)?$`)

// mentionMax is the most rows the @ list shows.
const mentionMax = 8

// mentionSpan is one @token in a text: the byte range of "@token" and the token
// itself, without the @. A token with a slash is an address.
type mentionSpan struct {
	start, end int
	token      string
}

// findMentions lists the @tokens of s in order. It skips SGR and other ANSI
// escape sequences, so styled text is read the same as plain text. An @ counts
// only at the start of a word: "me@host" and "explore/@groot" are not mentions.
func findMentions(s string) []mentionSpan {
	var out []mentionSpan
	prev := ' '
	for i := 0; i < len(s); {
		if s[i] == 0x1b {
			i = skipEscape(s, i)
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == '@' && !mentionLead(prev) {
			if end := scanAddress(s, i+1); end > i+1 {
				out = append(out, mentionSpan{start: i, end: end, token: s[i+1 : end]})
				prev = rune(s[end-1])
				i = end
				continue
			}
		}
		prev = r
		i += size
	}
	return out
}

// skipEscape returns the index after the escape sequence that starts at i.
func skipEscape(s string, i int) int {
	j := i + 1
	if j < len(s) && s[j] == '[' {
		j++
		for j < len(s) && (s[j] < 0x40 || s[j] > 0x7e) {
			j++
		}
		return min(j+1, len(s))
	}
	return min(i+2, len(s))
}

// mentionLead reports whether a rune before an @ keeps it from being a mention.
func mentionLead(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' || r == '.' || r == '@' || r == '/'
}

// scanAddress returns the end of the address that starts at i, or i when there
// is none: a name, optionally followed by / and a second name.
func scanAddress(s string, i int) int {
	end := scanName(s, i)
	if end == i {
		return i
	}
	if end < len(s) && s[end] == '/' {
		if second := scanName(s, end+1); second > end+1 {
			return second
		}
	}
	return end
}

func scanName(s string, i int) int {
	for i < len(s) && isNameByte(s[i]) {
		i++
	}
	return i
}

func isNameByte(b byte) bool {
	return b == '_' || b == '-' || ('0' <= b && b <= '9') || ('a' <= b && b <= 'z') || ('A' <= b && b <= 'Z')
}

// mentionsName reports whether s mentions name as a whole word, not as an address.
func mentionsName(s, name string) bool {
	if name == "" {
		return false
	}
	for _, sp := range findMentions(s) {
		if !strings.Contains(sp.token, "/") && strings.EqualFold(sp.token, name) {
			return true
		}
	}
	return false
}

// highlightMentions styles the mentions of the operator and of groot in line in
// the accent style. The text itself does not change, and the style codes that
// line already carries are kept.
func highlightMentions(line, operator string) string {
	var sb strings.Builder
	last := 0
	for _, sp := range findMentions(line) {
		if strings.Contains(sp.token, "/") {
			continue
		}
		if !strings.EqualFold(sp.token, "groot") && (operator == "" || !strings.EqualFold(sp.token, operator)) {
			continue
		}
		sb.WriteString(line[last:sp.start])
		sb.WriteString(styleAccentBold.Render(line[sp.start:sp.end]))
		last = sp.end
	}
	if last == 0 {
		return line
	}
	sb.WriteString(line[last:])
	return sb.String()
}

// agentAddress is an agent assigned to a space.
type agentAddress struct {
	space, agent string
}

func (a agentAddress) String() string { return a.space + "/" + a.agent }

// Resolution outcomes of an address typed in the UI.
type resolution int

const (
	resolved resolution = iota
	unknownAgent
	ambiguousAgent
)

// agentAddresses lists every agent assigned to a space, spaces in their order,
// without the operator's own name.
func (m Model) agentAddresses() []agentAddress {
	operator := m.ws.OperatorName()
	var out []agentAddress
	for _, sp := range m.ws.Spaces() {
		for _, name := range sp.Agents {
			if operator != "" && strings.EqualFold(name, operator) {
				continue
			}
			out = append(out, agentAddress{space: sp.Name, agent: name})
		}
	}
	return out
}

// mentionLabels names each address the way the @ list and the notes print it:
// a bare name for an agent of the active space, and space/name otherwise. A name
// that exists in more than one space is always qualified, so no bare homonym
// is printed. The current space's agents come first, then the others in order.
func (m Model) mentionLabels() []agentAddress {
	all := m.agentAddresses()
	active := m.activeSpaceName()
	var ordered []agentAddress
	for _, a := range all {
		if a.space == active {
			ordered = append(ordered, a)
		}
	}
	for _, a := range all {
		if a.space != active {
			ordered = append(ordered, a)
		}
	}
	return ordered
}

// mentionLabel is the text that addresses a, as the lists print it.
func (m Model) mentionLabel(a agentAddress) string {
	all := m.agentAddresses()
	homonym := 0
	for _, o := range all {
		if o.agent == a.agent {
			homonym++
		}
	}
	if a.space == m.activeSpaceName() && homonym == 1 {
		return a.agent
	}
	return a.String()
}

// activeSpaceName is the name of the active space, or "".
func (m Model) activeSpaceName() string {
	sp, _ := m.ws.ActiveSpace()
	return sp.Name
}

// resolveAddress finds the agent a token names. An address with a slash names
// one agent exactly. A bare name prefers the active space; otherwise it must
// name exactly one agent. The matches are returned for the note on ambiguity.
func (m Model) resolveAddress(token string) (agentAddress, resolution, []agentAddress) {
	all := m.agentAddresses()
	if space, agent, ok := strings.Cut(token, "/"); ok {
		for _, a := range all {
			if a.space == space && a.agent == agent {
				return a, resolved, nil
			}
		}
		return agentAddress{}, unknownAgent, nil
	}
	var matches []agentAddress
	for _, a := range all {
		if strings.EqualFold(a.agent, token) {
			matches = append(matches, a)
		}
	}
	switch {
	case len(matches) == 0:
		return agentAddress{}, unknownAgent, nil
	case len(matches) == 1:
		return matches[0], resolved, nil
	}
	for _, a := range matches {
		if a.space == m.activeSpaceName() {
			return a, resolved, nil
		}
	}
	return agentAddress{}, ambiguousAgent, matches
}

// mentionNote is the line appended to a message: the agents it mentions, each
// once, in their full address, so an agent can send_message to them. It is ""
// when the message mentions no agent.
func (m Model) mentionNote(text string) string {
	operator := m.ws.OperatorName()
	seen := map[string]bool{}
	var addrs []string
	for _, sp := range findMentions(text) {
		if operator != "" && strings.EqualFold(sp.token, operator) {
			continue
		}
		a, res, _ := m.resolveAddress(sp.token)
		if res != resolved || seen[a.String()] {
			continue
		}
		seen[a.String()] = true
		addrs = append(addrs, a.String())
	}
	if len(addrs) == 0 {
		return ""
	}
	return "[mentions: " + strings.Join(addrs, ", ") + "]"
}

// agentListing is the list of addresses that unknown-target notes print.
func (m Model) agentListing() string {
	var labels []string
	for _, a := range m.mentionLabels() {
		labels = append(labels, m.mentionLabel(a))
	}
	return strings.Join(labels, ", ")
}

// unknownAgentNote is the note for an @address that names no agent.
func (m Model) unknownAgentNote(token string) string {
	return fmt.Sprintf("no agent @%s; agents: %s", token, m.agentListing())
}

// ambiguousAgentNote is the note for a bare name that several spaces hold.
func (m Model) ambiguousAgentNote(token string, matches []agentAddress) string {
	var names []string
	for _, a := range matches {
		names = append(names, a.String())
	}
	return fmt.Sprintf("@%s is in more than one space: %s; write the space too", token, strings.Join(names, ", "))
}
