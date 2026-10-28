// Package provider is the port to language models. Adapters live beside this
// file: anthropic.go (Anthropic Messages API), gemini.go (Gemini Developer API),
// mock.go (scripted, for tests). router.go sends each call to the adapter of
// the model's provider.
//
// TODO(phase-2): OpenAI-compatible, Ollama, Bedrock and Vertex adapters. See
// docs/ROADMAP.md. They are intentionally not implemented in this MVP.
package provider

import (
	"context"
	"errors"
	"fmt"

	"github.com/tinchoram/groot-cli/internal/api"
)

// Provider sends one turn to a model and returns its answer.
type Provider interface {
	Send(ctx context.Context, req Request) (api.Response, error)
	Model() string
	SetModel(name string)
}

// Request is one model call.
type Request struct {
	System   string
	Messages []api.Message
	Tools    []api.ToolDef
	OnText   func(delta string) // streaming text callback, may be nil
	// Effort overrides the provider's effort for this call. Empty uses the
	// provider's value, and an empty value there sends none.
	Effort string
}

// UsageReporter is implemented by providers that track token usage and cost.
type UsageReporter interface {
	Usage() api.Usage
	EstimatedCostUSD() float64
}

// ErrorKind classifies provider failures so callers can react (retry, wait, stop).
type ErrorKind int

const (
	KindOther ErrorKind = iota
	KindRateLimit
	KindAuth
	KindStatus
	KindConnection
)

// Error is a typed provider failure.
type Error struct {
	Kind    ErrorKind
	Status  int
	Message string
	Err     error
}

func (e *Error) Error() string {
	if e.Status != 0 {
		return fmt.Sprintf("provider error (HTTP %d): %s", e.Status, e.Message)
	}
	return "provider error: " + e.Message
}

func (e *Error) Unwrap() error { return e.Err }

// IsRateLimit reports whether err is a rate-limit failure.
func IsRateLimit(err error) bool {
	var pe *Error
	return errors.As(err, &pe) && pe.Kind == KindRateLimit
}
