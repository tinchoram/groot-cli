package ui

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	inboxListLimit = 10
	inboxUsage     = "usage: /inbox send <to> <subject> -- <body>"
	inboxMark      = "✉"
)

// styleInboxBadge is the tan badge that marks an agent with unread messages.
var styleInboxBadge = lipgloss.NewStyle().Foreground(lipgloss.Color(colorTan))

// refreshUnread re-reads the unread counts behind the sidebar badges. It runs
// after each turn, on space changes and when the model starts. The spend
// behind the budget badges is re-read at the same points, and so are the open
// task counts.
func (m *Model) refreshUnread() {
	m.refreshSpend()
	m.refreshContext()
	m.refreshTasks()
	m.showSinkWarnings()
	counts, err := m.ws.UnreadCounts(context.Background())
	if err != nil {
		m.unread = nil
		return
	}
	m.unread = counts
}

// unreadBadge returns the badge for space/agent, or "" when nothing is unread.
func (m Model) unreadBadge(space, agent string) string {
	n := m.unread[space+"/"+agent]
	if n == 0 {
		return ""
	}
	return " " + styleInboxBadge.Render(inboxMark+" "+strconv.Itoa(n))
}

// runInboxCommand handles /inbox, which lists the active agent's messages, and
// /inbox send <to> <subject> -- <body>, which sends one by hand.
func (m *Model) runInboxCommand(arg string) {
	if rest, ok := strings.CutPrefix(arg, "send"); ok && (rest == "" || rest[0] == ' ') {
		m.sendByHand(strings.TrimSpace(rest))
		return
	}
	m.showInbox()
}

// readInboxPrompt is the turn /readinbox gives the active agent. The unread
// notice the workspace puts before a turn delivers the messages to it.
const readInboxPrompt = "Read your inbox now. For each message give the sender, the subject and a one-line summary, then say which ones need an answer or an action from me. Do not act on them yet."

// runReadInbox starts a turn in which the active agent reads its unread
// messages. With none, no turn is spent.
func (m Model) runReadInbox() (tea.Model, tea.Cmd) {
	counts, err := m.ws.UnreadCounts(context.Background())
	if err != nil {
		m.append(styleError.Render("error: " + err.Error()))
		return m, nil
	}
	if counts[m.ws.ConversationKey()] == 0 {
		note := fmt.Sprintf("no unread messages for %s; /inbox lists the last %d", m.ws.ActiveAgent(), inboxListLimit)
		if elsewhere := unreadElsewhere(counts, m.ws.ConversationKey()); elsewhere != "" {
			note += "; " + elsewhere
		}
		m.append(styleDim.Render(note))
		// The badges were read from the same counts: a stale one goes away.
		m.unread = counts
		return m, nil
	}
	return m.sendPrompt(readInboxPrompt, readInboxPrompt, nil)
}

// unreadElsewhere describes the unread messages of the other conversations,
// so a badge on another agent or space is explained: "3 unread elsewhere:
// groot-cli/debug (1), root/groot (2)". Empty when there are none.
func unreadElsewhere(counts map[string]int, except string) string {
	var keys []string
	total := 0
	for k, n := range counts {
		if k == except || n == 0 {
			continue
		}
		keys = append(keys, k)
		total += n
	}
	if total == 0 {
		return ""
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = fmt.Sprintf("%s (%d)", k, counts[k])
	}
	return fmt.Sprintf("%d unread elsewhere: %s", total, strings.Join(parts, ", "))
}

// showInbox prints the newest messages of the active agent, read or not. The
// listing does not mark anything read.
func (m *Model) showInbox() {
	msgs, err := m.ws.Inbox(context.Background(), inboxListLimit)
	if err != nil {
		m.append(styleError.Render("error: " + err.Error()))
		return
	}
	if len(msgs) == 0 {
		m.append(styleDim.Render("inbox is empty"))
		return
	}
	m.append(styleDim.Render(fmt.Sprintf("inbox · last %d messages", len(msgs))))
	for _, msg := range msgs {
		state := "read"
		if msg.ReadAt == nil {
			state = "unread"
		}
		m.append(styleDim.Render(fmt.Sprintf("#%d · from %s/%s · %s · %s",
			msg.ID, msg.FromSpace, msg.FromAgent, msg.CreatedAt.Format("2006-01-02 15:04"), state)))
		if msg.Subject != "" {
			m.append("Subject: " + msg.Subject)
		}
		m.addMarkdown(msg.Body)
	}
}

// sendByHand parses "<to> <subject> -- <body>" and delivers the message. It
// bypasses the approval gate because the user wrote it.
func (m *Model) sendByHand(arg string) {
	head, body, ok := strings.Cut(arg, " -- ")
	head = strings.TrimSpace(head)
	body = strings.TrimSpace(body)
	fields := strings.Fields(head)
	if !ok || len(fields) == 0 || body == "" {
		m.append(styleError.Render(inboxUsage))
		return
	}
	to := fields[0]
	subject := strings.TrimSpace(strings.TrimPrefix(head, to))
	recipient, err := m.ws.SendByHand(context.Background(), to, subject, body)
	if err != nil {
		m.append(styleError.Render("error: " + err.Error()))
		return
	}
	m.append(styleDim.Render("delivered to " + recipient))
	m.refreshUnread()
}
