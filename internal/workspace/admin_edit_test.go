package workspace

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/tinchoram/groot-cli/internal/catalog"
	"github.com/tinchoram/groot-cli/internal/flow"
	"github.com/tinchoram/groot-cli/internal/inbox"
	"github.com/tinchoram/groot-cli/internal/memory"
	"github.com/tinchoram/groot-cli/internal/space"
)

// addRoot registers the root space, which every home has.
func addRoot(t *testing.T, f *fixture) {
	t.Helper()
	if err := f.spaces.Add(rootSpaceName, t.TempDir(), t0); err != nil {
		t.Fatal(err)
	}
}

// reread reads spaces.yaml back from disk.
func reread(t *testing.T, f *fixture) *space.Store {
	t.Helper()
	st, err := space.Open(filepath.Join(f.home, "spaces.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	return st
}

// openTask puts one open task of agent into the database of dir.
func openTask(t *testing.T, f *fixture, spaceName, dir, agent string) {
	t.Helper()
	f.ws.mu.Lock()
	st, err := f.ws.storesFor(dir)
	f.ws.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.mem.Tasks().Open(context.Background(), flow.Task{
		Space: spaceName, Agent: agent, Size: flow.SizeSmall, Phase: flow.PhaseClassified,
		Goal: "tidy", OpenedAt: t0, PhaseAt: t0, UpdatedAt: t0,
	}); err != nil {
		t.Fatal(err)
	}
}

// conversationKeys lists the open conversations.
func conversationKeys(f *fixture) []string {
	f.ws.mu.Lock()
	defer f.ws.mu.Unlock()
	var keys []string
	for k := range f.ws.convs {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

func TestDeleteSpaceRefusesRoot(t *testing.T) {
	f := newFixture(t)
	addRoot(t, f)
	if _, err := f.ws.DeleteSpace(rootSpaceName); err == nil {
		t.Fatal("the root space must never be deleted")
	}
	if _, ok := reread(t, f).Get(rootSpaceName); !ok {
		t.Fatal("root vanished from disk")
	}
}

func TestDeleteSpaceKeepsDirectoryAndReportsWhatWasLost(t *testing.T) {
	f := newFixture(t)
	addRoot(t, f)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "keep.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := f.spaces.Add("extra", dir, t0); err != nil {
		t.Fatal(err)
	}
	if err := f.spaces.Assign("extra", "reader"); err != nil {
		t.Fatal(err)
	}
	openTask(t, f, "extra", dir, "reader")

	out, err := f.ws.DeleteSpace("extra")
	if err != nil {
		t.Fatalf("DeleteSpace: %v", err)
	}
	if _, ok := reread(t, f).Get("extra"); ok {
		t.Fatal("deleted space is still in spaces.yaml")
	}
	if _, err := os.Stat(filepath.Join(dir, "keep.txt")); err != nil {
		t.Fatal("the directory of a deleted space must stay")
	}
	for _, want := range []string{"reader", "open tasks: 1", "kept"} {
		if !strings.Contains(out, want) {
			t.Fatalf("result lacks %q:\n%s", want, out)
		}
	}
}

func TestDeleteSpaceWithoutDatabaseReportsZeroTasks(t *testing.T) {
	f := newFixture(t)
	addRoot(t, f)
	dir := t.TempDir()
	if err := f.spaces.Add("empty", dir, t0); err != nil {
		t.Fatal(err)
	}
	out, err := f.ws.DeleteSpace("empty")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "open tasks: 0") {
		t.Fatalf("result = %q", out)
	}
	if _, err := os.Stat(filepath.Join(dir, ".groot")); !os.IsNotExist(err) {
		t.Fatal("deleting a space must not create its database")
	}
}

func TestDeleteCurrentSpaceSwitchesToRootAndClosesItsConversations(t *testing.T) {
	f := newFixture(t)
	addRoot(t, f)
	if err := f.ws.SelectAgent("reader"); err != nil {
		t.Fatal(err)
	}
	f.ws.Chat()
	if keys := conversationKeys(f); !slices.Equal(keys, []string{"default/reader"}) {
		t.Fatalf("open conversations = %v", keys)
	}

	if _, err := f.ws.DeleteSpace("default"); err != nil {
		t.Fatalf("DeleteSpace: %v", err)
	}
	if cur, _ := f.spaces.Current(); cur.Name != rootSpaceName {
		t.Fatalf("selection = %q, want root", cur.Name)
	}
	if keys := conversationKeys(f); len(keys) != 0 {
		t.Fatalf("conversations of the deleted space stay open: %v", keys)
	}
}

func TestEditSpaceRenameMovesSelectionConversationsAndInbox(t *testing.T) {
	f := newFixture(t)
	addRoot(t, f)
	if err := f.ws.SelectAgent("reader"); err != nil {
		t.Fatal(err)
	}
	f.ws.Chat()
	if _, err := f.ws.inbox.Send(context.Background(), inbox.Message{
		FromSpace: rootSpaceName, FromAgent: managedAgent, ToSpace: "default", ToAgent: "reader", Subject: "hi", Body: "b",
	}); err != nil {
		t.Fatal(err)
	}

	out, err := f.ws.EditSpace("default", "work", "")
	if err != nil {
		t.Fatalf("EditSpace: %v", err)
	}
	back := reread(t, f)
	if _, ok := back.Get("default"); ok {
		t.Fatal("old name is still on disk")
	}
	if sp, ok := back.Get("work"); !ok || sp.Path != f.dir || !slices.Equal(sp.Agents, []string{"reader"}) {
		t.Fatalf("renamed space on disk = %+v ok=%v", sp, ok)
	}
	if cur, _ := back.Current(); cur.Name != "work" {
		t.Fatalf("selection = %q, want the renamed space", cur.Name)
	}
	if keys := conversationKeys(f); len(keys) != 0 {
		t.Fatalf("conversation keys still use the old name: %v", keys)
	}
	unread, err := f.ws.inbox.Unread(context.Background(), "work", "reader")
	if err != nil || len(unread) != 1 {
		t.Fatalf("unread after rename = %+v, %v", unread, err)
	}
	if !strings.Contains(out, "work") {
		t.Fatalf("result = %q", out)
	}
}

func TestEditSpaceRefusesRootExistingAndEmptyEdits(t *testing.T) {
	f := newFixture(t)
	addRoot(t, f)
	if _, err := f.ws.EditSpace(rootSpaceName, "home", ""); err == nil {
		t.Fatal("root must never be renamed")
	}
	if _, err := f.ws.EditSpace(rootSpaceName, "", t.TempDir()); err == nil {
		t.Fatal("root is the home directory: its path must never change")
	}
	if _, err := f.ws.EditSpace("default", rootSpaceName, ""); err == nil {
		t.Fatal("renaming onto an existing space must fail")
	}
	if _, err := f.ws.EditSpace("default", "", ""); err == nil {
		t.Fatal("an edit with nothing to change must fail")
	}
	if _, err := f.ws.EditSpace("ghost", "x", ""); err == nil {
		t.Fatal("an unknown space must fail")
	}
}

func TestEditSpacePathKeepsDataAndSaysSo(t *testing.T) {
	f := newFixture(t)
	addRoot(t, f)
	newDir := t.TempDir()
	out, err := f.ws.EditSpace("default", "", newDir)
	if err != nil {
		t.Fatalf("EditSpace: %v", err)
	}
	if sp, _ := reread(t, f).Get("default"); sp.Path != newDir {
		t.Fatalf("path on disk = %q, want %q", sp.Path, newDir)
	}
	if !strings.Contains(out, "not moved") {
		t.Fatalf("result must say the old data was not moved:\n%s", out)
	}
	if _, err := f.ws.EditSpace("default", "", filepath.Join(newDir, "missing")); err == nil {
		t.Fatal("a missing directory must be refused")
	}
}

func TestDeleteAgentRefusesGroot(t *testing.T) {
	f := newFixture(t)
	if _, err := f.ws.DeleteAgent(managedAgent); err == nil {
		t.Fatalf("%s must never be deleted", managedAgent)
	}
}

func TestDeleteAgentRemovesFileAssignmentsAndConversations(t *testing.T) {
	f := newFixture(t)
	if err := f.spaces.Add("other", t.TempDir(), t0); err != nil {
		t.Fatal(err)
	}
	if err := f.spaces.Assign("other", "reader"); err != nil {
		t.Fatal(err)
	}
	if err := f.ws.SelectAgent("reader"); err != nil {
		t.Fatal(err)
	}
	f.ws.Chat()
	openTask(t, f, "default", f.dir, "reader")

	out, err := f.ws.DeleteAgent("reader")
	if err != nil {
		t.Fatalf("DeleteAgent: %v", err)
	}
	if catalog.Exists(f.home, "reader") {
		t.Fatal("the agent file is still on disk")
	}
	defs, err := catalog.Load(f.home)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range defs {
		if d.Name == "reader" {
			t.Fatal("the agent is still in the catalog")
		}
	}
	back := reread(t, f)
	for _, n := range []string{"default", "other"} {
		if sp, _ := back.Get(n); slices.Contains(sp.Agents, "reader") {
			t.Fatalf("space %s still assigns reader", n)
		}
	}
	if keys := conversationKeys(f); len(keys) != 0 {
		t.Fatalf("conversations of the agent stay open: %v", keys)
	}
	for _, want := range []string{"default, other", "default: 1", "memory and sessions stay"} {
		if !strings.Contains(out, want) {
			t.Fatalf("result lacks %q:\n%s", want, out)
		}
	}
}

func TestEditAgentChangesManagedDescriptionAndPromptOnly(t *testing.T) {
	f := newFixture(t)
	if err := catalog.Save(f.home, catalog.AgentDef{Name: managedAgent, Description: "old", Tools: []string{"list_spaces", "recall"}}, "old prompt"); err != nil {
		t.Fatal(err)
	}
	desc, prompt := "new description", "new prompt"
	if _, err := f.ws.EditAgent(AgentEdit{Name: managedAgent, Description: &desc, Prompt: &prompt}); err != nil {
		t.Fatalf("EditAgent: %v", err)
	}
	defs, _ := catalog.Load(f.home)
	var g catalog.AgentDef
	for _, d := range defs {
		if d.Name == managedAgent {
			g = d
		}
	}
	if g.Description != desc || g.Prompt != prompt || !slices.Equal(g.Tools, []string{"list_spaces", "recall"}) {
		t.Fatalf("groot on disk = %+v", g)
	}
	if _, err := f.ws.EditAgent(AgentEdit{Name: managedAgent, NewName: "boss"}); err == nil {
		t.Fatal("a managed agent must keep its name")
	}
}

func TestEditAgentRenameMovesAssignmentsMemoryAndMessages(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if err := f.ws.SelectAgent("reader"); err != nil {
		t.Fatal(err)
	}
	f.ws.Chat()
	openTask(t, f, "default", f.dir, "reader")
	if _, err := f.ws.inbox.Send(ctx, inbox.Message{
		FromSpace: rootSpaceName, FromAgent: managedAgent, ToSpace: "default", ToAgent: "reader", Subject: "hi", Body: "b",
	}); err != nil {
		t.Fatal(err)
	}

	out, err := f.ws.EditAgent(AgentEdit{Name: "reader", NewName: "scout"})
	if err != nil {
		t.Fatalf("EditAgent rename: %v", err)
	}
	if catalog.Exists(f.home, "reader") || !catalog.Exists(f.home, "scout") {
		t.Fatal("the file must move to the new name")
	}
	defs, _ := catalog.Load(f.home)
	var scout catalog.AgentDef
	for _, d := range defs {
		if d.Name == "scout" {
			scout = d
		}
	}
	if scout.Description != "reads files" || scout.Prompt != "You read files carefully." || !slices.Equal(scout.Tools, []string{"read_file", "recall"}) {
		t.Fatalf("definition after rename = %+v", scout)
	}
	if sp, _ := reread(t, f).Get("default"); !slices.Equal(sp.Agents, []string{"scout"}) {
		t.Fatalf("assignments after rename = %v", sp.Agents)
	}
	if keys := conversationKeys(f); len(keys) != 0 {
		t.Fatalf("conversations keep the old name: %v", keys)
	}
	if w := f.ws.ActiveAgent(); w != "scout" {
		t.Fatalf("selection = %q, want scout", w)
	}
	unread, err := f.ws.inbox.Unread(ctx, "default", "scout")
	if err != nil || len(unread) != 1 {
		t.Fatalf("unread after rename = %+v, %v", unread, err)
	}
	f.ws.mu.Lock()
	st, err := f.ws.storesFor(f.dir)
	f.ws.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	tasks, err := st.mem.Tasks().List(ctx, "default", "scout", 10)
	if err != nil || len(tasks) != 1 {
		t.Fatalf("tasks under the new name = %+v, %v", tasks, err)
	}
	if !strings.Contains(out, "scout") {
		t.Fatalf("result = %q", out)
	}
}

func TestEditAgentRefusesExistingAndInvalidNames(t *testing.T) {
	f := newFixture(t)
	if err := catalog.Save(f.home, catalog.AgentDef{Name: "other", Description: "x", Tools: []string{"recall"}}, "p"); err != nil {
		t.Fatal(err)
	}
	if err := f.ws.reload(); err != nil {
		t.Fatal(err)
	}
	if _, err := f.ws.EditAgent(AgentEdit{Name: "reader", NewName: "other"}); err == nil {
		t.Fatal("renaming onto an existing agent must fail")
	}
	if _, err := f.ws.EditAgent(AgentEdit{Name: "reader", NewName: "Not A Slug"}); err == nil {
		t.Fatal("an invalid name must fail")
	}
	if _, err := f.ws.EditAgent(AgentEdit{Name: "reader", NewName: managedAgent}); err == nil {
		t.Fatal("renaming onto groot must fail")
	}
	if _, err := f.ws.EditAgent(AgentEdit{Name: "reader"}); err == nil {
		t.Fatal("an edit with nothing to change must fail")
	}
}

func TestEditAgentPromptChangeRestartsOpenConversations(t *testing.T) {
	f := newFixture(t)
	if err := f.ws.SelectAgent("reader"); err != nil {
		t.Fatal(err)
	}
	f.ws.Chat()
	prompt := "A new prompt."
	if _, err := f.ws.EditAgent(AgentEdit{Name: "reader", Prompt: &prompt}); err != nil {
		t.Fatal(err)
	}
	if keys := conversationKeys(f); len(keys) != 0 {
		t.Fatalf("the open conversation keeps the old prompt: %v", keys)
	}
}

func TestMoveAgentIsAtomicAndUpdatesSelection(t *testing.T) {
	f := newFixture(t)
	if err := f.spaces.Add("other", t.TempDir(), t0); err != nil {
		t.Fatal(err)
	}
	if err := f.ws.SelectAgent("reader"); err != nil {
		t.Fatal(err)
	}
	f.ws.Chat()

	out, err := f.ws.MoveAgent("reader", "default", "other", false)
	if err != nil {
		t.Fatalf("MoveAgent: %v", err)
	}
	back := reread(t, f)
	from, _ := back.Get("default")
	to, _ := back.Get("other")
	if len(from.Agents) != 0 || !slices.Equal(to.Agents, []string{"reader"}) {
		t.Fatalf("after move default=%v other=%v", from.Agents, to.Agents)
	}
	if keys := conversationKeys(f); len(keys) != 0 {
		t.Fatalf("the conversation in the old space stays open: %v", keys)
	}
	if got := f.ws.ActiveAgent(); got != "" {
		t.Fatalf("selection in default = %q, want none", got)
	}
	if !strings.Contains(out, "default: none") || !strings.Contains(out, "other: reader") {
		t.Fatalf("result lists both spaces:\n%s", out)
	}
}

func TestMoveAgentRefusesUnassignedAndAlreadyPresent(t *testing.T) {
	f := newFixture(t)
	if err := f.spaces.Add("other", t.TempDir(), t0); err != nil {
		t.Fatal(err)
	}
	if _, err := f.ws.MoveAgent("reader", "other", "default", false); err == nil {
		t.Fatal("moving an agent that is not in from must fail")
	}
	if err := f.spaces.Assign("other", "reader"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.ws.MoveAgent("reader", "default", "other", false); err == nil {
		t.Fatal("moving onto a space that has the agent must fail")
	}
	if _, err := f.ws.MoveAgent("reader", "default", "default", false); err == nil {
		t.Fatal("moving within the same space must fail")
	}
}

func TestCopyAgentDuplicatesDefinitionAndAssigns(t *testing.T) {
	f := newFixture(t)
	if err := f.spaces.Add("other", t.TempDir(), t0); err != nil {
		t.Fatal(err)
	}
	out, err := f.ws.CopyAgent("reader", "reader-two", "other")
	if err != nil {
		t.Fatalf("CopyAgent: %v", err)
	}
	defs, _ := catalog.Load(f.home)
	var copied catalog.AgentDef
	for _, d := range defs {
		if d.Name == "reader-two" {
			copied = d
		}
	}
	if copied.Name == "" || copied.Description != "reads files" || copied.Prompt != "You read files carefully." ||
		!slices.Equal(copied.Tools, []string{"read_file", "recall"}) {
		t.Fatalf("copy = %+v", copied)
	}
	if sp, _ := reread(t, f).Get("other"); !slices.Equal(sp.Agents, []string{"reader-two"}) {
		t.Fatalf("assigned after copy = %v", sp.Agents)
	}
	if !strings.Contains(out, "reader-two") {
		t.Fatalf("result = %q", out)
	}
}

func TestCopyAgentRefusesExistingManagedAndUnknownSpace(t *testing.T) {
	f := newFixture(t)
	if _, err := f.ws.CopyAgent("reader", "reader", ""); err == nil {
		t.Fatal("an existing name must be refused")
	}
	if _, err := f.ws.CopyAgent("reader", managedAgent, ""); err == nil {
		t.Fatal("a managed name must be refused")
	}
	if _, err := f.ws.CopyAgent("reader", "lost", "nowhere"); err == nil {
		t.Fatal("an unknown space must be refused")
	}
	if catalog.Exists(f.home, "lost") {
		t.Fatal("a refused copy must not write the file")
	}
}

func TestDeleteSpaceWithoutConfirmReportsAndChangesNothing(t *testing.T) {
	f := newFixture(t)
	addRoot(t, f)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "keep.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := f.spaces.Add("extra", dir, t0); err != nil {
		t.Fatal(err)
	}
	if err := f.spaces.Assign("extra", "reader"); err != nil {
		t.Fatal(err)
	}
	openTask(t, f, "extra", dir, "reader")
	before, err := os.ReadFile(filepath.Join(f.home, "spaces.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	out, err := f.ws.DeleteSpaceWithConfirm("extra", false, false)
	if err != nil {
		t.Fatalf("report: %v", err)
	}
	after, err := os.ReadFile(filepath.Join(f.home, "spaces.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("the report changed spaces.yaml")
	}
	if _, ok := reread(t, f).Get("extra"); !ok {
		t.Fatal("the report deleted the space")
	}
	if _, err := os.Stat(filepath.Join(dir, "keep.txt")); err != nil {
		t.Fatal("the report touched the directory")
	}
	for _, want := range []string{"reader", "open tasks: reader 1", dir, "Call again with confirm: true to delete."} {
		if !strings.Contains(out, want) {
			t.Fatalf("report lacks %q:\n%s", want, out)
		}
	}

	if _, err := f.ws.DeleteSpaceWithConfirm("extra", true, false); err != nil {
		t.Fatalf("confirmed delete: %v", err)
	}
	if _, ok := reread(t, f).Get("extra"); ok {
		t.Fatal("the confirmed delete kept the space on disk")
	}
}

func TestDeleteAgentWithoutConfirmReportsSpacesAndChangesNothing(t *testing.T) {
	f := newFixture(t)
	openTask(t, f, "default", f.dir, "reader")
	before, err := os.ReadFile(filepath.Join(f.home, "spaces.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	agentFile, err := os.ReadFile(catalog.Path(f.home, "reader"))
	if err != nil {
		t.Fatal(err)
	}

	out, err := f.ws.DeleteAgentWithConfirm("reader", false)
	if err != nil {
		t.Fatalf("report: %v", err)
	}
	if after, _ := os.ReadFile(filepath.Join(f.home, "spaces.yaml")); !bytes.Equal(before, after) {
		t.Fatal("the report changed spaces.yaml")
	}
	if again, _ := os.ReadFile(catalog.Path(f.home, "reader")); !bytes.Equal(agentFile, again) {
		t.Fatal("the report changed the agent file")
	}
	if !catalog.Exists(f.home, "reader") {
		t.Fatal("the report deleted the agent")
	}
	for _, want := range []string{"default: 1", "Call again with confirm: true to delete."} {
		if !strings.Contains(out, want) {
			t.Fatalf("report lacks %q:\n%s", want, out)
		}
	}

	if _, err := f.ws.DeleteAgentWithConfirm("reader", true); err != nil {
		t.Fatalf("confirmed delete: %v", err)
	}
	if catalog.Exists(f.home, "reader") {
		t.Fatal("the confirmed delete kept the agent")
	}
}

// spaceWithData makes a space whose directory holds a user file and a .groot
// database with one open task.
func spaceWithData(t *testing.T, f *fixture, name string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, keep := range []string{"keep.txt", "sub/keep2.txt"} {
		if err := os.WriteFile(filepath.Join(dir, keep), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.spaces.Add(name, dir, t0); err != nil {
		t.Fatal(err)
	}
	if err := f.spaces.Assign(name, "reader"); err != nil {
		t.Fatal(err)
	}
	openTask(t, f, name, dir, "reader")
	if _, err := os.Stat(filepath.Join(dir, ".groot")); err != nil {
		t.Fatalf("setup: no .groot data: %v", err)
	}
	return dir
}

func TestDeleteSpaceReportDoesNotCreateAnAuditLog(t *testing.T) {
	f := newFixture(t)
	addRoot(t, f)
	dir := spaceWithData(t, f, "extra")
	if err := f.ws.closeStoresAt(dir); err != nil {
		t.Fatal(err)
	}
	audit := filepath.Join(dir, ".groot", "audit.jsonl")
	if err := os.Remove(audit); err != nil {
		t.Fatalf("setup: %v", err)
	}

	if _, err := f.ws.DeleteSpaceWithConfirm("extra", false, false); err != nil {
		t.Fatalf("report: %v", err)
	}
	if _, err := os.Stat(audit); !os.IsNotExist(err) {
		t.Fatal("the report created an audit log: it must change nothing on disk")
	}
}

func TestDeleteSpacePurgeRemovesOnlyDotGroot(t *testing.T) {
	f := newFixture(t)
	addRoot(t, f)
	dir := spaceWithData(t, f, "extra")

	out, err := f.ws.DeleteSpaceWithConfirm("extra", true, true)
	if err != nil {
		t.Fatalf("purge: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".groot")); !os.IsNotExist(err) {
		t.Fatal("the purge left .groot on disk")
	}
	for _, keep := range []string{"keep.txt", "sub/keep2.txt"} {
		if _, err := os.Stat(filepath.Join(dir, keep)); err != nil {
			t.Fatalf("the purge removed %s: %v", keep, err)
		}
	}
	f.ws.mu.Lock()
	_, open := f.ws.stores[dir]
	f.ws.mu.Unlock()
	if open {
		t.Fatal("the stores of the purged space are still open")
	}
	if !strings.Contains(out, "purged") || !strings.Contains(out, filepath.Join(dir, ".groot")) {
		t.Fatalf("result must say what was removed:\n%s", out)
	}
}

func TestDeleteSpaceWithoutPurgeKeepsDotGroot(t *testing.T) {
	f := newFixture(t)
	addRoot(t, f)
	dir := spaceWithData(t, f, "extra")

	out, err := f.ws.DeleteSpaceWithConfirm("extra", true, false)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".groot")); err != nil {
		t.Fatal("a delete without purge removed .groot")
	}
	if !strings.Contains(out, "kept") {
		t.Fatalf("result must say the data was kept:\n%s", out)
	}
}

// moveSetup gives the reader agent a memory entry and an open task in the
// default space, and adds a second space named other.
func moveSetup(t *testing.T, f *fixture) {
	t.Helper()
	if err := f.spaces.Add("other", t.TempDir(), t0); err != nil {
		t.Fatal(err)
	}
	openTask(t, f, "default", f.dir, "reader")
	f.ws.mu.Lock()
	st, err := f.ws.storesFor(f.dir)
	f.ws.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.mem.Remember(context.Background(), memory.Entry{
		Kind: memory.KindFact, Scope: memory.ScopeAgent, Agent: "reader", Text: "reader fact", Source: "test",
	}); err != nil {
		t.Fatal(err)
	}
}

// targetStores opens the memory database of a space for the test to read.
func targetStores(t *testing.T, f *fixture, name string) *spaceStores {
	t.Helper()
	sp, ok := f.spaces.Get(name)
	if !ok {
		t.Fatalf("no space %s", name)
	}
	f.ws.mu.Lock()
	defer f.ws.mu.Unlock()
	st, err := f.ws.storesFor(sp.Path)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func TestMoveAgentWithMemoryCopiesDataAndTheSourceKeepsIt(t *testing.T) {
	f := newFixture(t)
	moveSetup(t, f)
	ctx := context.Background()

	out, err := f.ws.MoveAgent("reader", "default", "other", true)
	if err != nil {
		t.Fatalf("MoveAgent: %v", err)
	}
	dst := targetStores(t, f, "other")
	if tasks, err := dst.mem.Tasks().List(ctx, "default", "reader", 10); err != nil || len(tasks) != 1 {
		t.Fatalf("copied tasks = %+v, %v", tasks, err)
	}
	if ov, err := dst.mem.Overview(ctx, "reader"); err != nil || ov.Visible != 1 {
		t.Fatalf("copied memory = %+v, %v", ov, err)
	}
	src := targetStores(t, f, "default")
	if tasks, err := src.mem.Tasks().List(ctx, "default", "reader", 10); err != nil || len(tasks) != 1 {
		t.Fatalf("the source lost its tasks: %+v, %v", tasks, err)
	}
	if ov, err := src.mem.Overview(ctx, "reader"); err != nil || ov.Visible != 1 {
		t.Fatalf("the source lost its memory: %+v, %v", ov, err)
	}
	if !strings.Contains(out, "copied") || !strings.Contains(out, "tasks 1") || !strings.Contains(out, "keeps its copies") {
		t.Fatalf("result must say what was copied:\n%s", out)
	}
}

func TestMoveAgentWithoutMemoryCopiesNothing(t *testing.T) {
	f := newFixture(t)
	moveSetup(t, f)
	ctx := context.Background()

	out, err := f.ws.MoveAgent("reader", "default", "other", false)
	if err != nil {
		t.Fatalf("MoveAgent: %v", err)
	}
	dst := targetStores(t, f, "other")
	if tasks, err := dst.mem.Tasks().List(ctx, "default", "reader", 10); err != nil || len(tasks) != 0 {
		t.Fatalf("a move without with_memory copied tasks: %+v, %v", tasks, err)
	}
	if ov, err := dst.mem.Overview(ctx, "reader"); err != nil || ov.Visible != 0 {
		t.Fatalf("a move without with_memory copied memory: %+v, %v", ov, err)
	}
	if strings.Contains(out, "copied") {
		t.Fatalf("a move without with_memory must not report a copy:\n%s", out)
	}
}

func TestDeleteSpacePurgeRefusesHomeAndSharedDirectories(t *testing.T) {
	f := newFixture(t)
	addRoot(t, f)
	if err := f.spaces.Add("homey", f.home, t0); err != nil {
		t.Fatal(err)
	}
	if _, err := f.ws.DeleteSpaceWithConfirm("homey", true, true); err == nil {
		t.Fatal("purging the home directory must be refused")
	}
	if _, ok := reread(t, f).Get("homey"); !ok {
		t.Fatal("a refused purge must not delete the space")
	}
	if err := f.spaces.Add("twin", f.dir, t0); err != nil {
		t.Fatal(err)
	}
	if _, err := f.ws.DeleteSpaceWithConfirm("twin", true, true); err == nil {
		t.Fatal("purging a directory that another space uses must be refused")
	}
	if _, ok := reread(t, f).Get("twin"); !ok {
		t.Fatal("a refused purge must not delete the space")
	}
}
