package workspace

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/tinchoram/groot-cli/internal/config"
	"github.com/tinchoram/groot-cli/internal/provider"
	"github.com/tinchoram/groot-cli/internal/space"
)

// Context bands: from warnShareContext the context shows as a warning, from
// fullShareContext as full.
const (
	warnShareContext = 70
	fullShareContext = 90
)

// ContextLevel says how full the context of an agent is.
type ContextLevel int

const (
	ContextNone ContextLevel = iota // no context size is known yet
	ContextOK                       // under the warning share
	ContextWarn                     // from the warning share
	ContextFull                     // from the full share
)

// ContextLevelOf places a context of tokens in a window of window tokens.
func ContextLevelOf(tokens int64, window int) ContextLevel {
	if tokens <= 0 || window <= 0 {
		return ContextNone
	}
	switch pct := provider.ContextPercent(tokens, window); {
	case pct >= fullShareContext:
		return ContextFull
	case pct >= warnShareContext:
		return ContextWarn
	}
	return ContextOK
}

// ContextRow is the context of one agent in one space: what its last model
// answer read, the window of its model and the compaction in effect.
type ContextRow struct {
	Space    string
	Agent    string
	Model    string
	Window   int
	Tokens   int64
	Percent  int
	Strategy string
	// Triggers reports whether a model call now would run the token trigger.
	Triggers bool
}

// Contexts returns the context of the agents of the active space, or of every
// space when all is set. An open conversation answers with its live values.
func (w *Workspace) Contexts(ctx context.Context, all bool) ([]ContextRow, error) {
	w.mu.Lock()
	spaces := w.opts.Spaces.List()
	active, hasActive := w.opts.Spaces.Current()
	w.mu.Unlock()
	return w.contextRows(ctx, spaces, func(sp space.Space) bool {
		return all || (hasActive && sp.Name == active.Name)
	})
}

// SpaceContexts returns the context of the agents of one named space.
func (w *Workspace) SpaceContexts(ctx context.Context, name string) ([]ContextRow, error) {
	w.mu.Lock()
	sp, ok := w.opts.Spaces.Get(name)
	w.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("unknown space %q", name)
	}
	return w.contextRows(ctx, []space.Space{sp}, func(space.Space) bool { return true })
}

func (w *Workspace) contextRows(ctx context.Context, spaces []space.Space, want func(space.Space) bool) ([]ContextRow, error) {
	w.mu.Lock()
	cfg := w.opts.Config
	overrides := maps.Clone(cfg.Models.ContextWindow)
	w.mu.Unlock()

	var rows []ContextRow
	for _, sp := range spaces {
		if !want(sp) {
			continue
		}
		// A space whose directory is gone has no ledger; opening its store
		// would recreate the directory.
		if _, err := os.Stat(sp.Path); err != nil {
			continue
		}
		w.mu.Lock()
		st, err := w.storesFor(sp.Path)
		w.mu.Unlock()
		if err != nil {
			return nil, fmt.Errorf("space %s: %w", sp.Name, err)
		}
		for _, agent := range sp.Agents {
			row := ContextRow{Space: sp.Name, Agent: agent, Strategy: cfg.Compaction.Strategy}
			sess, ok, err := st.mem.LatestSession(ctx, agent)
			if err != nil {
				return nil, fmt.Errorf("space %s: %w", sp.Name, err)
			}
			if ok {
				row.Model = sess.Model
				// A cleared session keeps the size of the conversation that
				// /clear dropped: it is no context of the agent any more.
				if !sess.Cleared {
					row.Tokens = sess.ContextTokens
				}
			}
			if live, ok := w.liveConversation(sp.Name, agent); ok {
				row.Model, row.Tokens = live.Model(), live.ContextTokens()
			}
			if row.Model == "" {
				row.Model = w.modelOfAgent(agent)
			}
			row.Window = provider.ContextWindow(row.Model, overrides)
			row.Percent = provider.ContextPercent(row.Tokens, row.Window)
			row.Triggers = row.Tokens > 0 && cfg.Compaction.Strategy != "none" &&
				cfg.Compaction.MaxContextPercent > 0 && row.Percent >= cfg.Compaction.MaxContextPercent
			rows = append(rows, row)
		}
	}
	return rows, nil
}

// liveConversation returns the open conversation of agent in space, if any.
func (w *Workspace) liveConversation(spaceName, agent string) (*Conversation, bool) {
	w.mu.Lock()
	c, ok := w.convs[spaceName+"/"+agent]
	w.mu.Unlock()
	return c, ok
}

// modelOfAgent is the model a conversation of agent would use: the agent's own,
// else provider.model from the configuration, else empty (the window default).
func (w *Workspace) modelOfAgent(agent string) string {
	w.mu.Lock()
	defer w.mu.Unlock()
	if d, ok := w.def(agent); ok && d.Model != "" {
		return d.Model
	}
	return w.opts.Config.Provider.Model
}

// windowFor is the context window of model, with the configured overrides. The
// agents read it before a model call.
func (w *Workspace) windowFor(model string) int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return provider.ContextWindow(model, w.opts.Config.Models.ContextWindow)
}

// SetContextWindow saves the window of a model (or model prefix) to the
// configuration file and applies it to the running conversations.
func (w *Workspace) SetContextWindow(model string, tokens int) error {
	model = strings.TrimSpace(model)
	if model == "" {
		return errors.New("model is empty")
	}
	if tokens <= 0 {
		return errors.New("context window must be a positive number of tokens")
	}
	if err := config.SetContextWindow(w.ConfigFile(), model, tokens); err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.opts.Config.Models.ContextWindow == nil {
		w.opts.Config.Models.ContextWindow = map[string]int{}
	}
	w.opts.Config.Models.ContextWindow[model] = tokens
	return nil
}

// ContextReport is the context table of the named space, or of the active
// space when name is empty, as text.
func (w *Workspace) ContextReport(ctx context.Context, name string) (string, error) {
	var (
		rows []ContextRow
		err  error
	)
	if name == "" {
		rows, err = w.Contexts(ctx, false)
	} else {
		rows, err = w.SpaceContexts(ctx, name)
	}
	if err != nil {
		return "", err
	}
	return FormatContexts(rows), nil
}

// FormatContexts lays out context rows as an aligned table.
func FormatContexts(rows []ContextRow) string {
	if len(rows) == 0 {
		return "no agents in the active space"
	}
	var sb strings.Builder
	tw := tabwriter.NewWriter(&sb, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "SPACE\tAGENT\tMODEL\tWINDOW\tCONTEXT\tUSED\tCOMPACTION\tTRIGGERS NOW")
	for _, r := range rows {
		used, last := "-", "-"
		if r.Tokens > 0 {
			used, last = fmt.Sprintf("%d%%", r.Percent), FormatTokens(r.Tokens)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", r.Space, r.Agent, r.Model,
			FormatTokens(int64(r.Window)), last, used, r.Strategy, triggerWord(r))
	}
	_ = tw.Flush()
	return strings.TrimRight(sb.String(), "\n")
}

func triggerWord(r ContextRow) string {
	switch {
	case r.Triggers:
		return "yes"
	case r.Strategy == "none":
		return "no (strategy none)"
	}
	return "no"
}

// FormatTokens writes a token count as 84k, 200k or 1.0M.
func FormatTokens(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%dk", n/1_000)
	}
	return fmt.Sprint(n)
}
