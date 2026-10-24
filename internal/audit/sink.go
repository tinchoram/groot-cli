package audit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"
)

// QueueSize is how many records a sink may hold while it is busy. A record
// that finds the queue full is dropped and counted, never waited for.
const QueueSize = 1000

// failWarnAt is the number of failures after which a sink warns, once.
const failWarnAt = 3

// Sink receives a copy of every audit record, asynchronously. Write and Flush
// may be slow or fail; the fanout absorbs both. A sink that buffers implements
// Flusher too.
type Sink interface {
	Write(ctx context.Context, r Record) error
	Close(ctx context.Context) error
}

// Flusher is implemented by sinks that hold records before sending them.
type Flusher interface {
	Flush(ctx context.Context) error
}

// droppedError is returned by a sink that gave up on records. The fanout adds
// Dropped to its counter, and the error text is the reason.
type droppedError struct {
	n     int
	cause error
}

func (e droppedError) Error() string {
	return fmt.Sprintf("%v; %d records dropped", e.cause, e.n)
}

func (e droppedError) Unwrap() error { return e.cause }

// Dropped is the number of records the sink gave up on.
func (e droppedError) Dropped() int { return e.n }

// SinkStat is the export counters of one sink.
type SinkStat struct {
	Name      string
	Sent      int
	Dropped   int // records lost to a full queue or to a failed batch
	Failures  int // failed writes and flushes
	LastError string
}

// Fanout copies records to sinks. Offer never blocks: each sink has a queue of
// QueueSize records, drained by its own goroutine. A nil *Fanout discards.
type Fanout struct {
	warn func(string)

	mu     sync.Mutex
	sinks  []*queue
	names  map[string]int
	closed bool
}

// queue is one sink with its queue and counters.
type queue struct {
	name       string
	sink       Sink
	ch         chan Record
	flushEvery time.Duration
	ctx        context.Context
	cancel     context.CancelFunc
	done       chan struct{}
	warn       func(string)

	mu        sync.Mutex
	sent      int
	dropped   int
	failures  int
	lastError string
}

// NewFanout returns a fanout. warn receives one line per sink when it has
// failed failWarnAt times; it may be nil.
func NewFanout(warn func(string)) *Fanout {
	return &Fanout{warn: warn, names: map[string]int{}}
}

// Add starts delivering to s under name. flushEvery, when positive, makes the
// fanout call Flush on s at that interval, for sinks that batch.
func (f *Fanout) Add(name string, s Sink, flushEvery time.Duration) {
	if f == nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return
	}
	f.names[name]++
	if n := f.names[name]; n > 1 {
		name = fmt.Sprintf("%s#%d", name, n)
	}
	ctx, cancel := context.WithCancel(context.Background())
	q := &queue{name: name, sink: s, ch: make(chan Record, QueueSize), flushEvery: flushEvery,
		ctx: ctx, cancel: cancel, done: make(chan struct{}), warn: f.warn}
	f.sinks = append(f.sinks, q)
	go f.run(q)
}

// Offer hands a copy of r to every sink. It never blocks.
func (f *Fanout) Offer(r Record) {
	if f == nil {
		return
	}
	r.Guard = slices.Clone(r.Guard)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return
	}
	for _, q := range f.sinks {
		select {
		case q.ch <- r:
		default:
			q.mu.Lock()
			q.dropped++
			q.mu.Unlock()
		}
	}
}

// Close stops the fanout: each sink delivers what it holds, is flushed and
// closed. ctx caps the wait. When it ends first, the sinks are cancelled and
// Close returns an error naming the sinks that did not finish.
func (f *Fanout) Close(ctx context.Context) error {
	if f == nil {
		return nil
	}
	f.mu.Lock()
	if f.closed {
		f.mu.Unlock()
		return nil
	}
	f.closed = true
	sinks := slices.Clone(f.sinks)
	for _, q := range sinks {
		close(q.ch)
	}
	f.mu.Unlock()

	var late []string
	for _, q := range sinks {
		select {
		case <-q.done:
		case <-ctx.Done():
			q.cancel()
			late = append(late, q.name)
		}
	}
	if len(late) > 0 {
		return fmt.Errorf("audit sinks did not finish in time: %s", strings.Join(late, ", "))
	}
	return nil
}

// Stats returns the counters of every sink, in the order they were added.
func (f *Fanout) Stats() []SinkStat {
	if f == nil {
		return nil
	}
	f.mu.Lock()
	sinks := slices.Clone(f.sinks)
	f.mu.Unlock()
	out := make([]SinkStat, 0, len(sinks))
	for _, q := range sinks {
		q.mu.Lock()
		out = append(out, SinkStat{Name: q.name, Sent: q.sent, Dropped: q.dropped, Failures: q.failures, LastError: q.lastError})
		q.mu.Unlock()
	}
	return out
}

// run delivers the queue of one sink until the fanout closes it.
func (f *Fanout) run(q *queue) {
	defer close(q.done)
	var tick <-chan time.Time
	if q.flushEvery > 0 {
		t := time.NewTicker(q.flushEvery)
		defer t.Stop()
		tick = t.C
	}
	for {
		select {
		case r, ok := <-q.ch:
			if !ok {
				q.flush()
				if err := q.sink.Close(q.ctx); err != nil {
					q.fail(err)
				}
				return
			}
			if err := q.sink.Write(q.ctx, r); err != nil {
				q.fail(err)
				continue
			}
			q.mu.Lock()
			q.sent++
			q.mu.Unlock()
		case <-tick:
			q.flush()
		}
	}
}

func (q *queue) flush() {
	fl, ok := q.sink.(Flusher)
	if !ok {
		return
	}
	if err := fl.Flush(q.ctx); err != nil {
		q.fail(err)
	}
}

// fail counts an error of q, and warns once when the sink reaches failWarnAt.
func (q *queue) fail(err error) {
	var d interface{ Dropped() int }
	q.mu.Lock()
	q.failures++
	q.lastError = err.Error()
	if errors.As(err, &d) {
		q.dropped += d.Dropped()
	}
	warnNow := q.failures == failWarnAt
	failures, last := q.failures, q.lastError
	q.mu.Unlock()
	if warnNow && q.warn != nil {
		q.warn(fmt.Sprintf("audit sink %s: %d failures, last: %s", q.name, failures, last))
	}
}

// encodeLine is one record as a JSON line, newline included.
func encodeLine(r Record) ([]byte, error) {
	b, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// redact replaces each secret in s with "[redacted]". Empty secrets are ignored.
func redact(s string, secrets []string) string {
	for _, v := range secrets {
		if v != "" {
			s = strings.ReplaceAll(s, v, "[redacted]")
		}
	}
	return s
}
