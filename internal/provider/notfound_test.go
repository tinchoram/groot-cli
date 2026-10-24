package provider

import (
	"errors"
	"fmt"
	"testing"
)

func TestIsModelNotFound(t *testing.T) {
	cases := map[string]struct {
		err  error
		want bool
	}{
		"model 404":     {&Error{Kind: KindStatus, Status: 404, Message: "not_found_error: model: list"}, true},
		"wrapped model": {fmt.Errorf("turn: %w", &Error{Kind: KindStatus, Status: 404, Message: "model: x"}), true},
		"other 404":     {&Error{Kind: KindStatus, Status: 404, Message: "route not found"}, false},
		"model 500":     {&Error{Kind: KindStatus, Status: 500, Message: "model overloaded"}, false},
		"plain error":   {errors.New("model: list"), false},
		"nil":           {nil, false},
	}
	for name, c := range cases {
		if got := IsModelNotFound(c.err); got != c.want {
			t.Fatalf("%s: IsModelNotFound = %v, want %v", name, got, c.want)
		}
	}
}
