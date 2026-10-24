package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPriceOfKnownAndUnknownModels(t *testing.T) {
	p, ok := PriceOf("claude-sonnet-5-5")
	if !ok || p.Input != 2 || p.Output != 10 {
		t.Fatalf("sonnet price = %+v (%v)", p, ok)
	}
	if _, ok := PriceOf("not-a-model"); ok {
		t.Fatal("an unknown model has no price")
	}
}

func TestAnthropicListModelsFollowsPages(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("after_id") == "" {
			fmt.Fprint(w, `{"data":[{"id":"claude-a","display_name":"Model A","created_at":"2025-01-01T00:00:00Z"}],"has_more":true,"first_id":"claude-a","last_id":"claude-a"}`)
			return
		}
		fmt.Fprint(w, `{"data":[{"id":"claude-b","display_name":"Model B","created_at":"2025-01-01T00:00:00Z"}],"has_more":false,"first_id":"claude-b","last_id":"claude-b"}`)
	}))
	defer srv.Close()

	a := NewAnthropic(AnthropicOptions{APIKey: "test-key", BaseURL: srv.URL, MaxRetries: -1})
	got, err := a.ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "claude-a" || got[0].DisplayName != "Model A" || got[1].ID != "claude-b" {
		t.Fatalf("models = %+v", got)
	}
}

func TestMockListsItsModels(t *testing.T) {
	m := NewMock()
	m.Models = []ModelInfo{{ID: "claude-x", DisplayName: "X"}}
	var l ModelLister = m
	got, err := l.ListModels(context.Background())
	if err != nil || len(got) != 1 || got[0].ID != "claude-x" {
		t.Fatalf("mock models = %+v (%v)", got, err)
	}
}
