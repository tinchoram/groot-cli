package memory

import (
	"fmt"
	"slices"
	"strings"
	"sync"
)

// Shown tracks the memory a conversation has been told about. The first block
// of a conversation carries the whole preamble; every later block carries only
// what changed since the previous one. Memory enters the conversation this way,
// so the system prompt never has to change when memory does.
//
// Changes are found by comparing the content of the entries, not their rendered
// lines: a tainted entry renders with a fresh envelope every time, and that must
// not look like a change.
type Shown struct {
	mu     sync.Mutex
	primed bool
	known  map[int64]Entry
}

// Reset forgets what was told. The next Block carries the whole memory again;
// /clear uses it, so a cleared conversation starts with its full memory.
func (s *Shown) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.primed = false
	s.known = nil
}

// Block returns the memory block for the next turn, given the entries the
// conversation may read now. It returns "" when there is nothing to tell.
func (s *Shown) Block(entries []Entry) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.primed {
		s.primed = true
		s.known = snapshot(entries)
		if len(entries) == 0 {
			return ""
		}
		return fullBlock(entries)
	}
	var changed []Entry
	seen := make(map[int64]bool, len(entries))
	for _, e := range entries {
		seen[e.ID] = true
		if old, ok := s.known[e.ID]; !ok || !sameView(old, e) {
			changed = append(changed, e)
		}
	}
	var removed []Entry
	for id, old := range s.known {
		if !seen[id] {
			removed = append(removed, old)
		}
	}
	slices.SortFunc(removed, func(a, b Entry) int { return int(a.ID - b.ID) })
	s.known = snapshot(entries)
	if len(changed) == 0 && len(removed) == 0 {
		return ""
	}
	return deltaBlock(changed, removed)
}

func snapshot(entries []Entry) map[int64]Entry {
	m := make(map[int64]Entry, len(entries))
	for _, e := range entries {
		m[e.ID] = e
	}
	return m
}

// sameView compares what the prompt shows of an entry, and nothing that the
// rendering adds or that changes without the text changing.
func sameView(a, b Entry) bool {
	return a.Kind == b.Kind && a.Scope == b.Scope && a.Title == b.Title && a.Text == b.Text &&
		a.TopicKey == b.TopicKey && a.Tainted == b.Tainted && a.Pinned == b.Pinned && a.State == b.State
}

// label names a removed entry without repeating its text, which may be untrusted.
func label(e Entry) string {
	if e.TopicKey != "" {
		return e.TopicKey
	}
	return fmt.Sprintf("#%d [%s]", e.ID, e.Kind)
}

const (
	memoryIntro = "This is the agent's stored memory for this space, as it stands at this turn."
	updateIntro = "The agent's stored memory for this space changed since your last turn."
)

func fullBlock(entries []Entry) string {
	var sb strings.Builder
	sb.WriteString("<memory>\n")
	sb.WriteString(memoryIntro + "\n")
	sb.WriteString(FormatPreamble(entries))
	sb.WriteString("</memory>")
	return sb.String()
}

func deltaBlock(changed, removed []Entry) string {
	var sb strings.Builder
	sb.WriteString("<memory-update>\n")
	sb.WriteString(updateIntro + "\n")
	if len(changed) > 0 {
		sb.WriteString("Added or updated:\n")
		sb.WriteString(FormatPreamble(changed))
	}
	if len(removed) > 0 {
		sb.WriteString("Removed:\n")
		for _, e := range removed {
			sb.WriteString("- " + label(e) + "\n")
		}
	}
	sb.WriteString("</memory-update>")
	return sb.String()
}
