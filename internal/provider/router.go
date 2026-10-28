package provider

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/tinchoram/groot-cli/internal/api"
)

// Provider names, as used in provider.name and in the model catalog.
const (
	NameAnthropic = "anthropic"
	NameGemini    = "gemini"
)

// NameOf returns the provider that serves model. A recognised prefix decides
// (gemini- is Gemini, claude- is Anthropic); any other id, including an empty
// one, gets fallback.
func NameOf(model, fallback string) string {
	switch {
	case strings.HasPrefix(model, "gemini-"):
		return NameGemini
	case strings.HasPrefix(model, "claude-"):
		return NameAnthropic
	}
	return fallback
}

// Builder creates a provider of the named kind for model. An empty model asks
// for the provider's own default.
type Builder func(name, model string) Provider

// RouterOptions configure a Router.
type RouterOptions struct {
	// Default is the provider for a model id without a recognised prefix.
	Default string
	// Model is the first model of the conversation. Empty means Default's own.
	Model string
	// Keyed lists the providers that have a key, in catalog order. The catalog
	// merges all of them.
	Keyed []string
	Build Builder
}

// Router is a Provider that sends each call to the provider that serves the
// current model. A conversation whose model moves from a claude- id to a
// gemini- id therefore gets a Gemini provider, and the usage of each provider
// is kept and summed. Router implements UsageReporter and ModelLister.
type Router struct {
	fallback string
	keyed    []string
	build    Builder

	mu       sync.Mutex
	model    string
	children map[string]Provider
}

// NewRouter builds the router and the provider for its first model. It does
// not contact any API.
func NewRouter(o RouterOptions) *Router {
	r := &Router{
		fallback: o.Default,
		keyed:    o.Keyed,
		build:    o.Build,
		children: map[string]Provider{},
	}
	if r.fallback == "" {
		r.fallback = NameAnthropic
	}
	name := NameOf(o.Model, r.fallback)
	child := r.build(name, o.Model)
	r.children[name] = child
	r.model = child.Model()
	return r
}

// childLocked returns the provider of kind name, building it on first use.
func (r *Router) childLocked(name, model string) Provider {
	if p, ok := r.children[name]; ok {
		return p
	}
	p := r.build(name, model)
	r.children[name] = p
	return p
}

// Model returns the model used for the next call.
func (r *Router) Model() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.model
}

// SetModel changes the model for subsequent calls. A model of another provider
// selects, and if needed builds, that provider.
func (r *Router) SetModel(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	kind := NameOf(name, r.fallback)
	r.childLocked(kind, name).SetModel(name)
	r.model = name
}

// Send runs the call on the provider of the current model.
func (r *Router) Send(ctx context.Context, req Request) (api.Response, error) {
	r.mu.Lock()
	child := r.childLocked(NameOf(r.model, r.fallback), r.model)
	r.mu.Unlock()
	return child.Send(ctx, req)
}

// Usage sums the usage of every provider the router has used.
func (r *Router) Usage() api.Usage {
	var total api.Usage
	for _, p := range r.snapshot() {
		if u, ok := p.(UsageReporter); ok {
			total.Add(u.Usage())
		}
	}
	return total
}

// EstimatedCostUSD sums the estimated cost of every provider the router has used.
func (r *Router) EstimatedCostUSD() float64 {
	var sum float64
	for _, p := range r.snapshot() {
		if u, ok := p.(UsageReporter); ok {
			sum += u.EstimatedCostUSD()
		}
	}
	return sum
}

func (r *Router) snapshot() []Provider {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Provider, 0, len(r.children))
	for _, p := range r.children {
		out = append(out, p)
	}
	return out
}

// ListModels merges the lists of every keyed provider, each entry tagged with
// its provider name. A provider that fails is left out; the error is returned
// only when no provider could list.
func (r *Router) ListModels(ctx context.Context) ([]ModelInfo, error) {
	names := r.keyed
	if len(names) == 0 {
		names = []string{r.fallback}
	}
	var out []ModelInfo
	var errs []error
	for _, name := range names {
		r.mu.Lock()
		child := r.childLocked(name, "")
		r.mu.Unlock()
		lister, ok := child.(ModelLister)
		if !ok {
			errs = append(errs, fmt.Errorf("%s cannot list its models", name))
			continue
		}
		models, err := lister.ListModels(ctx)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
			continue
		}
		for _, m := range models {
			m.Provider = name
			out = append(out, m)
		}
	}
	if len(out) == 0 && len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return out, nil
}
