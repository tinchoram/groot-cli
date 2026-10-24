package audit

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

const (
	defaultBatch   = 50
	defaultRetries = 3
	defaultBackoff = 500 * time.Millisecond
	httpTimeout    = 10 * time.Second
)

// HTTPOptions configure an HTTPSink. Zero values take the defaults: a batch of
// 50 records, 3 retries, and 500ms of backoff that doubles on each retry.
type HTTPOptions struct {
	URL     string
	Headers map[string]string // header values are secrets: they never reach an error
	Batch   int
	Retries int
	Backoff time.Duration
	Client  *http.Client
}

// HTTPSink POSTs JSON Lines batches to a collector. A batch goes out when it
// holds Batch records, on Flush, and on Close. A batch that still fails after
// the retries is dropped and reported as a droppedError.
type HTTPSink struct {
	opts    HTTPOptions
	secrets []string

	mu  sync.Mutex
	buf []byte // pending lines
	n   int    // pending records
}

// NewHTTPSink returns a sink for opts. It does not connect until a batch goes out.
func NewHTTPSink(opts HTTPOptions) *HTTPSink {
	if opts.Batch <= 0 {
		opts.Batch = defaultBatch
	}
	if opts.Retries < 0 {
		opts.Retries = 0
	} else if opts.Retries == 0 {
		opts.Retries = defaultRetries
	}
	if opts.Backoff <= 0 {
		opts.Backoff = defaultBackoff
	}
	if opts.Client == nil {
		opts.Client = &http.Client{Timeout: httpTimeout}
	}
	secrets := []string{opts.URL}
	for _, v := range opts.Headers {
		secrets = append(secrets, v)
	}
	return &HTTPSink{opts: opts, secrets: secrets}
}

// Write adds r to the batch and sends the batch when it is full.
func (s *HTTPSink) Write(ctx context.Context, r Record) error {
	line, err := encodeLine(r)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.buf = append(s.buf, line...)
	s.n++
	if s.n < s.opts.Batch {
		return nil
	}
	return s.sendLocked(ctx)
}

// Flush sends the pending batch, if any.
func (s *HTTPSink) Flush(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sendLocked(ctx)
}

// Close sends what is pending. The sink takes no records after it.
func (s *HTTPSink) Close(ctx context.Context) error {
	return s.Flush(ctx)
}

// sendLocked posts the pending batch with retries. Callers hold s.mu.
func (s *HTTPSink) sendLocked(ctx context.Context) error {
	if s.n == 0 {
		return nil
	}
	body := s.buf
	n := s.n
	s.buf, s.n = nil, 0

	var last error
	for attempt := 0; attempt <= s.opts.Retries; attempt++ {
		if attempt > 0 {
			wait := s.opts.Backoff << (attempt - 1)
			select {
			case <-ctx.Done():
				return droppedError{n: n, cause: fmt.Errorf("audit sink http: %w", ctx.Err())}
			case <-time.After(wait):
			}
		}
		last = s.post(ctx, body)
		if last == nil {
			return nil
		}
	}
	return droppedError{n: n, cause: last}
}

// post sends one batch. Its errors name the status or the failure class only.
func (s *HTTPSink) post(ctx context.Context, body []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.opts.URL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("audit sink http: %s", redact(err.Error(), s.secrets))
	}
	req.Header.Set("Content-Type", "application/x-ndjson")
	for k, v := range s.opts.Headers {
		req.Header.Set(k, v)
	}
	resp, err := s.opts.Client.Do(req)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("audit sink http: request cancelled")
		}
		return fmt.Errorf("audit sink http: request failed: %s", redact(err.Error(), s.secrets))
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("audit sink http: HTTP %d", resp.StatusCode)
	}
	return nil
}
