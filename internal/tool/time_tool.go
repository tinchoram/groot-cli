package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/tinchoram/groot-cli/internal/api"
	"github.com/tinchoram/groot-cli/internal/clock"
)

// CurrentTime reports the current date, time and zone. It needs no approval:
// it reads the clock of the process and nothing else.
type CurrentTime struct {
	now   func() time.Time
	local *time.Location
	// names resolves the IANA name of the local zone. Tests inject the host.
	names clock.Namer
}

// NewCurrentTime returns the current_time tool. A nil now reads the system clock.
func NewCurrentTime(now func() time.Time) *CurrentTime {
	if now == nil {
		now = time.Now
	}
	return &CurrentTime{now: now, local: time.Local, names: clock.System}
}

type currentTimeInput struct {
	Timezone string `json:"timezone"`
}

func (*CurrentTime) Definition() api.ToolDef {
	return api.ToolDef{
		Name:        "current_time",
		Description: "Return the current date, time and zone. Use it whenever an answer depends on today's date or the time of day, instead of guessing. Pass an IANA timezone such as America/Argentina/Buenos_Aires to see that zone; with none, the local time of this machine is returned. It only reads this machine's clock.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"timezone":{"type":"string","description":"IANA timezone name, such as America/Argentina/Buenos_Aires; empty for local time"}}}`),
	}
}

// Risk is RiskRead: the tool reads the clock only.
func (*CurrentTime) Risk() Risk { return RiskRead }

// RiskFor is the static risk.
func (c *CurrentTime) RiskFor(string) Risk { return c.Risk() }

func (*CurrentTime) Preview(string) string { return "current_time" }

func (c *CurrentTime) Execute(_ context.Context, input string) (string, bool) {
	var in currentTimeInput
	if strings.TrimSpace(input) != "" {
		if err := json.Unmarshal([]byte(input), &in); err != nil {
			return "invalid input: " + err.Error(), true
		}
	}
	now := c.now()
	tz := strings.TrimSpace(in.Timezone)
	if tz == "" {
		return c.describe(now, c.local), false
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return "unknown timezone: " + tz, true
	}
	return c.describe(now, loc) + "\nlocal: " + c.names.Stamp(now, c.local) + ", " + now.In(c.local).Weekday().String(), false
}

// describe is "<stamp>, <weekday>, unix <seconds>" in loc.
func (c *CurrentTime) describe(now time.Time, loc *time.Location) string {
	return fmt.Sprintf("%s, %s, unix %d", c.names.Stamp(now, loc), now.In(loc).Weekday(), now.Unix())
}
