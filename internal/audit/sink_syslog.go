package audit

import (
	"context"
	"fmt"
	"io"
)

// SyslogWriter is what the syslog sink writes to. The unix dialer returns a
// *log/syslog.Writer; tests pass a fake.
type SyslogWriter interface {
	io.Writer
	Close() error
}

// SyslogSink sends one JSON line per record to a syslog writer.
type SyslogSink struct {
	w SyslogWriter
}

// NewSyslogSink wraps w.
func NewSyslogSink(w SyslogWriter) *SyslogSink {
	return &SyslogSink{w: w}
}

// Write sends r as one syslog message.
func (s *SyslogSink) Write(_ context.Context, r Record) error {
	line, err := encodeLine(r)
	if err != nil {
		return err
	}
	if _, err := s.w.Write(line[:len(line)-1]); err != nil {
		return fmt.Errorf("audit sink syslog: %w", err)
	}
	return nil
}

// Close closes the writer.
func (s *SyslogSink) Close(context.Context) error {
	return s.w.Close()
}
