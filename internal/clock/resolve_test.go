package clock

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// fakeHost is a Namer whose environment and /etc/localtime link are fixed.
func fakeHost(tz, link string, linkErr error) Namer {
	return Namer{
		Getenv: func(k string) string {
			if k == "TZ" {
				return tz
			}
			return ""
		},
		Readlink: func(path string) (string, error) {
			if path != "/etc/localtime" {
				return "", errors.New("unexpected path " + path)
			}
			return link, linkErr
		},
	}
}

// requireSystemLocal skips when this process runs with a named Local zone (a TZ
// set at start), because the resolution then starts from that name.
func requireSystemLocal(t *testing.T) {
	t.Helper()
	if time.Local.String() != "Local" {
		t.Skipf("time.Local is named %q on this host", time.Local.String())
	}
}

func TestNameResolvesTheLocalZoneFromItsLinkOrTZ(t *testing.T) {
	requireSystemLocal(t)
	cases := []struct {
		name string
		host Namer
		want string
	}{
		{"macOS link", fakeHost("", "/var/db/timezone/zoneinfo/America/Argentina/Buenos_Aires", nil), "America/Argentina/Buenos_Aires"},
		{"Linux link", fakeHost("", "/usr/share/zoneinfo/Europe/Paris", nil), "Europe/Paris"},
		{"relative link", fakeHost("", "../usr/share/zoneinfo/UTC", nil), "UTC"},
		{"TZ wins over the link", fakeHost("America/Argentina/Buenos_Aires", "/usr/share/zoneinfo/Europe/Paris", nil), "America/Argentina/Buenos_Aires"},
		{"TZ with the colon form", fakeHost(":Asia/Tokyo", "/usr/share/zoneinfo/Europe/Paris", nil), "Asia/Tokyo"},
		{"unloadable TZ falls back to the link", fakeHost("Mars/Olympus", "/usr/share/zoneinfo/Europe/Paris", nil), "Europe/Paris"},
		{"missing symlink", fakeHost("", "", errors.New("no such file")), ""},
		{"link without zoneinfo", fakeHost("", "/opt/localtime-custom", nil), ""},
		{"bogus zone in the link", fakeHost("", "/usr/share/zoneinfo/Mars/Olympus", nil), ""},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.host.Name(time.Local); got != tt.want {
				t.Fatalf("Name() = %q, want %q", got, tt.want)
			}
		})
	}
}

// Only time.Local is resolved from the host. A named or fixed zone keeps its own name.
func TestNameLeavesOtherLocationsAlone(t *testing.T) {
	host := fakeHost("Asia/Tokyo", "/usr/share/zoneinfo/Europe/Paris", nil)
	ba, err := time.LoadLocation("America/Argentina/Buenos_Aires")
	if err != nil {
		t.Fatal(err)
	}
	if got := host.Name(ba); got != "America/Argentina/Buenos_Aires" {
		t.Fatalf("named zone resolved to %q", got)
	}
	if got := host.Name(time.FixedZone("", 3600)); got != "" {
		t.Fatalf("fixed unnamed zone resolved to %q; it must not borrow the host zone", got)
	}
}

func TestPromptLineUsesTheResolvedLocalName(t *testing.T) {
	requireSystemLocal(t)
	host := fakeHost("", "/var/db/timezone/zoneinfo/America/Argentina/Buenos_Aires", nil)
	line := host.PromptLine(saturdayEvening, time.Local)
	if want := "America/Argentina/Buenos_Aires)"; !strings.Contains(line, want) {
		t.Fatalf("PromptLine = %q, want the IANA name %q", line, want)
	}
	noLink := fakeHost("", "", errors.New("missing"))
	if line := noLink.PromptLine(saturdayEvening, time.Local); strings.Contains(line, "/") {
		t.Fatalf("with no link the zone falls back to the abbreviation: %q", line)
	}
}
