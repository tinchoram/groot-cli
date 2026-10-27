package api

import "testing"

func TestValidEffortAcceptsTheFiveLevelsAndUnset(t *testing.T) {
	for _, v := range []string{"", "low", "medium", "high", "xhigh", "max"} {
		if !ValidEffort(v) {
			t.Errorf("ValidEffort(%q) = false, want true", v)
		}
	}
	for _, v := range []string{"turbo", "HIGH", " high", "none"} {
		if ValidEffort(v) {
			t.Errorf("ValidEffort(%q) = true, want false", v)
		}
	}
}
