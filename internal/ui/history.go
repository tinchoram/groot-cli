package ui

import (
	"bufio"
	"os"
	"strings"
)

// HistoryCap is the most entries kept, in memory and on disk.
const HistoryCap = 1000

// History is the shell-style prompt history. Entries are immutable strings,
// so editing the recalled text in the input never changes the history.
type History struct {
	entries  []string // oldest first
	max      int
	cursor   int    // index of the recalled entry; len(entries) means the draft
	draft    string // text typed before browsing started
	browsing bool
}

// NewHistory returns a history holding up to max entries, starting with the
// given entries (oldest first).
func NewHistory(entries []string, max int) *History {
	h := &History{max: max}
	for _, e := range entries {
		h.Push(e)
	}
	return h
}

// Push records a submitted line. Blank lines and a line equal to the newest
// entry are not stored. Pushing also ends browsing.
func (h *History) Push(line string) {
	h.browsing = false
	h.cursor = len(h.entries)
	if strings.TrimSpace(line) == "" {
		return
	}
	if n := len(h.entries); n > 0 && h.entries[n-1] == line {
		return
	}
	h.entries = append(h.entries, line)
	if over := len(h.entries) - h.max; h.max > 0 && over > 0 {
		h.entries = h.entries[over:]
	}
}

// Prev steps to the previous entry. The first step saves current as the draft.
// At the oldest entry it stays there.
func (h *History) Prev(current string) (string, bool) {
	if len(h.entries) == 0 {
		return "", false
	}
	if !h.browsing {
		h.browsing = true
		h.draft = current
		h.cursor = len(h.entries)
	}
	if h.cursor > 0 {
		h.cursor--
	}
	return h.entries[h.cursor], true
}

// Next steps toward the newest entry. Past the newest it restores the draft
// and stops browsing. It reports false when browsing has not started.
func (h *History) Next() (string, bool) {
	if !h.browsing {
		return "", false
	}
	if h.cursor < len(h.entries)-1 {
		h.cursor++
		return h.entries[h.cursor], true
	}
	return h.Reset(), true
}

// Reset stops browsing and returns the draft that was saved.
func (h *History) Reset() string {
	h.browsing = false
	h.cursor = len(h.entries)
	return h.draft
}

// Browsing reports whether a recalled entry is shown.
func (h *History) Browsing() bool { return h.browsing }

// Entries returns a copy of the stored entries, oldest first.
func (h *History) Entries() []string {
	return append([]string(nil), h.entries...)
}

// escapeEntry writes an entry on one line: backslashes and newlines are escaped.
func escapeEntry(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	return strings.ReplaceAll(s, "\n", `\n`)
}

// unescapeEntry reverses escapeEntry.
func unescapeEntry(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\\' && i+1 < len(s) {
			switch s[i+1] {
			case 'n':
				b.WriteByte('\n')
				i++
				continue
			case '\\':
				b.WriteByte('\\')
				i++
				continue
			}
		}
		b.WriteByte(c)
	}
	return b.String()
}

// LoadHistory reads a history file. A missing file is an empty history. Only
// the newest HistoryCap entries are kept.
func LoadHistory(path string) ([]string, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var entries []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		if line := sc.Text(); line != "" {
			entries = append(entries, unescapeEntry(line))
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if over := len(entries) - HistoryCap; over > 0 {
		entries = entries[over:]
	}
	return entries, nil
}

// SaveHistory rewrites the history file with the newest HistoryCap entries.
// The file is created with mode 0600 and tightened if it already existed.
func SaveHistory(path string, entries []string) error {
	if over := len(entries) - HistoryCap; over > 0 {
		entries = entries[over:]
	}
	var b strings.Builder
	for _, e := range entries {
		b.WriteString(escapeEntry(e))
		b.WriteByte('\n')
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}
