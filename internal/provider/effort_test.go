package provider

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/tinchoram/groot-cli/internal/api"
)

func effortRequest() Request {
	return Request{Messages: []api.Message{{Role: api.RoleUser, Content: []api.Block{{Type: api.BlockText, Text: "x"}}}}}
}

func TestBuildParamsSendsEffortWhenSet(t *testing.T) {
	req := effortRequest()
	req.Effort = "high"
	params, err := buildParams(req, "claude-sonnet-5-5", 1024)
	if err != nil {
		t.Fatal(err)
	}
	if string(params.OutputConfig.Effort) != "high" {
		t.Fatalf("output_config.effort = %q, want high", params.OutputConfig.Effort)
	}
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"output_config":{"effort":"high"}`) {
		t.Fatalf("request body lacks output_config.effort: %s", raw)
	}
}

func TestBuildParamsOmitsEffortWhenEmpty(t *testing.T) {
	params, err := buildParams(effortRequest(), "claude-sonnet-5-5", 1024)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "output_config") || strings.Contains(string(raw), "effort") {
		t.Fatalf("an unset effort must not be sent: %s", raw)
	}
}

func TestProviderEffortIsSentAndRequestEffortWins(t *testing.T) {
	f := &fakeAPI{reply: sse(messageStart, `{"type":"message_stop"}`)}
	a, done := newTestProvider(t, f, 0)
	defer done()
	a.effort = "low"

	_, _ = a.Send(context.Background(), effortRequest())
	if !strings.Contains(f.bodies[0], `"output_config":{"effort":"low"}`) {
		t.Fatalf("provider effort not sent: %s", f.bodies[0])
	}

	req := effortRequest()
	req.Effort = "max"
	_, _ = a.Send(context.Background(), req)
	if !strings.Contains(f.bodies[1], `"output_config":{"effort":"max"}`) {
		t.Fatalf("request effort must win over the provider's: %s", f.bodies[1])
	}
}

func TestNewAnthropicTakesEffortFromOptions(t *testing.T) {
	a := NewAnthropic(AnthropicOptions{APIKey: "k", Effort: "medium"})
	if a.effort != "medium" {
		t.Fatalf("effort = %q, want medium", a.effort)
	}
}
