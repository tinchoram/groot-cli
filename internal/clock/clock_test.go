package clock

import (
	"testing"
	"time"
	_ "time/tzdata" // the zone names below must resolve on every host
)

func mustLoad(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

// saturdayEvening is 2026-10-25 18:42:07 in Buenos Aires (-03), 21:42:07 UTC.
var saturdayEvening = time.Date(2026, 10, 25, 21, 42, 7, 0, time.UTC)

func TestPromptLineNamesTheZoneAndDay(t *testing.T) {
	ba := mustLoad(t, "America/Argentina/Buenos_Aires")
	cases := []struct {
		name string
		loc  *time.Location
		want string
	}{
		{"iana name", ba, "Current time: 2026-10-25 18:42 (-03, America/Argentina/Buenos_Aires), Sunday."},
		{"utc", time.UTC, "Current time: 2026-10-25 21:42 (+00, UTC), Sunday."},
		{"fixed zone without name", time.FixedZone("", 5*3600+30*60), "Current time: 2026-10-26 03:12 (+05:30), Monday."},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := PromptLine(saturdayEvening, tt.loc); got != tt.want {
				t.Fatalf("PromptLine() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestStampCarriesSecondsAndOffset(t *testing.T) {
	ba := mustLoad(t, "America/Argentina/Buenos_Aires")
	got := Stamp(saturdayEvening, ba)
	want := "2026-10-25 18:42:07 -03 (America/Argentina/Buenos_Aires)"
	if got != want {
		t.Fatalf("Stamp() = %q, want %q", got, want)
	}
}
