//go:build unix

package audit

import (
	"fmt"
	"log/syslog"
)

// DialSyslog opens the local syslog daemon with tag as the program name.
func DialSyslog(tag string) (*SyslogSink, error) {
	w, err := syslog.New(syslog.LOG_INFO|syslog.LOG_USER, tag)
	if err != nil {
		return nil, fmt.Errorf("audit sink syslog: %w", err)
	}
	return NewSyslogSink(w), nil
}
