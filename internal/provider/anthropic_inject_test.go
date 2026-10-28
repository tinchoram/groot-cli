package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/tinchoram/groot-cli/internal/api"
)

// TestInjectedMessageServiceIsUsed proves that a message service passed in
// through the options carries the call, not the client the adapter would build.
func TestInjectedMessageServiceIsUsed(t *testing.T) {
	var gotKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("X-Api-Key")
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, sse(
			messageStart,
			`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hi"}}`,
			`{"type":"content_block_stop","index":0}`,
			`{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":2}}`,
			`{"type":"message_stop"}`,
		))
	}))
	defer srv.Close()

	client := anthropic.NewClient(option.WithAPIKey("injected-key"), option.WithBaseURL(srv.URL), option.WithMaxRetries(0))
	a := NewAnthropic(AnthropicOptions{
		APIKey:     "unused-key",
		BaseURL:    "http://127.0.0.1:1",
		MaxRetries: -1,
		Messages:   &client.Messages,
	})

	resp, err := a.Send(context.Background(), Request{Messages: []api.Message{{Role: api.RoleUser, Content: []api.Block{{Type: api.BlockText, Text: "hello"}}}}})
	if err != nil {
		t.Fatalf("Send through injected service: %v", err)
	}
	if gotKey != "injected-key" {
		t.Fatalf("request carried key %q, want the injected service's key", gotKey)
	}
	if len(resp.Content) != 1 || resp.Content[0].Text != "hi" {
		t.Fatalf("response = %+v, want one text block %q", resp.Content, "hi")
	}
}
