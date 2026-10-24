package audit

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// stallSink blocks every Write until release is closed.
type stallSink struct {
	release chan struct{}
}

func (s *stallSink) Write(ctx context.Context, r Record) error {
	<-s.release
	return nil
}

func (s *stallSink) Close(context.Context) error { return nil }

// memSink keeps the tool names it receives.
type memSink struct {
	mu    sync.Mutex
	tools []string
	flush atomic.Int32
}

func (s *memSink) Write(_ context.Context, r Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tools = append(s.tools, r.Tool)
	return nil
}

func (s *memSink) Flush(context.Context) error {
	s.flush.Add(1)
	return nil
}

func (s *memSink) Close(context.Context) error { return nil }

func (s *memSink) got() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.tools...)
}

// failSink fails every Write with boom.
type failSink struct{}

func (failSink) Write(context.Context, Record) error { return errors.New("boom") }
func (failSink) Close(context.Context) error         { return nil }

func TestFanoutNeverBlocksOnAStalledSink(t *testing.T) {
	stall := &stallSink{release: make(chan struct{})}
	f := NewFanout(nil)
	f.Add("stalled", stall, 0)

	start := time.Now()
	const offers = 3000
	for i := 0; i < offers; i++ {
		f.Offer(Record{Tool: "bash"})
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("Offer blocked for %v behind a stalled sink", elapsed)
	}

	st := f.Stats()
	if len(st) != 1 {
		t.Fatalf("stats = %+v", st)
	}
	// One record is in the sink's Write, QueueSize wait in the queue; the rest
	// is dropped and counted.
	if st[0].Dropped < offers-QueueSize-1 || st[0].Dropped > offers-QueueSize {
		t.Fatalf("dropped = %d, want about %d", st[0].Dropped, offers-QueueSize)
	}

	close(stall.release)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := f.Close(ctx); err != nil {
		t.Fatalf("Close after release: %v", err)
	}
}

func TestFanoutSinkErrorsAreCountedAndWarnedOnce(t *testing.T) {
	var mu sync.Mutex
	var warnings []string
	f := NewFanout(func(msg string) {
		mu.Lock()
		defer mu.Unlock()
		warnings = append(warnings, msg)
	})
	f.Add("flaky", failSink{}, 0)
	for i := 0; i < 10; i++ {
		f.Offer(Record{Tool: "bash"})
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		st := f.Stats()
		if len(st) == 1 && st[0].Failures == 10 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("failures never reached 10: %+v", st)
		}
		time.Sleep(5 * time.Millisecond)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := f.Close(ctx); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(warnings) != 1 || warnings[0] != "audit sink flaky: 3 failures, last: boom" {
		t.Fatalf("warnings = %q, want exactly one after 3 failures", warnings)
	}
	st := f.Stats()
	if st[0].LastError != "boom" {
		t.Fatalf("last error = %q", st[0].LastError)
	}
}

func TestFanoutDeliversCopiesAndFlushesOnClose(t *testing.T) {
	sink := &memSink{}
	f := NewFanout(nil)
	f.Add("mem", sink, 0)
	for _, tool := range []string{"a", "b", "c"} {
		f.Offer(Record{Tool: tool, Guard: []string{"override"}})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := f.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(sink.got(), ","); got != "a,b,c" {
		t.Fatalf("delivered %q, want a,b,c", got)
	}
	if sink.flush.Load() == 0 {
		t.Fatal("Close must flush the sink")
	}
	if st := f.Stats(); st[0].Sent != 3 {
		t.Fatalf("sent = %d, want 3", st[0].Sent)
	}
}

func TestFanoutCloseIsCappedByItsContext(t *testing.T) {
	stall := &stallSink{release: make(chan struct{})}
	defer close(stall.release)
	f := NewFanout(nil)
	f.Add("stalled", stall, 0)
	f.Offer(Record{Tool: "bash"})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := f.Close(ctx)
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("Close took %v with a 100ms cap", elapsed)
	}
	if err == nil {
		t.Fatal("Close must report the sinks it could not flush")
	}
}

func TestFanoutNilIsNoop(t *testing.T) {
	var f *Fanout
	f.Offer(Record{Tool: "x"})
	if err := f.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if f.Stats() != nil {
		t.Fatal("nil fanout has no stats")
	}
}

func TestLogExportsAfterTheLocalWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	l, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	sink := &memSink{}
	f := NewFanout(nil)
	f.Add("mem", sink, 0)
	l.SetFanout(f)
	if err := l.Write(Record{Tool: "bash", Decision: "allow"}); err != nil {
		t.Fatal(err)
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := f.Close(ctx); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(raw), `"tool":"bash"`) {
		t.Fatalf("local file must hold the record: %q, %v", raw, err)
	}
	if got := sink.got(); len(got) != 1 || got[0] != "bash" {
		t.Fatalf("sink got %v", got)
	}
}

func TestFileSinkAppends(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	s, err := NewFileSink("~/central/all.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, "central", "all.jsonl")
	if err := s.Write(context.Background(), Record{Tool: "a", Space: "one"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(context.Background()); err != nil {
		t.Fatal(err)
	}

	s2, err := NewFileSink(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s2.Write(context.Background(), Record{Tool: "b", Space: "two"}); err != nil {
		t.Fatal(err)
	}
	if err := s2.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], `"space":"one"`) || !strings.Contains(lines[1], `"space":"two"`) {
		t.Fatalf("file sink must append JSON lines: %q", raw)
	}
}

func TestFileSinkRejectsEmptyPath(t *testing.T) {
	if _, err := NewFileSink("  "); err == nil {
		t.Fatal("empty path must fail")
	}
}

// collector is an httptest server that records each request body and header.
type collector struct {
	mu      sync.Mutex
	bodies  []string
	headers []http.Header
	status  func(attempt int) int
	calls   int
}

func (c *collector) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	c.mu.Lock()
	c.calls++
	attempt := c.calls
	code := http.StatusOK
	if c.status != nil {
		code = c.status(attempt)
	}
	if code == http.StatusOK {
		c.bodies = append(c.bodies, string(b))
		c.headers = append(c.headers, r.Header.Clone())
	}
	c.mu.Unlock()
	w.WriteHeader(code)
}

func (c *collector) lines() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []string
	for _, b := range c.bodies {
		sc := bufio.NewScanner(strings.NewReader(b))
		for sc.Scan() {
			out = append(out, sc.Text())
		}
	}
	return out
}

func TestHTTPSinkBatchesJSONLines(t *testing.T) {
	c := &collector{}
	srv := httptest.NewServer(c)
	defer srv.Close()

	s := NewHTTPSink(HTTPOptions{URL: srv.URL, Headers: map[string]string{"Authorization": "Bearer tok-123"}, Batch: 3, Backoff: time.Millisecond})
	ctx := context.Background()
	for i := 0; i < 7; i++ {
		if err := s.Write(ctx, Record{Tool: fmt.Sprintf("t%d", i), Space: "one"}); err != nil {
			t.Fatal(err)
		}
	}
	// Two full batches went out on their own; the seventh waits for Close.
	if n := len(c.lines()); n != 6 {
		t.Fatalf("before Close: %d lines delivered, want 6", n)
	}
	if err := s.Close(ctx); err != nil {
		t.Fatal(err)
	}
	lines := c.lines()
	if len(lines) != 7 {
		t.Fatalf("after Close: %d lines, want 7", len(lines))
	}
	for _, l := range lines {
		var r Record
		if err := json.Unmarshal([]byte(l), &r); err != nil || r.Space != "one" {
			t.Fatalf("line is not an exported record: %q (%v)", l, err)
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if got := c.headers[0].Get("Authorization"); got != "Bearer tok-123" {
		t.Fatalf("Authorization = %q", got)
	}
	if got := c.headers[0].Get("Content-Type"); !strings.HasPrefix(got, "application/x-ndjson") {
		t.Fatalf("Content-Type = %q", got)
	}
}

func TestHTTPSinkFlushSendsPending(t *testing.T) {
	c := &collector{}
	srv := httptest.NewServer(c)
	defer srv.Close()
	s := NewHTTPSink(HTTPOptions{URL: srv.URL, Batch: 50, Backoff: time.Millisecond})
	if err := s.Write(context.Background(), Record{Tool: "x"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := len(c.lines()); n != 1 {
		t.Fatalf("flush delivered %d lines, want 1", n)
	}
}

func TestHTTPSinkRetriesThenDelivers(t *testing.T) {
	c := &collector{status: func(attempt int) int {
		if attempt <= 2 {
			return http.StatusInternalServerError
		}
		return http.StatusOK
	}}
	srv := httptest.NewServer(c)
	defer srv.Close()
	s := NewHTTPSink(HTTPOptions{URL: srv.URL, Batch: 1, Backoff: time.Millisecond})
	if err := s.Write(context.Background(), Record{Tool: "x"}); err != nil {
		t.Fatalf("a batch that succeeds on retry must not fail: %v", err)
	}
	if n := len(c.lines()); n != 1 {
		t.Fatalf("delivered %d lines after retries, want 1", n)
	}
	if c.calls != 3 {
		t.Fatalf("attempts = %d, want 3", c.calls)
	}
}

func TestHTTPSinkDropsAfterRetriesAndRedactsErrors(t *testing.T) {
	const secret = "tok-SUPER-SECRET"
	c := &collector{status: func(int) int { return http.StatusServiceUnavailable }}
	srv := httptest.NewServer(c)
	defer srv.Close()
	s := NewHTTPSink(HTTPOptions{URL: srv.URL, Headers: map[string]string{"Authorization": "Bearer " + secret}, Batch: 1, Backoff: time.Millisecond})
	err := s.Write(context.Background(), Record{Tool: "x"})
	if err == nil {
		t.Fatal("a batch that never lands must report an error")
	}
	if c.calls != 4 {
		t.Fatalf("attempts = %d, want 1 + 3 retries", c.calls)
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("error leaks the header value: %v", err)
	}
	if !strings.Contains(err.Error(), "503") {
		t.Fatalf("error should name the status: %v", err)
	}
}

func TestHTTPSinkTransportErrorDoesNotLeakHeaders(t *testing.T) {
	const secret = "tok-TRANSPORT-SECRET"
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close() // nothing listens any more
	s := NewHTTPSink(HTTPOptions{URL: url, Headers: map[string]string{"Authorization": secret}, Batch: 1, Backoff: time.Millisecond})
	err := s.Write(context.Background(), Record{Tool: "x"})
	if err == nil {
		t.Fatal("refused connection must be an error")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("transport error leaks the header value: %v", err)
	}
}

func TestRedactRemovesSecrets(t *testing.T) {
	got := redact("failed with Bearer abc123 at host", []string{"abc123", ""})
	if strings.Contains(got, "abc123") || !strings.Contains(got, "[redacted]") {
		t.Fatalf("Redact = %q", got)
	}
}

func TestSyslogSinkWritesThroughItsWriter(t *testing.T) {
	w := &fakeSyslog{}
	s := NewSyslogSink(w)
	if err := s.Write(context.Background(), Record{Tool: "bash", Space: "one"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(w.msgs) != 1 || !strings.Contains(w.msgs[0], `"tool":"bash"`) {
		t.Fatalf("syslog messages = %q", w.msgs)
	}
	if !w.closed {
		t.Fatal("Close must close the writer")
	}

	failing := NewSyslogSink(&fakeSyslog{err: errors.New("no daemon")})
	if err := failing.Write(context.Background(), Record{Tool: "x"}); err == nil {
		t.Fatal("a writer error must surface")
	}
}

type fakeSyslog struct {
	msgs   []string
	err    error
	closed bool
}

func (f *fakeSyslog) Write(b []byte) (int, error) {
	if f.err != nil {
		return 0, f.err
	}
	f.msgs = append(f.msgs, string(b))
	return len(b), nil
}

func (f *fakeSyslog) Close() error {
	f.closed = true
	return nil
}
