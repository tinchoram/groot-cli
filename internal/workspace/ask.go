package workspace

import (
	"context"
	"errors"
	"sync"
)

// askSubject is the subject of a question the operator asks an agent.
const askSubject = "ask"

// askBook remembers the conversation each operator ask was sent from, by inbox
// id, until the turn that reads the ask. The answer goes back to that origin.
type askBook struct {
	mu     sync.Mutex
	origin map[int64]string
}

func newAskBook() *askBook {
	return &askBook{origin: map[int64]string{}}
}

// take returns the origins of the asks among ids and forgets them: an ask is
// answered once.
func (b *askBook) take(ids []int64) []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []string
	for _, id := range ids {
		if o, ok := b.origin[id]; ok {
			out = append(out, o)
			delete(b.origin, id)
		}
	}
	return out
}

// Ask sends the operator's question to an agent, written as "space/agent". The
// screen stays where it is: origin is the conversation the question was asked
// from. The agent answers on its own turn, and the answer comes through
// Hooks.OnAsk with origin, whether the agent was open or not. A busy agent reads
// the question when its turn ends.
func (w *Workspace) Ask(origin, to, text string) error {
	cur, ok := w.opts.Spaces.Current()
	if !ok {
		return errors.New("no space selected: use /space use <name>")
	}
	// The book is locked across the delivery: a turn that reads the question
	// waits for its origin to be recorded.
	w.asks.mu.Lock()
	defer w.asks.mu.Unlock()
	_, id, err := w.messengerFor(cur.Name, handSender).Send(context.Background(), to, askSubject, text)
	if err != nil {
		return err
	}
	w.asks.origin[id] = origin
	return nil
}
