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

// askTo sends text to the agent that token names, and leaves the screen where
// it is. The answer comes back through the workspace's ask hook. A busy agent
// reads the ask when its turn ends, and the screen says so. It reports false,
// after a note or an error, when the ask is not sent.
func (m *Model) askTo(token, text string) bool {
	addr, res, matches := m.resolveAddress(token)
	if !m.addressNote(token, res, matches) {
		return false
	}
	if note := m.mentionNote(text); note != "" {
		text += "\n" + note
	}
	key := addr.String()
	if busy, _ := m.keyState(key); busy {
		m.append(styleDim.Render(key + " is working; the ask waits"))
	}
	if err := m.ws.Ask(m.key, key, text); err != nil {
		m.append(styleError.Render("error: " + err.Error()))
		return false
	}
	m.asked[key] = true
	m.append(styleDim.Render("asked " + key))
	return true
}

// routeTo makes the agent that token names the conversation on screen. It
// switches the space when the agent lives in another one, then selects the
// agent through the sidebar's path. It reports false, after a note, when the
// token names no agent or names one in several spaces.
func (m *Model) routeTo(token string) bool {
	addr, res, matches := m.resolveAddress(token)
	if !m.addressNote(token, res, matches) {
		return false
	}
	if addr.space != m.activeSpaceName() {
		m.useSpace(addr.space)
	}
	m.selectAgent(addr.agent)
	return true
}

// addressNote shows the note for a token that names no agent or several, and
// reports false. A token that resolves reports true and shows nothing.
func (m *Model) addressNote(token string, res resolution, matches []agentAddress) bool {
	switch res {
	case unknownAgent:
		m.append(styleDim.Render(m.unknownAgentNote(token)))
		return false
	case ambiguousAgent:
		m.append(styleDim.Render(m.ambiguousAgentNote(token, matches)))
		return false
	}
	return true
}

// answerAsk shows the answer to an operator ask. On the screen the ask was sent
// from, the answer is a block headed by the agent. Anywhere else the operator
// gets one line. The answer itself stays in the agent's transcript, which the
// turn's answer message fills.
func (m *Model) answerAsk(msg askMsg) {
	m.announced[msg.key] = true
	if msg.origin != m.key {
		m.append(styleDim.Render(m.answerLine(msg.key, msg.text, msg.err)))
		return
	}
	// The answer already reads on this screen when the agent is the screen.
	if msg.key == m.key {
		return
	}
	m.append(styleDim.Render(msg.key + ":"))
	if msg.err != nil {
		m.append(styleError.Render("error: " + msg.err.Error()))
		return
	}
	if msg.text != "" {
		m.addMarkdown(msg.text)
		m.refresh()
	}
}

// noteAnswer adds one line for the end of a turn on another screen, when the
// operator asked that agent or the agent mentions the operator. An answer that
// askMsg already showed is not repeated. It runs before the answer is routed to
// its conversation, so the line lands on the screen.
func (m *Model) noteAnswer(msg answerMsg) {
	shown := m.announced[msg.key]
	delete(m.announced, msg.key)
	if shown || msg.key == "" || msg.key == m.ws.ConversationKey() || msg.text == "" {
		return
	}
	if !m.asked[msg.key] && !mentionsName(msg.text, m.ws.OperatorName()) {
		return
	}
	m.append(styleDim.Render(m.answerLine(msg.key, msg.text, nil)))
}

// answerLine is the one line that stands for an answer: the agent, then the
// first line of the answer (or of the error), cut to the width of the chat.
func (m Model) answerLine(key, text string, err error) string {
	body := text
	if err != nil {
		body = "error: " + err.Error()
	}
	first := strings.TrimSpace(strings.SplitN(body, "\n", 2)[0])
	return truncateCells(key+": "+first, m.contentWidth())
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
