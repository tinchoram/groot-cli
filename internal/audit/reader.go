package audit

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
)

// DefaultLimit is how many records Read returns when the query sets no limit.
const DefaultLimit = 50

// readChunk is how many bytes the reverse reader takes from the file at a time.
const readChunk = 64 << 10

// Query selects records. Zero fields do not filter. Limit caps Read; zero means
// DefaultLimit.
type Query struct {
	Since, Until time.Time
	Tool         string
	Risk         string
	Decision     string
	Agent        string
	Session      string
	// Find is a case-insensitive substring of the input preview.
	Find        string
	ErrorsOnly  bool
	GuardOnly   bool // records with guard findings
	SandboxNone bool // calls that ran with no sandbox backend
	Limit       int
}

// Matches reports whether r passes every filter of q. It ignores Limit.
func (q Query) Matches(r Record) bool {
	if !q.Since.IsZero() && r.Time.Before(q.Since) {
		return false
	}
	if !q.Until.IsZero() && r.Time.After(q.Until) {
		return false
	}
	if q.Tool != "" && r.Tool != q.Tool {
		return false
	}
	if q.Risk != "" && r.Risk != q.Risk {
		return false
	}
	if q.Decision != "" && r.Decision != q.Decision {
		return false
	}
	if q.Agent != "" && r.Agent != q.Agent {
		return false
	}
	if q.Session != "" && r.Session != q.Session {
		return false
	}
	if q.ErrorsOnly && !r.IsError {
		return false
	}
	if q.GuardOnly && len(r.Guard) == 0 {
		return false
	}
	if q.SandboxNone && r.Sandbox != "none" {
		return false
	}
	if q.Find != "" && !strings.Contains(strings.ToLower(r.InputPreview), strings.ToLower(q.Find)) {
		return false
	}
	return true
}

// Read returns the records of the JSONL file at path that match q, newest
// first, at most q.Limit (DefaultLimit when zero). It walks the file from the
// end, so a small limit does not read the whole file. A missing file has no
// records. Malformed lines are skipped.
func Read(path string, q Query) ([]Record, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open audit log: %w", err)
	}
	defer f.Close()

	limit := q.Limit
	if limit <= 0 {
		limit = DefaultLimit
	}
	rr := newReverseLines(f, readChunk)
	var out []Record
	for len(out) < limit {
		b, ok, err := rr.Next()
		if err != nil {
			return nil, fmt.Errorf("read audit log: %w", err)
		}
		if !ok {
			break
		}
		var r Record
		if json.Unmarshal(b, &r) != nil || !q.Matches(r) {
			continue
		}
		out = append(out, r)
	}
	return out, nil
}

// Stats summarizes the records that match a query.
type Stats struct {
	Total      int
	ByDecision map[string]int
	ByRisk     map[string]int
	Denied     int
	Guard      int // records with guard findings
	Errors     int
	// Skipped counts the malformed lines of the file, whatever the query.
	Skipped int
	// Sinks are the export counters of the sinks, when the workspace fills them.
	Sinks []SinkStat
}

// Summarize counts the records of the file at path that match q. It reads the
// whole file, streaming, and ignores q.Limit. A missing file is empty.
func Summarize(path string, q Query) (Stats, error) {
	st := Stats{ByDecision: map[string]int{}, ByRisk: map[string]int{}}
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return st, nil
	}
	if err != nil {
		return st, fmt.Errorf("open audit log: %w", err)
	}
	defer f.Close()

	br := bufio.NewReaderSize(f, readChunk)
	for {
		b, err := br.ReadBytes('\n')
		if len(bytes.TrimSpace(b)) > 0 {
			var r Record
			if json.Unmarshal(b, &r) != nil {
				st.Skipped++
			} else if q.Matches(r) {
				st.add(r)
			}
		}
		if errors.Is(err, io.EOF) {
			return st, nil
		}
		if err != nil {
			return st, fmt.Errorf("read audit log: %w", err)
		}
	}
}

func (st *Stats) add(r Record) {
	st.Total++
	st.ByDecision[r.Decision]++
	st.ByRisk[r.Risk]++
	if r.Decision == "deny" {
		st.Denied++
	}
	if len(r.Guard) > 0 {
		st.Guard++
	}
	if r.IsError {
		st.Errors++
	}
}

// ParseWhen reads a point in time for --since style filters. It accepts a
// duration back from now ("90m", "2h", "1d", where d is 24 hours) or an
// absolute time: RFC 3339, "2006-01-02 15:04", "2006-01-02", or "15:04" today.
func ParseWhen(s string, now time.Time) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, errors.New("empty time")
	}
	if strings.HasSuffix(s, "d") {
		if n, err := strconv.ParseFloat(strings.TrimSuffix(s, "d"), 64); err == nil {
			if n < 0 {
				return time.Time{}, fmt.Errorf("negative duration %q", s)
			}
			return now.Add(-time.Duration(n * float64(24*time.Hour))), nil
		}
	}
	if d, err := time.ParseDuration(s); err == nil {
		if d < 0 {
			return time.Time{}, fmt.Errorf("negative duration %q", s)
		}
		return now.Add(-d), nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	for _, layout := range []string{"2006-01-02 15:04", "2006-01-02"} {
		if t, err := time.ParseInLocation(layout, s, now.Location()); err == nil {
			return t, nil
		}
	}
	if t, err := time.ParseInLocation("15:04", s, now.Location()); err == nil {
		y, m, d := now.Date()
		return time.Date(y, m, d, t.Hour(), t.Minute(), 0, 0, now.Location()), nil
	}
	return time.Time{}, fmt.Errorf("not a time or duration: %q", s)
}

// reverseLines yields the lines of a file from the last to the first. Empty
// lines are skipped. It reads chunk bytes at a time.
type reverseLines struct {
	f     *os.File
	pos   int64  // file offset of the first byte not yet read
	chunk int64  // bytes per read
	buf   []byte // bytes read and not yet returned; a partial line at its start
}

func newReverseLines(f *os.File, chunk int) *reverseLines {
	size := int64(0)
	if fi, err := f.Stat(); err == nil {
		size = fi.Size()
	}
	return &reverseLines{f: f, pos: size, chunk: int64(chunk)}
}

// Next returns the next line from the end, without its newline. ok is false
// at the start of the file.
func (r *reverseLines) Next() (line []byte, ok bool, err error) {
	for {
		if i := bytes.LastIndexByte(r.buf, '\n'); i >= 0 {
			line = r.buf[i+1:]
			r.buf = r.buf[:i]
			if len(line) > 0 {
				return line, true, nil
			}
			continue
		}
		if r.pos == 0 {
			if len(r.buf) > 0 {
				line = r.buf
				r.buf = nil
				return line, true, nil
			}
			return nil, false, nil
		}
		n := min(r.chunk, r.pos)
		r.pos -= n
		next := make([]byte, n, n+int64(len(r.buf)))
		if _, err := r.f.ReadAt(next, r.pos); err != nil && !errors.Is(err, io.EOF) {
			return nil, false, err
		}
		r.buf = append(next, r.buf...)
	}
}
