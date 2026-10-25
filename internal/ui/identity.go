package ui

import (
	"strconv"
	"strings"
)

// onboardingPrompt is the placeholder of the input on the first start, when the
// operator has no name yet.
const onboardingPrompt = "How should agents call you? (one word, e.g. Tinchoram)"

// operatorTag is the banner's line about the operator, dim.
func operatorTag(name string) string {
	if name == "" {
		return ""
	}
	return styleDim.Render("operated by: @" + name)
}

// runMeCommand handles /me: with no argument it shows the name, with one it
// stores the name (with or without the @).
func (m *Model) runMeCommand(arg string) {
	if strings.TrimSpace(arg) == "" {
		if name := m.ws.OperatorName(); name != "" {
			m.append(styleDim.Render("you are @" + name))
		} else {
			m.append(styleDim.Render("no name set: /me <name>"))
		}
		return
	}
	*m = m.saveOperator(arg, false)
}

// needsBadge is the needs-you marker of a conversation: an @ in the badge
// place of the sidebar row, styled like the unread badge, or "" when the
// conversation is not marked.
func (m Model) needsBadge(space, agent string) string {
	if !m.needsYou[space+"/"+agent] {
		return ""
	}
	return " " + styleInboxBadge.Render("@")
}

// needsYouCount is how many conversations other than the one on screen wait
// for the operator.
func (m Model) needsYouCount() int {
	n := 0
	for key, marked := range m.needsYou {
		if marked && key != m.key {
			n++
		}
	}
	return n
}

// needsYouLabel is the status line marker: "@2" when two other conversations
// mention the operator, or "" when none does.
func (m Model) needsYouLabel() string {
	n := m.needsYouCount()
	if n == 0 {
		return ""
	}
	return styleInboxBadge.Render("@" + strconv.Itoa(n))
}
