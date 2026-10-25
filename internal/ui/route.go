package ui

import "strings"

// leadingAddress returns the @address a message starts with, and the message
// after it. ok is false when the message does not start with an address, or when
// the address is the operator's own name, which is plain text.
func (m Model) leadingAddress(text string) (token, rest string, ok bool) {
	if !strings.HasPrefix(text, "@") {
		return "", text, false
	}
	token, rest = text[1:], ""
	if i := strings.IndexAny(token, " \t\n"); i >= 0 {
		token, rest = token[:i], token[i:]
	}
	if !addressRule.MatchString(token) {
		return "", text, false
	}
	if op := m.ws.OperatorName(); op != "" && strings.EqualFold(token, op) {
		return "", text, false
	}
	return token, strings.TrimSpace(rest), true
}

// routeTo makes the agent that token names the conversation on screen. It
// switches the space when the agent lives in another one, then selects the
// agent through the sidebar's path. It reports false, after a note, when the
// token names no agent or names one in several spaces.
func (m *Model) routeTo(token string) bool {
	addr, res, matches := m.resolveAddress(token)
	switch res {
	case unknownAgent:
		m.append(styleDim.Render(m.unknownAgentNote(token)))
		return false
	case ambiguousAgent:
		m.append(styleDim.Render(m.ambiguousAgentNote(token, matches)))
		return false
	}
	if addr.space != m.activeSpaceName() {
		m.useSpace(addr.space)
	}
	m.selectAgent(addr.agent)
	return true
}

// saveOperator stores the name typed for /me or the onboarding prompt. On
// success it confirms in the transcript. On failure it shows the error and, when
// onboarding, puts the typed text back so it can be corrected.
func (m Model) saveOperator(raw string, onboarding bool) Model {
	name, err := m.ws.SetOperatorName(raw)
	if err != nil {
		m.append(styleError.Render("error: " + err.Error()))
		if onboarding {
			m.input.SetValue(raw)
		}
		return m
	}
	if onboarding {
		m.onboard = false
		m.input.Placeholder = ""
	}
	m.append(styleDim.Render("you are @" + name))
	return m
}
