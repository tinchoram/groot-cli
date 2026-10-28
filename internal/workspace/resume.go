package workspace

import (
	"context"
	"errors"

	"fmt"
	"github.com/tinchoram/groot-cli/internal/jobs"
	"time"

	"github.com/tinchoram/groot-cli/internal/agent"
	"github.com/tinchoram/groot-cli/internal/api"
	"github.com/tinchoram/groot-cli/internal/compact"
	"github.com/tinchoram/groot-cli/internal/config"
	"github.com/tinchoram/groot-cli/internal/memory"
)

// transcriptCap is how many stored messages a resumed transcript shows.
const transcriptCap = 200

// Resumed describes a conversation that continued a stored session. The UI
// draws Messages and the header line; the agent already holds the history.
type Resumed struct {
	ID           string
	Count        int           // stored messages in the session
	LastActivity time.Time     // when the session ended, or started if it did not end
	Messages     []api.Message // the transcript: the last transcriptCap stored messages
	Times        []time.Time   // when each transcript message was written; zero when unknown
	Started      time.Time     // when the session started: the fallback time of old messages
	// Dropped are the stored messages older than the transcript, cut from the
	// context by the summarize strategy. The conversation summarizes them before
	// its first model call.
	Dropped []api.Message
}

// startOrResume gives the agent its conversation: the latest stored session of
// the agent when it can be continued, otherwise a new session. It returns what
// the transcript needs when a session was resumed.
func (w *Workspace) startOrResume(ctx context.Context, ag *agent.Agent, store memory.Store, agentName string) *Resumed {
	info, err := w.resume(ctx, ag, store, agentName)
	if err != nil {
		if ag.OnWarning != nil {
			ag.OnWarning(fmt.Sprintf("resume %s: %v; starting a new session", agentName, err))
		}
	}
	if info != nil {
		return info
	}
	ag.Start(ctx)
	return nil
}

// resume continues the latest session of the agent, unless it was cleared or
// holds no messages. It returns nil when nothing was resumed.
func (w *Workspace) resume(ctx context.Context, ag *agent.Agent, store memory.Store, agentName string) (*Resumed, error) {
	sess, ok, err := store.LatestSession(ctx, agentName)
	if err != nil {
		return nil, err
	}
	if !ok || sess.Cleared || sess.Messages == 0 {
		return nil, nil
	}
	msgs, err := store.SessionMessages(ctx, sess.ID)
	if err != nil {
		return nil, err
	}
	ag.Resume(sess.ID, restoreHistory(msgs, w.opts.Config.Compaction), sess.Turns, sess.CostUSD)
	ag.RestoreContext(sess.ContextTokens)
	last := sess.EndedAt
	if last.IsZero() {
		last = sess.StartedAt
	}
	times, err := store.SessionTimes(ctx, sess.ID)
	if err != nil {
		return nil, err
	}
	tail := transcriptTail(msgs)
	tailTimes := make([]time.Time, len(tail))
	if len(times) == len(msgs) {
		copy(tailTimes, times[len(msgs)-len(tail):])
	}
	if n := len(times); n > 0 && !times[n-1].IsZero() {
		last = times[n-1]
	}
	return &Resumed{
		ID: sess.ID, Count: sess.Messages, LastActivity: last, Messages: tail, Times: tailTimes, Started: sess.StartedAt,
		Dropped: droppedMessages(msgs, w.opts.Config.Compaction),
	}, nil
}

// droppedMessages returns the stored messages that restoreHistory leaves out
// under the summarize strategy, the older ones of the transcript cap. The window
// strategy drops messages by design, and none keeps all of them.
func droppedMessages(msgs []api.Message, c config.CompactionConfig) []api.Message {
	if c.Strategy != "summarize" {
		return nil
	}
	kept := transcriptTail(msgs)
	if len(kept) == len(msgs) {
		return nil
	}
	return msgs[:len(msgs)-len(kept)]
}

// carryOver puts the messages a resume left out of the context in front of the
// history. They are summarized with the compaction rules, in one model call
// bounded by the summary timeout. When the call fails, or answers nothing, a note
// says how many messages are out of context instead.
func (c *Conversation) carryOver(ctx context.Context) {
	older := c.carried
	c.carried = nil
	sctx, cancel := context.WithTimeout(ctx, summaryTimeout)
	defer cancel()
	summary, err := compact.Digest(sctx, c.prov, older)
	if err != nil {
		c.warn(fmt.Sprintf("summary of the %d earlier messages failed: %v", len(older), err))
	}
	var head []api.Message
	if err == nil && summary != "" {
		head = carriedPair(fmt.Sprintf("Summary of the %d earlier messages of this session:\n%s", len(older), summary))
	} else {
		head = carriedPair(fmt.Sprintf("%d earlier messages are not in context.", len(older)))
	}
	c.Agent.PrependHistory(head)
}

// carriedPair is a user message with the model's answer that acknowledges it,
// so the history keeps its alternation.
func carriedPair(user string) []api.Message {
	return []api.Message{
		{Role: api.RoleUser, Content: []api.Block{{Type: api.BlockText, Text: user}}},
		{Role: api.RoleAssistant, Content: []api.Block{{Type: api.BlockText, Text: "Understood. I will use that context."}}},
	}
}

// restoreHistory returns the messages the model gets when a session resumes,
// as the compaction strategy permits. Without compaction everything is kept.
// The window strategy keeps its Keep most recent messages. Summarize would
// call the model to shrink the history, so the resume keeps the transcript cap
// and lets the next turn compact it. Every cut is moved to a plain user
// message, so a tool_use is never separated from its tool_result.
func restoreHistory(msgs []api.Message, c config.CompactionConfig) []api.Message {
	switch c.Strategy {
	case "window":
		if c.Keep > 0 && len(msgs) > c.Keep {
			if cut := compact.SafeSplitPoint(msgs, len(msgs)-c.Keep); cut > 0 {
				return msgs[cut:]
			}
		}
		return msgs
	case "summarize":
		return transcriptTail(msgs)
	}
	return msgs
}

// transcriptTail returns the last transcriptCap messages, cut at a plain user
// message when one exists, so the transcript starts with a user turn.
func transcriptTail(msgs []api.Message) []api.Message {
	if len(msgs) <= transcriptCap {
		return msgs
	}
	desired := len(msgs) - transcriptCap
	if cut := compact.SafeSplitPoint(msgs, desired); cut > 0 {
		return msgs[cut:]
	}
	return msgs[desired:]
}

// Resumed reports the stored session this conversation continued, if any.
func (c *Conversation) Resumed() (Resumed, bool) {
	if c.resumed == nil {
		return Resumed{}, false
	}
	return *c.resumed, true
}

// Clear starts a fresh conversation for /clear. The old session is detached in
// memory right away, so the next Send opens a new one, and the prompt is rebuilt
// from the memory stored so far. The returned function finishes the old session:
// it writes the summary, records the end and marks the session cleared. The
// summary reaches the next turn with the memory block, which is sent in full
// again. The caller runs it off the UI loop.
func (c *Conversation) Clear() func(context.Context) error {
	// The session's own spend, not the provider's total: the provider keeps
	// counting across the sessions of this conversation.
	cost := c.Agent.SessionCost()
	// The local summary is taken before the detach, while the exchange is still
	// in memory; it is written only when the model summary fails.
	local := c.localSummary()
	id, transcript, n := c.Agent.Detach()
	c.resumed = nil
	c.carried = nil
	c.lastGoal = ""
	// The next turn carries the whole memory again, the summary of this session included.
	c.memShown.Reset()
	// The automatic turn count belongs to the session, so the new one starts at zero.
	c.autoTurns.Store(0)
	c.capWarned.Store(false)
	// The jobs of the cleared session stop now; the manager stays for the next one.
	running := c.runningJobIDs()
	return func(ctx context.Context) error {
		c.stopJobIDs(running, jobs.StopByClear)
		var errs []error
		if id != "" {
			if n > 0 {
				if err := c.summarize(ctx, id, transcript); err != nil {
					errs = append(errs, c.keepLocalSummary(ctx, id, local, err))
				}
			}
			if err := c.store.EndSession(ctx, id, cost); err != nil {
				errs = append(errs, err)
			}
			if err := c.store.MarkCleared(ctx, id); err != nil {
				errs = append(errs, err)
			}
		}
		return errors.Join(errs...)
	}
}
