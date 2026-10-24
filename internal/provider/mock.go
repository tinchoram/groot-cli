package provider

import (
	"context"
	"errors"
	"sync"

	"github.com/tinchoram/groot-cli/internal/api"
)

// Step is one scripted answer for Mock: either a response or an error.
type Step struct {
	Response api.Response
	Err      error
}

// Mock is a Provider that replays scripted steps. It records every request.
type Mock struct {
	mu     sync.Mutex
	model  string
	script []Step
	Calls  []Request
	usage  tracker
	// Models and ModelsErr answer ListModels.
	Models    []ModelInfo
	ModelsErr error
}

// NewMock returns a Mock that plays steps in order.
func NewMock(steps ...Step) *Mock {
	return &Mock{model: "mock-model", script: steps}
}

// Send records the request, streams text to OnText, and returns the next step.
func (m *Mock) Send(_ context.Context, req Request) (api.Response, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, req)
	if len(m.script) == 0 {
		return api.Response{}, errors.New("mock: no scripted response left")
	}
	step := m.script[0]
	m.script = m.script[1:]
	if step.Err != nil {
		return api.Response{}, step.Err
	}
	if req.OnText != nil {
		for _, b := range step.Response.Content {
			if b.Type == api.BlockText && b.Text != "" {
				req.OnText(b.Text)
			}
		}
	}
	step.Response.Model = m.model
	m.usage.add(step.Response.Usage, "")
	return step.Response, nil
}

func (m *Mock) Model() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.model
}

func (m *Mock) SetModel(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.model = name
}

// Usage returns the accumulated token usage.
func (m *Mock) Usage() api.Usage {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.usage.total
}

// EstimatedCostUSD returns zero: the mock has no price.
func (m *Mock) EstimatedCostUSD() float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.usage.cost
}
