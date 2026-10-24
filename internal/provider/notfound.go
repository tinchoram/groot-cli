package provider

import (
	"errors"
	"strings"
)

// IsModelNotFound reports a provider failure that says the requested model does
// not exist: an HTTP 404 whose message names the model.
func IsModelNotFound(err error) bool {
	var pe *Error
	if !errors.As(err, &pe) || pe.Status != 404 {
		return false
	}
	return strings.Contains(strings.ToLower(pe.Message), "model")
}
