package gate

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"sync"

	"github.com/tinchoram/groot-cli/internal/tool"
)

// WorkConfig configures the work mode of one conversation.
type WorkConfig struct {
	// Space is the directory a write must stay inside to be approved.
	Space string
	// Ask lists the bash commands that always go to the user, as regular expressions.
	Ask []string
	// MaxTurns limits the auto-approved calls per task. 0 means no limit.
	MaxTurns int
	// MaxUSD limits the spend of the session. 0 means no limit.
	MaxUSD float64
	// Cost reports the spend of the session so far.
	Cost func() float64
	// Warn receives the messages that turn work mode off.
	Warn func(msg string)
}

// WorkState is the work mode of a conversation: whether it is on, and the
// counters of its limits. It is safe for concurrent use.
type WorkState struct {
	cfg  WorkConfig
	ask  []*regexp.Regexp
	mu   sync.Mutex
	on   bool
	used int
}

// NewWorkState compiles the configuration. It refuses a broken pattern.
func NewWorkState(cfg WorkConfig) (*WorkState, error) {
	s := &WorkState{cfg: cfg}
	for _, p := range cfg.Ask {
		re, err := regexp.Compile(p)
		if err != nil {
			return nil, fmt.Errorf("work.always_ask %q: %w", p, err)
		}
		s.ask = append(s.ask, re)
	}
	return s, nil
}

// On reports whether work mode is on.
func (s *WorkState) On() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.on
}

// Set turns work mode on or off.
func (s *WorkState) Set(on bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.on = on
}

// Would reports whether work mode would approve the call now, without counting
// it or turning work mode off. The transcript uses it to label auto-approved calls.
func (s *WorkState) Would(req Request) bool {
	s.mu.Lock()
	on, used := s.on, s.used
	s.mu.Unlock()
	if !on {
		return false
	}
	if s.cfg.MaxTurns > 0 && used >= s.cfg.MaxTurns {
		return false
	}
	if s.cfg.MaxUSD > 0 && s.cfg.Cost != nil && s.cfg.Cost() >= s.cfg.MaxUSD {
		return false
	}
	return s.approves(req)
}

// Limits reports the turn and spend limits of the work mode. Zero means none.
func (s *WorkState) Limits() (turns int, usd float64) {
	return s.cfg.MaxTurns, s.cfg.MaxUSD
}

// ResetTurns starts the turn budget of a new task.
func (s *WorkState) ResetTurns() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.used = 0
}

// asks reports whether a bash command matches one of the always-ask patterns.
func (s *WorkState) asks(command string) bool {
	for _, re := range s.ask {
		if re.MatchString(command) {
			return true
		}
	}
	return false
}

// Work approves, in work mode, the calls that need no question: writes inside
// the space, bash commands that do not match the always-ask list, and the
// task_open call. Everything else goes to Inner, the user's gate. It sits
// under the flow gate, so the flow rules still apply first.
type Work struct {
	Inner Gate
	State *WorkState
}

// Decide approves the calls work mode covers and leaves the rest to Inner.
func (w Work) Decide(ctx context.Context, req Request) Decision {
	s := w.State
	if s == nil || !s.On() {
		return w.Inner.Decide(ctx, req)
	}
	if s.overLimit() {
		s.Set(false)
		return w.Inner.Decide(ctx, req)
	}
	if !s.approves(req) {
		return w.Inner.Decide(ctx, req)
	}
	s.mu.Lock()
	s.used++
	s.mu.Unlock()
	return Allow
}

// overLimit turns work mode off when a limit is reached, and says why.
func (s *WorkState) overLimit() bool {
	s.mu.Lock()
	turns, max := s.used, s.cfg.MaxTurns
	s.mu.Unlock()
	switch {
	case max > 0 && turns >= max:
		s.warnOff("work mode off · limit reached (turns)")
		return true
	case s.cfg.MaxUSD > 0 && s.cfg.Cost != nil && s.cfg.Cost() >= s.cfg.MaxUSD:
		s.warnOff("work mode off · limit reached (budget)")
		return true
	}
	return false
}

func (s *WorkState) warnOff(msg string) {
	s.Set(false)
	if s.cfg.Warn != nil {
		s.cfg.Warn(msg)
	}
}

// approves reports whether work mode covers this call.
func (s *WorkState) approves(req Request) bool {
	if adminChanges[req.Tool] || req.Tainted {
		return false
	}
	switch req.Tool {
	case "write_file":
		return insideSpace(s.cfg.Space, inputField(req.Input, "path"))
	case "bash":
		return !s.asks(inputField(req.Input, "command"))
	}
	return true
}

// adminChanges are the tools that change the catalog or the spaces. Work mode
// never approves them: they keep asking the user.
var adminChanges = map[string]bool{
	"create_space": true, "assign_agent": true, "unassign_agent": true,
	"create_agent": true, "import_agent": true, "install_agent": true,
	"upgrade_agent": true, "set_work_mode": true, "set_agent_model": true,
	"set_space_route": true, "set_space_model": true, "set_space_delegation": true, "set_space_budget": true, "set_agent_budget": true, "set_context_window": true,
	"grant_tool": true, "revoke_tool": true, "grant_global_memory": true, "revoke_global_memory": true, "set_default_model": true,
	"delete_space": true, "edit_space": true, "delete_agent": true, "edit_agent": true, "move_agent": true, "copy_agent": true,
}

// insideSpace reports whether path, relative to space or absolute, is inside it.
func insideSpace(space, path string) bool {
	if path == "" || space == "" {
		return false
	}
	target := path
	if !filepath.IsAbs(path) {
		target = filepath.Join(space, path)
	}
	rel, err := filepath.Rel(space, target)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func inputField(input, field string) string {
	var m map[string]any
	if err := json.Unmarshal([]byte(input), &m); err != nil {
		return ""
	}
	s, _ := m[field].(string)
	return s
}

// the tool package is imported for its risk levels, which the tests use.
var _ = tool.RiskRead
