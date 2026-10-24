// Package feedback keeps the backlog of bugs and feature requests about groot.
// Each item is a Markdown file with frontmatter under items/, and BACKLOG.md is
// an index regenerated after every change. Pure file storage, no model calls.
package feedback

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Types of item.
const (
	TypeBug  = "bug"
	TypeFeat = "feat"
)

// Statuses of item.
const (
	StatusOpen       = "open"
	StatusInProgress = "in_progress"
	StatusDone       = "done"
	StatusWontfix    = "wontfix"
)

// Item is one entry of the backlog.
type Item struct {
	ID         string
	Type       string
	Status     string
	Area       string
	Title      string
	Body       string
	Notes      string
	Space      string
	Agent      string
	Version    string
	ReportedAt time.Time
	Context    []string // the last plain transcript lines, when captured from a chat
}

// Store is the backlog in one directory.
type Store struct {
	dir string
}

// Open creates the backlog directory if needed and returns the store.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(filepath.Join(dir, "items"), 0o755); err != nil {
		return nil, fmt.Errorf("create backlog: %w", err)
	}
	return &Store{dir: dir}, nil
}

// Report saves a new item. The id counter is per type: BUG-001, FEAT-001, ...
func (s *Store) Report(it Item) (Item, error) {
	if it.Type != TypeBug && it.Type != TypeFeat {
		return Item{}, fmt.Errorf("type %q is not bug or feat", it.Type)
	}
	if strings.TrimSpace(it.Title) == "" {
		return Item{}, errors.New("a title is required")
	}
	items, err := s.load()
	if err != nil {
		return Item{}, err
	}
	next := 1
	for _, o := range items {
		if o.Type == it.Type {
			if n := idNumber(o.ID); n >= next {
				next = n + 1
			}
		}
	}
	it.ID = fmt.Sprintf("%s-%03d", strings.ToUpper(it.Type), next)
	it.Status = StatusOpen
	if it.ReportedAt.IsZero() {
		it.ReportedAt = time.Now().UTC()
	}
	if err := s.write(it); err != nil {
		return Item{}, err
	}
	return it, s.reindex()
}

// Path returns the file of an item, or "" when there is none.
func (s *Store) Path(id string) string {
	p, _ := s.find(id)
	return p
}

// Get returns one item by id.
func (s *Store) Get(id string) (Item, error) {
	items, err := s.load()
	if err != nil {
		return Item{}, err
	}
	for _, it := range items {
		if it.ID == id {
			return it, nil
		}
	}
	return Item{}, fmt.Errorf("no item %s", id)
}

// List returns the items with the given status, or every item when status is "".
// Newest id first within the status.
func (s *Store) List(status string) ([]Item, error) {
	items, err := s.load()
	if err != nil {
		return nil, err
	}
	var out []Item
	for _, it := range items {
		if status == "" || it.Status == status {
			out = append(out, it)
		}
	}
	sort.Slice(out, func(i, j int) bool { return sortKey(out[i].ID) < sortKey(out[j].ID) })
	return out, nil
}

// Update sets the status of an item and appends an optional note.
func (s *Store) Update(id, status, note string) error {
	switch status {
	case StatusOpen, StatusInProgress, StatusDone, StatusWontfix:
	default:
		return fmt.Errorf("status %q is not valid (open, in_progress, done, wontfix)", status)
	}
	it, err := s.Get(id)
	if err != nil {
		return err
	}
	it.Status = status
	if n := strings.TrimSpace(note); n != "" {
		it.Notes = strings.TrimSpace(it.Notes + "\n- " + time.Now().UTC().Format("2006-01-02") + ": " + n)
	}
	if err := s.write(it); err != nil {
		return err
	}
	return s.reindex()
}

// load reads every item file.
func (s *Store) load() ([]Item, error) {
	entries, err := os.ReadDir(filepath.Join(s.dir, "items"))
	if err != nil {
		return nil, fmt.Errorf("read backlog: %w", err)
	}
	var out []Item
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(s.dir, "items", e.Name()))
		if err != nil {
			return nil, err
		}
		it, err := parse(string(raw))
		if err != nil {
			return nil, fmt.Errorf("item %s: %w", e.Name(), err)
		}
		out = append(out, it)
	}
	return out, nil
}

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

func slug(title string) string {
	s := slugRe.ReplaceAllString(strings.ToLower(title), "-")
	s = strings.Trim(s, "-")
	if len(s) > 40 {
		s = strings.Trim(s[:40], "-")
	}
	if s == "" {
		s = "item"
	}
	return s
}

func (s *Store) path(it Item) string {
	return filepath.Join(s.dir, "items", it.ID+"-"+slug(it.Title)+".md")
}

// write saves the item file, replacing an older file of the same id.
func (s *Store) write(it Item) error {
	if old, ok := s.find(it.ID); ok && old != s.path(it) {
		if err := os.Remove(old); err != nil {
			return err
		}
	}
	return os.WriteFile(s.path(it), []byte(render(it)), 0o644)
}

// find returns the file of an id, if it exists.
func (s *Store) find(id string) (string, bool) {
	matches, _ := filepath.Glob(filepath.Join(s.dir, "items", id+"-*.md"))
	if len(matches) == 0 {
		return "", false
	}
	return matches[0], true
}

func render(it Item) string {
	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "id: %s\n", it.ID)
	fmt.Fprintf(&b, "type: %s\n", it.Type)
	fmt.Fprintf(&b, "status: %s\n", it.Status)
	fmt.Fprintf(&b, "area: %s\n", it.Area)
	fmt.Fprintf(&b, "title: %s\n", strconv.Quote(it.Title))
	fmt.Fprintf(&b, "reported_at: %s\n", it.ReportedAt.UTC().Format(time.RFC3339))
	fmt.Fprintf(&b, "space: %s\n", it.Space)
	fmt.Fprintf(&b, "agent: %s\n", it.Agent)
	fmt.Fprintf(&b, "version: %s\n", it.Version)
	b.WriteString("---\n\n")
	if it.Body != "" {
		b.WriteString(strings.TrimSpace(it.Body) + "\n")
	}
	if len(it.Context) > 0 {
		b.WriteString("\n## Context\n\n")
		for _, line := range it.Context {
			b.WriteString(line + "\n")
		}
	}
	if it.Notes != "" {
		b.WriteString("\n## Notes\n\n" + it.Notes + "\n")
	}
	return b.String()
}

// parse reads an item file written by render.
func parse(raw string) (Item, error) {
	var it Item
	lines := strings.Split(raw, "\n")
	if len(lines) == 0 || lines[0] != "---" {
		return it, errors.New("missing frontmatter")
	}
	end := 0
	for i := 1; i < len(lines); i++ {
		if lines[i] == "---" {
			end = i
			break
		}
		key, value, ok := strings.Cut(lines[i], ": ")
		if !ok {
			continue
		}
		switch key {
		case "id":
			it.ID = value
		case "type":
			it.Type = value
		case "status":
			it.Status = value
		case "area":
			it.Area = value
		case "title":
			it.Title, _ = strconv.Unquote(value)
		case "reported_at":
			it.ReportedAt, _ = time.Parse(time.RFC3339, value)
		case "space":
			it.Space = value
		case "agent":
			it.Agent = value
		case "version":
			it.Version = value
		}
	}
	if end == 0 {
		return it, errors.New("frontmatter is not closed")
	}
	body := strings.Join(lines[end+1:], "\n")
	var sections []string
	section := ""
	var buf []string
	flush := func() {
		text := strings.TrimSpace(strings.Join(buf, "\n"))
		switch section {
		case "":
			it.Body = text
		case "Context":
			it.Context = nonEmpty(buf)
		case "Notes":
			it.Notes = text
		}
		buf = nil
	}
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "## ") {
			flush()
			section = strings.TrimPrefix(line, "## ")
			sections = append(sections, section)
			continue
		}
		buf = append(buf, line)
	}
	flush()
	return it, nil
}

func nonEmpty(lines []string) []string {
	var out []string
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}

// reindex rewrites BACKLOG.md from the items.
func (s *Store) reindex() error {
	items, err := s.load()
	if err != nil {
		return err
	}
	sort.Slice(items, func(i, j int) bool { return sortKey(items[i].ID) < sortKey(items[j].ID) })
	var b strings.Builder
	b.WriteString("# Groot backlog\n\nGenerated from the items under items/. Edit the items, not this file.\n\n")
	b.WriteString("| ID | type | status | area | title | reported |\n")
	b.WriteString("| --- | --- | --- | --- | --- | --- |\n")
	for _, it := range items {
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s |\n",
			it.ID, it.Type, it.Status, it.Area, strings.ReplaceAll(it.Title, "|", "/"), it.ReportedAt.Format("2006-01-02"))
	}
	return os.WriteFile(filepath.Join(s.dir, "BACKLOG.md"), []byte(b.String()), 0o644)
}

// idNumber is the number of an id such as BUG-012.
func idNumber(id string) int {
	_, num, _ := strings.Cut(id, "-")
	n, _ := strconv.Atoi(num)
	return n
}

// sortKey orders bugs before features, then by number.
func sortKey(id string) string {
	typ, _, _ := strings.Cut(id, "-")
	return fmt.Sprintf("%s-%06d", typ, idNumber(id))
}

func readFile(path string) (string, error) {
	raw, err := os.ReadFile(path)
	return string(raw), err
}
