package workspace

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/tinchoram/groot-cli/internal/memory"
)

// Caps of a local summary: the goal is the last request, the done line is the
// last answer.
const (
	localGoalRunes    = 200
	localSummaryRunes = 300
)

// localSummary is the summary written without a model call. It uses the
// headings of the model summary: Goal is the last request, Done the last
// answer, and the rest is unknown because nothing was asked of the model. It is
// empty when there was no exchange.
func (c *Conversation) localSummary() string {
	user, answer := c.LastExchange()
	if c.lastGoal != "" {
		user = c.lastGoal
	}
	if user == "" && answer == "" {
		return ""
	}
	return fmt.Sprintf("Goal: %s\nDone: %s\nDecisions: none\nNext: unknown (fast close)\nFiles: none",
		cutRunes(strings.TrimSpace(user), localGoalRunes), cutRunes(strings.TrimSpace(answer), localSummaryRunes))
}

// cutRunes keeps the first n runes of s and marks the cut with an ellipsis.
func cutRunes(s string, n int) string {
	if utf8.RuneCountInString(s) > n {
		return string([]rune(s)[:n]) + "…"
	}
	return s
}

// sessionTitle names a session summary by when it was written and by its agent.
func sessionTitle(now time.Time, agent string) string {
	return "Session " + now.Format("2006-01-02 15:04") + " " + agent
}

// storeSummary saves the summary of session id in the agent memory.
func (c *Conversation) storeSummary(ctx context.Context, id, text string) error {
	_, err := c.store.Remember(ctx, memory.Entry{
		Kind:   memory.KindSessionSummary,
		Scope:  memory.ScopeAgent,
		Agent:  c.name,
		Title:  sessionTitle(time.Now(), c.name),
		Text:   text,
		Source: "session:" + id,
	})
	return err
}

// endLocal writes the local summary of the conversation and ends its session,
// without calling the model.
func (c *Conversation) endLocal(ctx context.Context) error {
	if c.SessionID() == "" || c.MessageCount() == 0 {
		return c.Agent.End(ctx)
	}
	var errs []error
	if text := c.localSummary(); text != "" {
		errs = append(errs, c.storeSummary(ctx, c.SessionID(), text))
	}
	errs = append(errs, c.Agent.End(ctx))
	return errors.Join(errs...)
}

// shutdown ends every open conversation and closes the stores. Graceful ends
// them with model summaries, in parallel, bounded by ctx; a conversation whose
// model summary does not finish keeps a local one. Otherwise every conversation
// gets its local summary at once.
func (w *Workspace) shutdown(ctx context.Context, graceful bool) error {
	w.mu.Lock()
	convs := w.convs
	stores := w.stores
	w.convs = map[string]*Conversation{}
	w.stores = map[string]*spaceStores{}
	w.mu.Unlock()

	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		errs []error
	)
	note := func(err error) {
		if err != nil {
			mu.Lock()
			errs = append(errs, err)
			mu.Unlock()
		}
	}
	for _, c := range convs {
		wg.Add(1)
		go func(c *Conversation) {
			defer wg.Done()
			c.stopJobs() // the jobs end first, so their exits reach the audit log
			if !graceful {
				note(c.endLocal(context.Background()))
				return
			}
			if err := c.End(ctx); err != nil {
				// The model did not finish its summary in time: the session is
				// ended, so only the local summary is still missing.
				note(c.writeLocalSummary(context.Background()))
			}
		}(c)
	}
	wg.Wait()

	// The export sinks get the last records before the logs close; the flush is
	// capped, so quitting never waits on a dead collector.
	note(w.closeSinks())

	for _, s := range stores {
		if s.aud != nil {
			note(s.aud.Close())
		}
		if s.mcp != nil {
			note(s.mcp.Close())
		}
		note(s.mem.Close())
	}
	note(w.inbox.Close())
	return errors.Join(errs...)
}

// ErrSummaryFallback marks a session that kept its local summary because the
// model summary failed, a timeout most often. It is a degradation to report
// as a note, not an error: the session is ended and remembered either way.
var ErrSummaryFallback = errors.New("summary fallback")

// keepLocalSummary stores the local summary of session id after the model
// summary failed with cause, and returns the fallback notice. An empty local
// summary (no exchange) leaves nothing to store, and the notice still says the
// model summary was skipped.
func (c *Conversation) keepLocalSummary(ctx context.Context, id, local string, cause error) error {
	if local != "" {
		if err := c.storeSummary(ctx, id, local); err != nil {
			return fmt.Errorf("local summary for %s: %w", c.name, err)
		}
	}
	reason := "the model did not answer"
	if errors.Is(cause, context.DeadlineExceeded) {
		reason = "the model did not answer in time"
	}
	return fmt.Errorf("%w: summary for %s: %s; a local summary was kept", ErrSummaryFallback, c.name, reason)
}

// writeLocalSummary stores the local summary when the conversation has one. It
// is used after a model summary failed, when the session is already ended.
func (c *Conversation) writeLocalSummary(ctx context.Context) error {
	text := c.localSummary()
	if text == "" {
		return nil
	}
	return c.storeSummary(ctx, c.SessionID(), text)
}
