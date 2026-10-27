package api

import "slices"

// Efforts are the levels of output_config.effort, from least to most thorough.
// An empty effort is not sent, so the model's own default applies.
var Efforts = []string{"low", "medium", "high", "xhigh", "max"}

// ValidEffort reports whether v is empty or one of Efforts. The match is exact.
func ValidEffort(v string) bool {
	return v == "" || slices.Contains(Efforts, v)
}
