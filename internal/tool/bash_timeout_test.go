package tool

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestBashSchemaHasOptionalTimeoutSeconds(t *testing.T) {
	var schema struct {
		Properties map[string]struct {
			Type string `json:"type"`
		} `json:"properties"`
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(NewBash("").Definition().InputSchema, &schema); err != nil {
		t.Fatal(err)
	}
	if got := schema.Properties["timeout_seconds"].Type; got != "integer" {
		t.Fatalf("timeout_seconds type = %q, want integer", got)
	}
	if !slices.Contains(schema.Required, "command") || slices.Contains(schema.Required, "timeout_seconds") {
		t.Fatalf("required = %v, want only command", schema.Required)
	}
}

func TestBashDefaultTimeoutUsedWhenFieldAbsent(t *testing.T) {
	b := NewBash("")
	b.Timeout = time.Second
	out, isErr := b.Execute(context.Background(), mustJSON(t, map[string]string{"command": "sleep 3"}))
	want := "command timed out after 1s; rerun with timeout_seconds up to 600 if it needs longer"
	if !isErr || !strings.Contains(out, want) {
		t.Fatalf("got (%q, %v), want timeout naming %q", out, isErr, want)
	}
}

func TestBashExplicitTimeoutSecondsOverridesDefault(t *testing.T) {
	b := NewBash("")
	b.Timeout = 30 * time.Second
	start := time.Now()
	out, isErr := b.Execute(context.Background(), mustJSON(t, map[string]any{"command": "sleep 2", "timeout_seconds": 1}))
	if !isErr || !strings.Contains(out, "command timed out after 1s; rerun with timeout_seconds up to 600") {
		t.Fatalf("got (%q, %v), want a 1s timeout", out, isErr)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("explicit timeout_seconds did not bound the command")
	}
}

func TestBashTimeoutSecondsClampedToMaximum(t *testing.T) {
	out, isErr := NewBash("").Execute(context.Background(), mustJSON(t, map[string]any{"command": "echo ok", "timeout_seconds": 900}))
	if isErr {
		t.Fatalf("clamped command should run, got (%q, %v)", out, isErr)
	}
	if !strings.HasPrefix(out, "[bash: timeout_seconds 900 clamped to 600]\n") || !strings.Contains(out, "ok") {
		t.Fatalf("output = %q, want clamp line then the result", out)
	}
}

func TestBashTimeoutSecondsRejectsNonPositive(t *testing.T) {
	for _, v := range []int{0, -5} {
		out, isErr := NewBash("").Execute(context.Background(), mustJSON(t, map[string]any{"command": "echo ran", "timeout_seconds": v}))
		if !isErr || !strings.Contains(out, "timeout_seconds must be a positive integer") || strings.Contains(out, "ran") {
			t.Fatalf("timeout_seconds %d: got (%q, %v), want an error that runs nothing", v, out, isErr)
		}
	}
}
