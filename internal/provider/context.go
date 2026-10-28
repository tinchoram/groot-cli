package provider

import (
	"errors"
	"strings"
)

// DefaultContextWindow is the window of a model that is not in the table and
// has no override: the size of Claude models.
const DefaultContextWindow = 200_000

// contextWindows holds the context window of each known model, keyed by model
// id prefix. It sits next to the price table; models.context_window in
// groot.yaml overrides it per model or prefix.
var contextWindows = map[string]int{
	"claude-opus-5-5":   DefaultContextWindow,
	"claude-sonnet-5-5": DefaultContextWindow,
	"claude-haiku-5-5":  DefaultContextWindow,
	// Gemini input limit, from the model pages on ai.google.dev (checked 2026-10-28).
	"gemini-4-argon":   1_048_576,
	"gemini-3.8-flash": 1_048_576,
}

// ContextWindow returns the context window of model in tokens. An override
// keyed by the model id or a prefix of it wins over the table; the longest
// matching key wins. A model with neither gets DefaultContextWindow.
func ContextWindow(model string, overrides map[string]int) int {
	if w, ok := longestPrefix(overrides, model); ok {
		return w
	}
	if w, ok := longestPrefix(contextWindows, model); ok {
		return w
	}
	return DefaultContextWindow
}

// longestPrefix returns the value of the longest key of m that model starts with.
func longestPrefix(m map[string]int, model string) (int, bool) {
	best, found := "", false
	var value int
	for key, v := range m {
		if key == "" || !strings.HasPrefix(model, key) {
			continue
		}
		if !found || len(key) > len(best) {
			best, value, found = key, v, true
		}
	}
	return value, found
}

// ContextPercent is tokens as a whole percentage of window, rounded down. It
// is 0 when the window is unknown.
func ContextPercent(tokens int64, window int) int {
	if window <= 0 || tokens <= 0 {
		return 0
	}
	return int(tokens * 100 / int64(window))
}

// IsContextOverflow reports whether err is the provider refusing a request
// because the prompt does not fit the model's context window. Anthropic answers
// with HTTP 400 and an invalid_request_error whose message reads "prompt is too
// long: N tokens > M maximum".
func IsContextOverflow(err error) bool {
	var pe *Error
	if !errors.As(err, &pe) || pe == nil {
		return false
	}
	return pe.Status == 400 && strings.Contains(strings.ToLower(pe.Message), "prompt is too long")
}
