package flow

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// FeatureDoc is the plan of a large task: numbered specs, the tasks that serve
// them and the log of the request. The agent writes it as Markdown, and the
// flow reads the numbered items back.
type FeatureDoc struct {
	Specs []Spec
	Tasks []DocTask
	Log   []LogEntry
}

// Spec is one numbered requirement, S<N>.
type Spec struct {
	N    int
	Text string
}

// DocTask is one numbered task, T<N>, with the specs it serves.
type DocTask struct {
	N     int
	Specs []int
	Text  string
}

// LogEntry is one numbered line of the log, L<N>. L1 is the original request.
type LogEntry struct {
	N    int
	Text string
}

// FeatureDocTemplate is the shape a feature document must have. Validation
// errors carry it, so the agent can correct the document in one step.
const FeatureDocTemplate = `## Specs
S1. "<what the user asked, verbatim>"
## Tasks
T1. (S1) <what you will change>
## Log
L1. <the original request, verbatim>`

var (
	headingRe = regexp.MustCompile(`^#{1,6}\s+(.*?)\s*$`)
	itemRe    = regexp.MustCompile(`^([STL])(\d+)\.\s*(.*)$`)
	specRefRe = regexp.MustCompile(`^S(\d+)$`)
)

// rawItem is a numbered item as read from its section, before validation.
type rawItem struct {
	n    int
	text string
}

// ParseFeatureDoc reads a feature document. Sections are "## Specs", "## Tasks"
// and "## Log", in any order and in any letter case; other sections are
// ignored. An item starts with S<n>., T<n> or L<n>. at the start of a line and
// is numbered from 1 without gaps. A line indented by two spaces or a tab
// continues the item before it. A task names the specs it serves as (S1) or
// (S1, S3), and each of them must exist.
func ParseFeatureDoc(src string) (FeatureDoc, error) {
	var (
		section string
		seen    = map[string]bool{}
		specs   []*rawItem
		tasks   []*rawItem
		logs    []*rawItem
		refs    = map[*rawItem][]int{}
		last    *rawItem
	)
	for i, line := range strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n") {
		where := fmt.Sprintf("line %d", i+1)
		if m := headingRe.FindStringSubmatch(line); m != nil {
			section, last = "", nil
			name := strings.ToLower(m[1])
			switch name {
			case "specs", "tasks", "log":
				if seen[name] {
					return FeatureDoc{}, docError(where + ": section ## " + m[1] + " appears twice")
				}
				seen[name], section = true, name
			}
			continue
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || section == "" {
			continue
		}
		if strings.HasPrefix(line, "  ") || strings.HasPrefix(line, "\t") {
			if last != nil {
				last.text += " " + trimmed
			}
			continue
		}
		m := itemRe.FindStringSubmatch(line)
		if m == nil {
			last = nil
			continue
		}
		letter, n, text := m[1], mustAtoi(m[2]), strings.TrimSpace(m[3])
		if !sectionTakes(section, letter) {
			return FeatureDoc{}, docError(fmt.Sprintf("%s: %s%d. is in ## %s; %s items go under ## %s",
				where, letter, n, title(section), letter, title(sectionOf(letter))))
		}
		var list *[]*rawItem
		switch letter {
		case "S":
			list = &specs
		case "T":
			list = &tasks
		default:
			list = &logs
		}
		want := len(*list) + 1
		if n != want {
			return FeatureDoc{}, docError(fmt.Sprintf("%s: %s%d is out of order, expected %s%d", where, letter, n, letter, want))
		}
		item := &rawItem{n: n, text: text}
		*list = append(*list, item)
		last = item
		if letter == "T" {
			taskRefs, rest, err := parseRefs(text)
			if err != nil {
				return FeatureDoc{}, docError(fmt.Sprintf("%s: T%d %s", where, n, err))
			}
			refs[item] = taskRefs
			item.text = rest
		}
		if item.text == "" {
			return FeatureDoc{}, docError(fmt.Sprintf("%s: %s%d has no text", where, letter, n))
		}
	}

	switch {
	case len(specs) == 0:
		return FeatureDoc{}, docError("the feature document needs at least one spec in ## Specs")
	case len(tasks) == 0:
		return FeatureDoc{}, docError("the feature document needs at least one task in ## Tasks")
	case len(logs) == 0:
		return FeatureDoc{}, docError("the feature document needs L1, the original request, in ## Log")
	}
	var doc FeatureDoc
	for _, s := range specs {
		doc.Specs = append(doc.Specs, Spec{N: s.n, Text: s.text})
	}
	for _, t := range tasks {
		for _, r := range refs[t] {
			if r < 1 || r > len(specs) {
				return FeatureDoc{}, docError(fmt.Sprintf("T%d references S%d, which does not exist: specs are S1..S%d", t.n, r, len(specs)))
			}
		}
		doc.Tasks = append(doc.Tasks, DocTask{N: t.n, Specs: refs[t], Text: t.text})
	}
	for _, l := range logs {
		doc.Log = append(doc.Log, LogEntry{N: l.n, Text: l.text})
	}
	return doc, nil
}

// parseRefs reads the "(S1, S3)" prefix of a task and returns the spec numbers
// with the rest of the text.
func parseRefs(text string) ([]int, string, error) {
	if !strings.HasPrefix(text, "(") {
		return nil, "", errors.New("must name the specs it serves, for example (S1)")
	}
	end := strings.Index(text, ")")
	if end < 0 {
		return nil, "", errors.New("has an unclosed spec list, for example (S1)")
	}
	var refs []int
	for _, part := range strings.Split(text[1:end], ",") {
		m := specRefRe.FindStringSubmatch(strings.TrimSpace(part))
		if m == nil {
			return nil, "", fmt.Errorf("has a bad spec reference %q, use S<n>", strings.TrimSpace(part))
		}
		refs = append(refs, mustAtoi(m[1]))
	}
	return refs, strings.TrimSpace(text[end+1:]), nil
}

// sectionTakes reports whether a section holds items with this letter.
func sectionTakes(section, letter string) bool {
	return sectionOf(letter) == section
}

func sectionOf(letter string) string {
	switch letter {
	case "S":
		return "specs"
	case "T":
		return "tasks"
	}
	return "log"
}

func title(section string) string {
	return strings.ToUpper(section[:1]) + section[1:]
}

func mustAtoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

// docError is a validation error that carries the template.
func docError(msg string) error {
	return fmt.Errorf("%s\n%s", msg, FeatureDocTemplate)
}
