//go:build !unix

package audit

import "errors"

// DialSyslog reports that the syslog sink needs a unix system.
func DialSyslog(string) (*SyslogSink, error) {
	return nil, errors.New("audit sink syslog: not available on this platform")
}
