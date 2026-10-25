package tool

import (
	"context"
	"strings"
	"testing"
	"time"
	_ "time/tzdata"

	"github.com/tinchoram/groot-cli/internal/clock"
)

// fixedClock returns the instant every call of a test sees.
func fixedClock(at time.Time) func() time.Time { return func() time.Time { return at } }

var tzInstant = time.Date(2026, 10, 25, 21, 42, 7, 0, time.UTC)

func TestCurrentTimeIsAReadToolWithoutApproval(t *testing.T) {
	ct := NewCurrentTime(fixedClock(tzInstant))
	if ct.Definition().Name != "current_time" {
		t.Fatalf("name = %q", ct.Definition().Name)
	}
	if ct.Risk() != RiskRead || ct.RiskFor(`{"timezone":"Mars/Olympus"}`) != RiskRead {
		t.Fatal("current_time must be RiskRead for every input")
	}
	if _, ok := any(ct).(Untrusted); ok {
		t.Fatal("current_time output is not untrusted text")
	}
}

func TestCurrentTimeOutput(t *testing.T) {
	local := time.FixedZone("", -3*3600)
	cases := []struct {
		name     string
		input    string
		wantSubs []string
		notSubs  []string
	}{
		{
			name:     "explicit timezone with a local line for comparison",
			input:    `{"timezone":"America/Argentina/Buenos_Aires"}`,
			wantSubs: []string{"2026-10-25 18:42:07 -03 (America/Argentina/Buenos_Aires), Sunday, unix 1792964527", "\nlocal: "},
		},
		{
			name:     "empty timezone is local time with no comparison line",
			input:    `{}`,
			wantSubs: []string{"2026-10-25 18:42:07 -03, Sunday, unix 1792964527"},
			notSubs:  []string{"local: "},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			ct := NewCurrentTime(fixedClock(tzInstant))
			ct.local = local
			out, isErr := ct.Execute(context.Background(), tt.input)
			if isErr {
				t.Fatalf("error result: %s", out)
			}
			for _, s := range tt.wantSubs {
				if !strings.Contains(out, s) {
					t.Fatalf("output %q lacks %q", out, s)
				}
			}
			for _, s := range tt.notSubs {
				if strings.Contains(out, s) {
					t.Fatalf("output %q must not contain %q", out, s)
				}
			}
		})
	}
}

func TestCurrentTimeRejectsUnknownTimezone(t *testing.T) {
	ct := NewCurrentTime(fixedClock(tzInstant))
	out, isErr := ct.Execute(context.Background(), `{"timezone":"Mars/Olympus"}`)
	if !isErr || out != "unknown timezone: Mars/Olympus" {
		t.Fatalf("out = %q, isErr = %v", out, isErr)
	}
}

func TestCurrentTimeReadsTheInjectedClock(t *testing.T) {
	calls := 0
	ct := NewCurrentTime(func() time.Time {
		calls++
		return tzInstant.Add(time.Duration(calls) * time.Hour)
	})
	first, _ := ct.Execute(context.Background(), `{}`)
	second, _ := ct.Execute(context.Background(), `{}`)
	if first == second {
		t.Fatalf("two calls gave the same time: %q", first)
	}
}

// With no TZ, the default-zone output names the zone the /etc/localtime link
// points to. The offset is the host's, so only the name is asserted.
func TestCurrentTimeDefaultZoneNamesTheLinkedZone(t *testing.T) {
	if time.Local.String() != "Local" {
		t.Skipf("time.Local is named %q on this host", time.Local.String())
	}
	ct := NewCurrentTime(fixedClock(tzInstant))
	ct.names = clock.Namer{
		Getenv:   func(string) string { return "" },
		Readlink: func(string) (string, error) { return "/var/db/timezone/zoneinfo/America/Argentina/Buenos_Aires", nil },
	}
	out, isErr := ct.Execute(context.Background(), `{}`)
	if isErr || !strings.Contains(out, "(America/Argentina/Buenos_Aires)") {
		t.Fatalf("default-zone output = %q (isErr=%v), want the linked IANA name", out, isErr)
	}
}
