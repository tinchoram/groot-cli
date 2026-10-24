package agent

import (
	"strings"

	"github.com/tinchoram/groot-cli/internal/api"
)

// LastExchange returns the newest plain user prompt and the newest assistant
// answer text in the history. Either is "" when the history has none.
func (a *Agent) LastExchange() (user, assistant string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for i := len(a.history) - 1; i >= 0 && (user == "" || assistant == ""); i-- {
		m := a.history[i]
		switch {
		case m.Role == api.RoleUser && user == "" && isPlainText(m):
			user = textOf(m)
		case m.Role == api.RoleAssistant && assistant == "":
			assistant = textOf(m)
		}
	}
	return user, assistant
}

func isPlainText(m api.Message) bool {
	if len(m.Content) == 0 {
		return false
	}
	for _, b := range m.Content {
		if b.Type != api.BlockText {
			return false
		}
	}
	return true
}

func textOf(m api.Message) string {
	var parts []string
	for _, b := range m.Content {
		if b.Type == api.BlockText && strings.TrimSpace(b.Text) != "" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, "\n")
}
