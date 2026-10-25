// Package clock formats instants for people and for the model: the zone label
// of a location, a full stamp for tool output, and the one-line prompt notice.
package clock

import (
	"fmt"
	"os"
	"strings"
	"time"
)

const (
	localtimePath = "/etc/localtime"
	zoneinfoMark  = "zoneinfo/"
)

// Namer names zones. Getenv and Readlink are the host's environment and the
// /etc/localtime link; they are injected so tests need no real host state.
type Namer struct {
	Getenv   func(string) string
	Readlink func(string) (string, error)
}

// System reads the real environment and link. The package functions use it.
var System = Namer{Getenv: os.Getenv, Readlink: os.Readlink}

// Name returns the IANA name of loc, or "" when it has none. A location other
// than time.Local keeps its own name. For time.Local the name is resolved in
// order: a named Local zone, then the TZ variable when it loads, then the zone
// file that /etc/localtime links to (macOS and Linux both keep it under a
// zoneinfo/ directory). A candidate counts only if it loads.
func (n Namer) Name(loc *time.Location) string {
	if loc != time.Local {
		return loc.String()
	}
	if name := loc.String(); name != "" && name != "Local" {
		return name
	}
	if tz := strings.TrimPrefix(n.Getenv("TZ"), ":"); tz != "" && !strings.HasPrefix(tz, "/") && loadable(tz) {
		return tz
	}
	target, err := n.Readlink(localtimePath)
	if err != nil {
		return ""
	}
	i := strings.Index(target, zoneinfoMark)
	if i < 0 {
		return ""
	}
	if name := target[i+len(zoneinfoMark):]; loadable(name) {
		return name
	}
	return ""
}

// loadable reports whether name is a zone the runtime can load.
func loadable(name string) bool {
	if name == "" || name == "Local" {
		return false
	}
	_, err := time.LoadLocation(name)
	return err == nil
}

// Zone returns the UTC offset of t in loc ("-03", "+05:30") and the label of
// its zone: the IANA name when Name finds one, else the zone abbreviation. The
// label is empty when the zone has no abbreviation or only a numeric one.
func (n Namer) Zone(t time.Time, loc *time.Location) (offset, label string) {
	abbr, secs := t.In(loc).Zone()
	offset = formatOffset(secs)
	if name := n.Name(loc); name != "" {
		return offset, name
	}
	if abbr == "" || abbr[0] == '+' || abbr[0] == '-' {
		return offset, ""
	}
	return offset, abbr
}

// Stamp returns t in loc as "2026-10-25 18:42:07 -03 (America/Argentina/Buenos_Aires)".
// The parenthesized zone label is omitted when Zone returns none.
func (n Namer) Stamp(t time.Time, loc *time.Location) string {
	offset, label := n.Zone(t, loc)
	s := t.In(loc).Format("2006-01-02 15:04:05") + " " + offset
	if label != "" {
		s += " (" + label + ")"
	}
	return s
}

// PromptLine is the notice given to the model on every turn, minute precision:
// "Current time: 2026-10-25 18:42 (-03, America/Argentina/Buenos_Aires), Sunday."
func (n Namer) PromptLine(t time.Time, loc *time.Location) string {
	local := t.In(loc)
	offset, label := n.Zone(t, loc)
	zone := offset
	if label != "" {
		zone += ", " + label
	}
	return fmt.Sprintf("Current time: %s (%s), %s.", local.Format("2006-01-02 15:04"), zone, local.Weekday())
}

// Zone is Namer.Zone on System.
func Zone(t time.Time, loc *time.Location) (offset, label string) { return System.Zone(t, loc) }

// Stamp is Namer.Stamp on System.
func Stamp(t time.Time, loc *time.Location) string { return System.Stamp(t, loc) }

// PromptLine is Namer.PromptLine on System.
func PromptLine(t time.Time, loc *time.Location) string { return System.PromptLine(t, loc) }

func formatOffset(secs int) string {
	sign := "+"
	if secs < 0 {
		sign = "-"
		secs = -secs
	}
	h, m := secs/3600, secs%3600/60
	if m == 0 {
		return fmt.Sprintf("%s%02d", sign, h)
	}
	return fmt.Sprintf("%s%02d:%02d", sign, h, m)
}
