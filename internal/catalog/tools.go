package catalog

import (
	"fmt"
	"slices"
	"strings"
)

// EditTools adds and removes tools of an agent definition. Added tools must be
// known tools; a tool in required cannot be removed. Existing tools keep their
// order, new ones are appended, and duplicates are ignored. The input is not
// changed.
func EditTools(d AgentDef, add, remove, required []string) (AgentDef, error) {
	for _, t := range add {
		if !slices.Contains(KnownTools, t) {
			return d, fmt.Errorf("unknown tool %q", t)
		}
	}
	tools := slices.Clone(d.Tools)
	for _, t := range remove {
		if slices.Contains(required, t) {
			return d, fmt.Errorf("%s is required for %s and cannot be removed", t, d.Name)
		}
		tools = slices.DeleteFunc(tools, func(x string) bool { return x == t })
	}
	for _, t := range add {
		if !slices.Contains(tools, t) {
			tools = append(tools, t)
		}
	}
	d.Tools = tools
	return d, nil
}

// SplitTools reads tool names given as words, comma-separated or both.
func SplitTools(args []string) []string {
	var out []string
	for _, a := range args {
		for _, t := range strings.FieldsFunc(a, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' }) {
			out = append(out, t)
		}
	}
	return out
}
