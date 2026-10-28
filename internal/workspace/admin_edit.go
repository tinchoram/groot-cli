package workspace

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/tinchoram/groot-cli/internal/catalog"
	"github.com/tinchoram/groot-cli/internal/memory"
	"github.com/tinchoram/groot-cli/internal/space"
)

// AgentEdit is a change to a catalog agent. A nil field keeps its value and an
// empty NewName keeps the name.
type AgentEdit struct {
	Name        string
	NewName     string
	Description *string
	Prompt      *string
}

// The methods below change spaces and agents for the administrator agent and
// the /space and /agent commands. Each one writes to disk, then reads the state
// back from disk and reports that state, so a success message always matches
// what was saved.

// spacesFile is the spaces list on disk.
func (w *Workspace) spacesFile() string { return filepath.Join(w.opts.Home, "spaces.yaml") }

// rereadSpaces opens spaces.yaml from disk.
func (w *Workspace) rereadSpaces() (*space.Store, error) {
	return space.Open(w.spacesFile())
}

// diskAgent reads the catalog from disk and returns the named agent, if any.
func (w *Workspace) diskAgent(name string) (catalog.AgentDef, bool, error) {
	defs, err := catalog.Load(w.opts.Home)
	if err != nil {
		return catalog.AgentDef{}, false, err
	}
	for _, d := range defs {
		if d.Name == name {
			return d, true, nil
		}
	}
	return catalog.AgentDef{}, false, nil
}

// agentList is a comma-separated list of names, or "none".
func agentList(names []string) string {
	if len(names) == 0 {
		return "none"
	}
	return strings.Join(names, ", ")
}

// inSpace matches the conversation keys of one space.
func inSpace(name string) func(string) bool {
	return func(key string) bool {
		sp, _, _ := strings.Cut(key, "/")
		return sp == name
	}
}

// ofAgent matches the conversation keys of one agent, in any space.
func ofAgent(name string) func(string) bool {
	return func(key string) bool {
		_, agent, _ := strings.Cut(key, "/")
		return agent == name
	}
}

// endConversations ends the open conversations whose key matches, and drops
// their in-memory work mode and session approvals. Their sessions stay stored,
// so they resume on the next turn.
func (w *Workspace) endConversations(match func(key string) bool) error {
	w.mu.Lock()
	var convs []*Conversation
	for key, c := range w.convs {
		if match(key) {
			convs = append(convs, c)
			delete(w.convs, key)
		}
	}
	for key := range w.work {
		if match(key) {
			delete(w.work, key)
		}
	}
	for key := range w.grants {
		if match(key) {
			delete(w.grants, key)
		}
	}
	w.mu.Unlock()
	var errs []error
	for _, c := range convs {
		c.stopJobs()
		errs = append(errs, c.endLocal(context.Background()))
	}
	return errors.Join(errs...)
}

// hasDatabase reports whether a space has a memory database yet. Looking does
// not create one.
func (w *Workspace) hasDatabase(sp space.Space) bool {
	_, err := os.Stat(memoryPathFor(sp.Path, w.opts.Config.Memory.Path))
	return err == nil
}

// openTasksOf lists the open tasks of a space. A space without a database has
// none, and no database is created to find that out. The memory database is
// read alone, without the audit log, so a report never creates a file; stores
// that are already open are reused.
func (w *Workspace) openTasksOf(ctx context.Context, sp space.Space) ([]memory.TaskSummary, error) {
	if !w.hasDatabase(sp) {
		return nil, nil
	}
	w.mu.Lock()
	st, open := w.stores[sp.Path]
	w.mu.Unlock()
	if open {
		return st.mem.TaskSummaries(ctx, false, time.Time{})
	}
	mem, err := memory.OpenSQLite(memoryPathFor(sp.Path, w.opts.Config.Memory.Path))
	if err != nil {
		return nil, fmt.Errorf("space %s: %w", sp.Name, err)
	}
	defer func() { _ = mem.Close() }()
	return mem.TaskSummaries(ctx, false, time.Time{})
}

// openTaskCounts lists the open tasks per agent, or "none".
func openTaskCounts(rows []memory.TaskSummary) string {
	counts := map[string]int{}
	for _, r := range rows {
		counts[r.Agent]++
	}
	if len(counts) == 0 {
		return "none"
	}
	var parts []string
	for _, name := range slices.Sorted(maps.Keys(counts)) {
		parts = append(parts, fmt.Sprintf("%s %d", name, counts[name]))
	}
	return strings.Join(parts, ", ")
}

// spaceDeleteReport is what the first delete_space call returns. It changes
// nothing.
func spaceDeleteReport(sp space.Space, open []memory.TaskSummary) string {
	var b strings.Builder
	fmt.Fprintf(&b, "space %s would be deleted from spaces.yaml\n", sp.Name)
	fmt.Fprintf(&b, "path: %s\n", sp.Path)
	fmt.Fprintf(&b, "agents assigned: %s\n", agentList(sp.Agents))
	fmt.Fprintf(&b, "open tasks: %s\n", openTaskCounts(open))
	fmt.Fprintf(&b, "kept on disk: the directory %s and its .groot data\n", sp.Path)
	b.WriteString("Call again with confirm: true to delete.")
	return b.String()
}

// agentDeleteReport is what the first delete_agent call returns. It changes
// nothing.
func agentDeleteReport(name string, assigned, open []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "agent %s would be deleted from the catalog\n", name)
	fmt.Fprintf(&b, "assigned to: %s\n", agentList(assigned))
	if len(open) == 0 {
		fmt.Fprintf(&b, "open tasks by space: none\n")
	} else {
		fmt.Fprintf(&b, "open tasks by space: %s\n", strings.Join(open, ", "))
	}
	fmt.Fprintf(&b, "kept: its memory and sessions stay in each space's database\n")
	b.WriteString("Call again with confirm: true to delete.")
	return b.String()
}

// checkPurge refuses a purge that could delete more than the space's own
// .groot data: the home directory, a relative path, or a directory that another
// space also uses.
func (w *Workspace) checkPurge(sp space.Space) error {
	if !filepath.IsAbs(sp.Path) {
		return fmt.Errorf("space %q has no absolute path: its data cannot be purged", sp.Name)
	}
	if filepath.Clean(sp.Path) == filepath.Clean(w.opts.Home) {
		return fmt.Errorf("space %q is the home directory: its data cannot be purged", sp.Name)
	}
	for _, other := range w.opts.Spaces.List() {
		if other.Name != sp.Name && filepath.Clean(other.Path) == filepath.Clean(sp.Path) {
			return fmt.Errorf("space %q uses the same directory %s: its data cannot be purged", other.Name, sp.Path)
		}
	}
	return nil
}

// closeStoresAt closes and forgets the open stores of a space directory, so its
// files can be removed.
func (w *Workspace) closeStoresAt(path string) error {
	w.mu.Lock()
	s, ok := w.stores[path]
	delete(w.stores, path)
	w.mu.Unlock()
	if !ok {
		return nil
	}
	return errors.Join(s.mem.Close(), s.aud.Close())
}

// purgeSpaceData deletes <space path>/.groot: the memory database and the audit
// log. Nothing else in the directory is touched.
func (w *Workspace) purgeSpaceData(sp space.Space) (string, error) {
	if err := w.closeStoresAt(sp.Path); err != nil {
		return "", err
	}
	dir := filepath.Join(sp.Path, ".groot")
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return "nothing to remove, " + dir + " does not exist", nil
	}
	if err := os.RemoveAll(dir); err != nil {
		return "", fmt.Errorf("remove %s: %w", dir, err)
	}
	return "removed " + dir + " (memory database and audit log)", nil
}

// renameAgentInDatabase renames an agent in the database of a space, if the
// space has one.
func (w *Workspace) renameAgentInDatabase(ctx context.Context, sp space.Space, oldName, newName string) error {
	if !w.hasDatabase(sp) {
		return nil
	}
	w.mu.Lock()
	st, err := w.storesFor(sp.Path)
	w.mu.Unlock()
	if err != nil {
		return err
	}
	return st.mem.RenameAgent(ctx, oldName, newName)
}

// DeleteSpace removes a space from spaces.yaml now, without a report. Its
// directory and its .groot data stay. The root space is never deleted. When the
// space was the active one, the selection moves to root.
func (w *Workspace) DeleteSpace(name string) (string, error) {
	return w.DeleteSpaceWithConfirm(name, true, false)
}

// DeleteSpaceWithConfirm is the two-step delete of a space. Without confirm it
// changes nothing and returns the report: the agents assigned, the open tasks
// per agent and what would stay on disk. With confirm it deletes the space, and
// purge also deletes <space path>/.groot. purge is honoured only with confirm.
func (w *Workspace) DeleteSpaceWithConfirm(name string, confirm, purge bool) (string, error) {
	if name == rootSpaceName {
		return "", fmt.Errorf("space %q is the home directory and cannot be deleted", rootSpaceName)
	}
	sp, ok := w.opts.Spaces.Get(name)
	if !ok {
		return "", fmt.Errorf("no space named %q", name)
	}
	ctx := context.Background()
	open, err := w.openTasksOf(ctx, sp)
	if err != nil {
		return "", err
	}
	if !confirm {
		return spaceDeleteReport(sp, open), nil
	}
	if purge {
		if err := w.checkPurge(sp); err != nil {
			return "", err
		}
	}
	wasCurrent := false
	if cur, ok := w.opts.Spaces.Current(); ok && cur.Name == name {
		wasCurrent = true
		if err := w.UseSpace(rootSpaceName); err != nil {
			return "", err
		}
	}
	if err := w.endConversations(inSpace(name)); err != nil {
		return "", err
	}
	if err := w.opts.Spaces.Remove(name); err != nil {
		return "", err
	}
	back, err := w.rereadSpaces()
	if err != nil {
		return "", err
	}
	if _, ok := back.Get(name); ok {
		return "", fmt.Errorf("space %q is still on disk after the delete", name)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "deleted space %s from spaces.yaml\n", name)
	if purge {
		msg, err := w.purgeSpaceData(sp)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&b, "its directory %s was kept; purged: %s\n", sp.Path, msg)
	} else {
		fmt.Fprintf(&b, "its directory %s and its .groot data were kept\n", sp.Path)
	}
	fmt.Fprintf(&b, "agents assigned: %s\n", agentList(sp.Agents))
	fmt.Fprintf(&b, "open tasks: %d\n", len(open))
	if wasCurrent {
		fmt.Fprintf(&b, "the active space is now %s\n", rootSpaceName)
	}
	fmt.Fprintf(&b, "spaces left: %d", len(back.List()))
	return b.String(), nil
}

// EditSpace renames a space, points it at another directory, or both. An empty
// newName or path keeps the current value. A rename keeps the selection, and
// the unread messages follow the new name. A new path does not move the
// .groot data under the old path.
func (w *Workspace) EditSpace(name, newName, path string) (string, error) {
	if newName == "" && path == "" {
		return "", errors.New("nothing to change: give a new name or a path")
	}
	if _, ok := w.opts.Spaces.Get(name); !ok {
		return "", fmt.Errorf("no space named %q", name)
	}
	renaming := newName != "" && newName != name
	if name == rootSpaceName && (renaming || path != "") {
		return "", fmt.Errorf("space %q is the home directory: it cannot be renamed or moved", rootSpaceName)
	}
	if path != "" {
		if err := space.CheckDir(path); err != nil {
			return "", err
		}
	}
	target := name
	var readdressed int64
	ctx := context.Background()
	if renaming {
		if err := w.opts.Spaces.Rename(name, newName); err != nil {
			return "", err
		}
		target = newName
		n, err := w.inbox.RenameSpace(ctx, name, newName)
		if err != nil {
			return "", err
		}
		readdressed = n
	}
	if path != "" {
		if err := w.opts.Spaces.SetPath(target, path); err != nil {
			return "", err
		}
	}
	if err := w.endConversations(inSpace(name)); err != nil {
		return "", err
	}
	back, err := w.rereadSpaces()
	if err != nil {
		return "", err
	}
	sp, ok := back.Get(target)
	if !ok {
		return "", fmt.Errorf("space %q is not on disk after the edit", target)
	}

	var b strings.Builder
	if renaming {
		fmt.Fprintf(&b, "renamed space %s to %s\n", name, newName)
	} else {
		fmt.Fprintf(&b, "edited space %s\n", target)
	}
	fmt.Fprintf(&b, "path: %s\n", sp.Path)
	if path != "" {
		fmt.Fprintf(&b, "the data under the old path was not moved: the .groot data of this space is now read from %s\n", path)
	}
	fmt.Fprintf(&b, "agents assigned: %s", agentList(sp.Agents))
	if renaming {
		fmt.Fprintf(&b, "\nmessages re-addressed to %s: %d", newName, readdressed)
	}
	return b.String(), nil
}

// DeleteAgent removes an agent from the catalog and from every space that
// assigns it. Its open conversations end. Its memory and sessions stay in each
// space's database. groot is never deleted; an installed debug can be, and its
// backlog files stay.
func (w *Workspace) DeleteAgent(name string) (string, error) {
	return w.DeleteAgentWithConfirm(name, true)
}

// DeleteAgentWithConfirm is the two-step delete of an agent. Without confirm it
// changes nothing and returns the report: the spaces the agent is assigned to
// and the open tasks per space. With confirm it deletes the agent.
func (w *Workspace) DeleteAgentWithConfirm(name string, confirm bool) (string, error) {
	if name == managedAgent {
		return "", fmt.Errorf("agent %q is managed and cannot be deleted", name)
	}
	if !catalog.Exists(w.opts.Home, name) {
		return "", fmt.Errorf("no agent named %q in the catalog", name)
	}
	ctx := context.Background()
	var assigned, open []string
	for _, sp := range w.opts.Spaces.List() {
		if !slices.Contains(sp.Agents, name) {
			continue
		}
		rows, err := w.openTasksOf(ctx, sp)
		if err != nil {
			return "", err
		}
		n := 0
		for _, r := range rows {
			if r.Agent == name {
				n++
			}
		}
		assigned = append(assigned, sp.Name)
		open = append(open, fmt.Sprintf("%s: %d", sp.Name, n))
	}
	if !confirm {
		return agentDeleteReport(name, assigned, open), nil
	}
	if err := w.endConversations(ofAgent(name)); err != nil {
		return "", err
	}
	removed, err := w.opts.Spaces.UnassignEverywhere(name)
	if err != nil {
		return "", err
	}
	if err := os.Remove(catalog.Path(w.opts.Home, name)); err != nil {
		return "", fmt.Errorf("remove agent file: %w", err)
	}
	if err := w.reload(); err != nil {
		return "", err
	}
	w.mu.Lock()
	if w.active == name {
		w.active = w.firstAssigned()
	}
	w.mu.Unlock()

	if _, ok, err := w.diskAgent(name); err != nil {
		return "", err
	} else if ok {
		return "", fmt.Errorf("agent %q is still in the catalog after the delete", name)
	}
	back, err := w.rereadSpaces()
	if err != nil {
		return "", err
	}
	for _, sp := range back.List() {
		if slices.Contains(sp.Agents, name) {
			return "", fmt.Errorf("space %q still assigns %q after the delete", sp.Name, name)
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "deleted agent %s from the catalog\n", name)
	fmt.Fprintf(&b, "removed from spaces: %s\n", agentList(removed))
	if len(open) == 0 {
		fmt.Fprintf(&b, "open tasks: none\n")
	} else {
		fmt.Fprintf(&b, "open tasks by space: %s\n", strings.Join(open, ", "))
	}
	fmt.Fprintf(&b, "memory and sessions stay in each space's database")
	return b.String(), nil
}

// EditAgent changes the description or the prompt of a catalog agent, or
// renames it. Managed agents keep their name. A prompt change restarts the open
// conversations of the agent, which resume their sessions with the new prompt.
// A rename moves the file, the assignments, the inbox, and the tasks, memory and
// sessions in each assigned space's database.
func (w *Workspace) EditAgent(e AgentEdit) (string, error) {
	if e.NewName == "" && e.Description == nil && e.Prompt == nil {
		return "", errors.New("nothing to change: give a description, a prompt or a new name")
	}
	d, err := w.agentDef(e.Name)
	if err != nil {
		return "", err
	}
	up := d
	if e.Description != nil {
		up.Description = *e.Description
	}
	if e.Prompt != nil {
		up.Prompt = *e.Prompt
	}
	renaming := e.NewName != "" && e.NewName != e.Name
	if renaming && isManaged(e.Name) {
		return "", fmt.Errorf("agent %q is managed and keeps its name", e.Name)
	}
	if !renaming {
		if err := catalog.Save(w.opts.Home, up, up.Prompt); err != nil {
			return "", err
		}
		if e.Prompt != nil {
			if err := w.endConversations(ofAgent(e.Name)); err != nil {
				return "", err
			}
		}
		if err := w.reload(); err != nil {
			return "", err
		}
		saved, ok, err := w.diskAgent(e.Name)
		if err != nil {
			return "", err
		}
		if !ok || saved.Description != up.Description || saved.Prompt != up.Prompt {
			return "", fmt.Errorf("agent %q on disk does not match the edit", e.Name)
		}
		var b strings.Builder
		fmt.Fprintf(&b, "updated agent %s\n", e.Name)
		fmt.Fprintf(&b, "description: %s\n", saved.Description)
		fmt.Fprintf(&b, "prompt: %d characters", len(saved.Prompt))
		if e.Prompt != nil {
			b.WriteString("\nopen conversations of this agent restart with the new prompt on their next turn")
		}
		return b.String(), nil
	}

	if isManaged(e.NewName) || catalog.Exists(w.opts.Home, e.NewName) {
		return "", fmt.Errorf("agent %q already exists", e.NewName)
	}
	up.Name = e.NewName
	if err := catalog.Validate(up); err != nil {
		return "", err
	}
	ctx := context.Background()
	var holders []space.Space
	for _, sp := range w.opts.Spaces.List() {
		if slices.Contains(sp.Agents, e.Name) {
			holders = append(holders, sp)
		}
	}
	if err := catalog.Save(w.opts.Home, up, up.Prompt); err != nil {
		return "", err
	}
	if err := os.Remove(catalog.Path(w.opts.Home, e.Name)); err != nil {
		return "", fmt.Errorf("remove old agent file: %w", err)
	}
	if err := w.endConversations(ofAgent(e.Name)); err != nil {
		return "", err
	}
	if err := w.opts.Spaces.RenameAgent(e.Name, e.NewName); err != nil {
		return "", err
	}
	for _, sp := range holders {
		if err := w.renameAgentInDatabase(ctx, sp, e.Name, e.NewName); err != nil {
			return "", fmt.Errorf("space %s: %w", sp.Name, err)
		}
	}
	readdressed, err := w.inbox.RenameAgent(ctx, e.Name, e.NewName)
	if err != nil {
		return "", err
	}
	w.mu.Lock()
	if w.active == e.Name {
		w.active = e.NewName
	}
	w.mu.Unlock()
	if err := w.reload(); err != nil {
		return "", err
	}

	if _, ok, err := w.diskAgent(e.Name); err != nil {
		return "", err
	} else if ok {
		return "", fmt.Errorf("agent %q is still in the catalog after the rename", e.Name)
	}
	if _, ok, err := w.diskAgent(e.NewName); err != nil {
		return "", err
	} else if !ok {
		return "", fmt.Errorf("agent %q is not in the catalog after the rename", e.NewName)
	}
	back, err := w.rereadSpaces()
	if err != nil {
		return "", err
	}
	var names []string
	for _, sp := range holders {
		if saved, ok := back.Get(sp.Name); ok && slices.Contains(saved.Agents, e.NewName) {
			names = append(names, sp.Name)
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "renamed agent %s to %s\n", e.Name, e.NewName)
	fmt.Fprintf(&b, "assigned in: %s\n", agentList(names))
	fmt.Fprintf(&b, "tasks, memory and sessions follow the new name in those spaces\n")
	fmt.Fprintf(&b, "messages re-addressed to %s: %d", e.NewName, readdressed)
	return b.String(), nil
}

// MoveAgent takes an agent off one space and puts it on another, in one save.
// The conversation in the old space ends; its sessions stay stored.
func (w *Workspace) MoveAgent(agent, from, to string, withMemory bool) (string, error) {
	if from == to {
		return "", errors.New("from and to are the same space")
	}
	src, ok := w.opts.Spaces.Get(from)
	if !ok {
		return "", fmt.Errorf("no space named %q", from)
	}
	dst, ok := w.opts.Spaces.Get(to)
	if !ok {
		return "", fmt.Errorf("no space named %q", to)
	}
	if !slices.Contains(src.Agents, agent) {
		return "", fmt.Errorf("agent %q is not assigned to space %q", agent, from)
	}
	if slices.Contains(dst.Agents, agent) {
		return "", fmt.Errorf("agent %q is already assigned to space %q", agent, to)
	}
	if err := w.endConversations(func(key string) bool { return key == from+"/"+agent }); err != nil {
		return "", err
	}
	// The copy runs before the move: if it fails, the agent stays where it was.
	var copied *memory.CopyCounts
	if withMemory {
		c, err := w.copyAgentMemory(context.Background(), src, dst, agent)
		if err != nil {
			return "", err
		}
		copied = &c
	}
	if err := w.opts.Spaces.Move(agent, from, to); err != nil {
		return "", err
	}
	w.mu.Lock()
	if cur, ok := w.opts.Spaces.Current(); ok {
		if cur.Name == from && w.active == agent {
			w.active = w.firstAssigned()
		} else if cur.Name == to && w.active == "" {
			w.active = agent
		}
	}
	w.mu.Unlock()

	back, err := w.rereadSpaces()
	if err != nil {
		return "", err
	}
	a, _ := back.Get(from)
	b, _ := back.Get(to)
	if slices.Contains(a.Agents, agent) || !slices.Contains(b.Agents, agent) {
		return "", fmt.Errorf("agent %q is not where the move put it", agent)
	}
	var out strings.Builder
	fmt.Fprintf(&out, "moved agent %s from %s to %s\n%s: %s\n%s: %s",
		agent, from, to, from, agentList(a.Agents), to, agentList(b.Agents))
	if copied != nil {
		fmt.Fprintf(&out, "\n%s", copyReport(to, from, *copied))
	}
	return out.String(), nil
}

// copyAgentMemory copies the memory, sessions and tasks of agent from the
// database of one space into the database of another. A source without a
// database has nothing to copy, and none is created for it.
func (w *Workspace) copyAgentMemory(ctx context.Context, from, to space.Space, agent string) (memory.CopyCounts, error) {
	if !w.hasDatabase(from) {
		return memory.CopyCounts{}, nil
	}
	w.mu.Lock()
	src, err := w.storesFor(from.Path)
	if err != nil {
		w.mu.Unlock()
		return memory.CopyCounts{}, err
	}
	dst, err := w.storesFor(to.Path)
	w.mu.Unlock()
	if err != nil {
		return memory.CopyCounts{}, err
	}
	return src.mem.CopyAgentData(ctx, dst.mem, agent)
}

// copyReport says what was copied into the target space.
func copyReport(to, from string, c memory.CopyCounts) string {
	s := fmt.Sprintf("data copied to %s: memories %d, sessions %d (%d messages), tasks %d (%d evidence)",
		to, c.Memories, c.Sessions, c.Messages, c.Tasks, c.Evidence)
	if c.SkippedSessions > 0 {
		s += fmt.Sprintf(", %d sessions skipped because %s already has them", c.SkippedSessions, to)
	}
	if c.SkippedTasks > 0 {
		s += fmt.Sprintf(", %d tasks skipped because %s already has them", c.SkippedTasks, to)
	}
	return s + fmt.Sprintf("; %s keeps its copies", from)
}

// CopyAgent makes a new catalog agent with the description, prompt, model,
// tools and max_turns of agent. The budget and the author are not copied. The
// copy is assigned to spaceName when one is given.
func (w *Workspace) CopyAgent(agent, newName, spaceName string) (string, error) {
	src, err := w.agentDef(agent)
	if err != nil {
		return "", err
	}
	if isManaged(newName) || catalog.Exists(w.opts.Home, newName) {
		return "", fmt.Errorf("agent %q already exists", newName)
	}
	if spaceName != "" {
		if _, ok := w.opts.Spaces.Get(spaceName); !ok {
			return "", fmt.Errorf("no space named %q", spaceName)
		}
	}
	up := catalog.AgentDef{
		Name:        newName,
		Description: src.Description,
		Model:       src.Model,
		Tools:       slices.Clone(src.Tools),
		MaxTurns:    src.MaxTurns,
		Prompt:      src.Prompt,
	}
	if err := catalog.Save(w.opts.Home, up, up.Prompt); err != nil {
		return "", err
	}
	if err := w.reload(); err != nil {
		return "", err
	}
	if spaceName != "" {
		if err := w.opts.Spaces.Assign(spaceName, newName); err != nil {
			return "", err
		}
		w.mu.Lock()
		if cur, ok := w.opts.Spaces.Current(); ok && cur.Name == spaceName && w.active == "" {
			w.active = newName
		}
		w.mu.Unlock()
	}

	saved, ok, err := w.diskAgent(newName)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("agent %q is not on disk after the copy", newName)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "copied agent %s to %s (description, prompt, model, tools, max_turns)\n", agent, newName)
	fmt.Fprintf(&b, "tools: %s\n", strings.Join(saved.Tools, ", "))
	if spaceName != "" {
		back, err := w.rereadSpaces()
		if err != nil {
			return "", err
		}
		sp, _ := back.Get(spaceName)
		if !slices.Contains(sp.Agents, newName) {
			return "", fmt.Errorf("agent %q is not assigned to %q after the copy", newName, spaceName)
		}
		fmt.Fprintf(&b, "assigned to space %s", spaceName)
	} else {
		fmt.Fprintf(&b, "not assigned to any space")
	}
	return b.String(), nil
}
