package catalog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func validDef(name string) AgentDef {
	return AgentDef{Name: name, Description: "test agent", Tools: []string{"read_file", "bash"}, MaxTurns: 7}
}

func TestParseFullFrontmatter(t *testing.T) {
	src := `---
name: reviewer
description: "Reviews diffs: strictly"
model: opus
tools: [read_file, recall]
max_turns: 9
---
You review code.

Keep it short.
`
	d, err := ParseAgentFile([]byte(src))
	if err != nil {
		t.Fatalf("ParseAgentFile: %v", err)
	}
	if d.Name != "reviewer" || d.Description != "Reviews diffs: strictly" || d.MaxTurns != 9 {
		t.Fatalf("fields = %+v", d)
	}
	if d.Model != "claude-opus-5-5" {
		t.Fatalf("alias not resolved: %q", d.Model)
	}
	if strings.Join(d.Tools, ",") != "read_file,recall" {
		t.Fatalf("tools = %v", d.Tools)
	}
	if d.Prompt != "You review code.\n\nKeep it short." {
		t.Fatalf("prompt = %q", d.Prompt)
	}
}

func TestParseToolsAsCommaString(t *testing.T) {
	d, err := ParseAgentFile([]byte("---\nname: x\ntools: \"Read, Bash ,Grep,\"\n---\n"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(d.Tools, "|") != "Read|Bash|Grep" {
		t.Fatalf("comma string tools = %q", d.Tools)
	}
}

func TestParseOptionalFieldsMissingAndAliases(t *testing.T) {
	cases := map[string]string{
		"missing model":    "---\nname: x\n---\n",
		"inherit":          "---\nname: x\nmodel: inherit\n---\n",
		"empty model":      "---\nname: x\nmodel: \"\"\n---\n",
		"haiku alias":      "---\nname: x\nmodel: haiku\n---\n",
		"explicit id":      "---\nname: x\nmodel: claude-haiku-5-5\n---\n",
		"upper-case alias": "---\nname: x\nmodel: Sonnet\n---\n",
	}
	want := map[string]string{
		"missing model":    "",
		"inherit":          "",
		"empty model":      "",
		"haiku alias":      "claude-haiku-5-5",
		"explicit id":      "claude-haiku-5-5",
		"upper-case alias": "claude-sonnet-5-5",
	}
	for label, src := range cases {
		d, err := ParseAgentFile([]byte(src))
		if err != nil {
			t.Fatalf("%s: %v", label, err)
		}
		if d.Model != want[label] {
			t.Errorf("%s: model = %q want %q", label, d.Model, want[label])
		}
	}
	d, err := ParseAgentFile([]byte("---\nname: x\n---\n"))
	if err != nil || d.Tools != nil || d.MaxTurns != 0 || d.Description != "" {
		t.Fatalf("optional fields should be zero: %+v (%v)", d, err)
	}
}

func TestParseCRLFAndUnknownFields(t *testing.T) {
	src := "---\r\nname: crlf\r\ncolor: teal\r\ntools: [bash]\r\n---\r\nPrompt line.\r\n"
	d, err := ParseAgentFile([]byte(src))
	if err != nil {
		t.Fatalf("CRLF file must parse: %v", err)
	}
	if d.Name != "crlf" || d.Prompt != "Prompt line." || strings.Join(d.Tools, ",") != "bash" {
		t.Fatalf("CRLF parse = %+v", d)
	}
}

func TestParseEmptyBodyAndClosingAtEOF(t *testing.T) {
	d, err := ParseAgentFile([]byte("---\nname: x\n---"))
	if err != nil || d.Name != "x" || d.Prompt != "" {
		t.Fatalf("closing fence at end of file: %+v (%v)", d, err)
	}
}

func TestParseRejectsBadFrontmatter(t *testing.T) {
	cases := map[string]string{
		"no frontmatter":    "You are an agent.",
		"unterminated":      "---\nname: x\nNo closing fence.",
		"invalid yaml":      "---\nname: [unclosed\n---\n",
		"tools wrong shape": "---\nname: x\ntools: {a: 1}\n---\n",
	}
	for label, src := range cases {
		if _, err := ParseAgentFile([]byte(src)); err == nil {
			t.Errorf("%s: expected an error", label)
		}
	}
}

func TestRenderThenParseRoundTrip(t *testing.T) {
	d := AgentDef{Name: "auditor", Description: "checks: merges", Model: "claude-haiku-5-5", Tools: []string{"read_file", "bash"}, MaxTurns: 4, Prompt: "You audit.\nBe strict."}
	raw, err := RenderAgentFile(d)
	if err != nil {
		t.Fatal(err)
	}
	back, err := ParseAgentFile(raw)
	if err != nil {
		t.Fatalf("rendered file does not parse: %v\n%s", err, raw)
	}
	if back.Name != d.Name || back.Description != d.Description || back.Model != d.Model || back.MaxTurns != d.MaxTurns || back.Prompt != d.Prompt || strings.Join(back.Tools, ",") != strings.Join(d.Tools, ",") {
		t.Fatalf("round trip changed the agent:\n%+v\n%+v", d, back)
	}
}

func TestSaveThenLoadMarkdown(t *testing.T) {
	home := t.TempDir()
	if err := Save(home, validDef("reviewer"), "You review code.\n"); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, "agents", "reviewer.md")); err != nil {
		t.Fatalf("agents/reviewer.md not written: %v", err)
	}
	defs, err := Load(home)
	if err != nil || len(defs) != 1 {
		t.Fatalf("Load = %d (%v)", len(defs), err)
	}
	if defs[0].Prompt != "You review code." || defs[0].MaxTurns != 7 {
		t.Fatalf("loaded = %+v", defs[0])
	}
}

func TestLoadMissingDirectoryIsEmpty(t *testing.T) {
	defs, err := Load(filepath.Join(t.TempDir(), "absent"))
	if err != nil || len(defs) != 0 {
		t.Fatalf("missing catalog should be empty: %d (%v)", len(defs), err)
	}
}

func TestLoadIsSortedAndIgnoresOtherFiles(t *testing.T) {
	home := t.TempDir()
	for _, n := range []string{"zeta", "alpha", "mid"} {
		if err := Save(home, validDef(n), ""); err != nil {
			t.Fatal(err)
		}
	}
	_ = os.WriteFile(filepath.Join(home, "agents", "notes.txt"), []byte("x"), 0o644)
	defs, err := Load(home)
	if err != nil {
		t.Fatal(err)
	}
	got := []string{defs[0].Name, defs[1].Name, defs[2].Name}
	if strings.Join(got, ",") != "alpha,mid,zeta" {
		t.Fatalf("order %v", got)
	}
}

func TestLoadRejectsFileNameMismatch(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "agents")
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, "folder-name.md"), []byte("---\nname: other-name\ntools: [bash]\n---\n"), 0o644)
	if _, err := Load(home); err == nil {
		t.Fatal("file stem and name must agree")
	}
}

func TestValidateRejectsBadNamesAndUnknownTools(t *testing.T) {
	cases := map[string]AgentDef{
		"empty name":     {Name: "", Tools: []string{"bash"}},
		"uppercase name": {Name: "DevJunior", Tools: []string{"bash"}},
		"spaces in name": {Name: "dev junior", Tools: []string{"bash"}},
		"path traversal": {Name: "../evil", Tools: []string{"bash"}},
		"unknown tool":   {Name: "ok", Tools: []string{"delete_everything"}},
		"no tools":       {Name: "ok", Tools: nil},
		"negative turns": {Name: "ok", Tools: []string{"bash"}, MaxTurns: -1},
	}
	for label, d := range cases {
		if err := Validate(d); err == nil {
			t.Errorf("%s: expected validation error", label)
		}
	}
	if err := Validate(validDef("good-name2")); err != nil {
		t.Fatalf("valid definition rejected: %v", err)
	}
}

func TestSaveRefusesInvalidDefinition(t *testing.T) {
	home := t.TempDir()
	if err := Save(home, AgentDef{Name: "bad name", Tools: []string{"bash"}}, ""); err == nil {
		t.Fatal("invalid definition must not be written")
	}
	if _, err := os.Stat(filepath.Join(home, "agents")); err == nil {
		t.Fatal("nothing should be created for an invalid definition")
	}
}

func TestMigrationConvertsLegacyFolderAndRemovesIt(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "agents", "legacy")
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, "agent.yaml"), []byte("name: legacy\ndescription: old style\ntools: [read_file]\nmodel: sonnet\n"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "prompt.md"), []byte("Old prompt.\n"), 0o644)

	defs, err := Load(home)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("legacy folder must be removed after a successful migration")
	}
	if len(defs) != 1 || defs[0].Name != "legacy" || defs[0].Prompt != "Old prompt." || defs[0].Model != "claude-sonnet-5-5" {
		t.Fatalf("migrated agent = %+v", defs)
	}
	if !Exists(home, "legacy") {
		t.Fatal("migrated agent must exist")
	}
}

func TestMigrationKeepsLegacyWhenMarkdownAlreadyExists(t *testing.T) {
	home := t.TempDir()
	if err := Save(home, validDef("dup"), "new"); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(home, "agents", "dup")
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, "agent.yaml"), []byte("name: dup\ntools: [bash]\n"), 0o644)
	defs, err := Load(home)
	if err != nil || len(defs) != 1 || defs[0].Prompt != "new" {
		t.Fatalf("existing markdown must win: %+v (%v)", defs, err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatal("the legacy folder must not be deleted when it was not migrated")
	}
}

func TestSaveRemovesStaleLegacyFolder(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "agents", "old")
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, "agent.yaml"), []byte("name: old\ntools: [bash]\n"), 0o644)
	if err := Save(home, validDef("old"), "fresh"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("saving must not leave a legacy folder behind")
	}
}

func TestExistsAndDefaultTools(t *testing.T) {
	home := t.TempDir()
	if Exists(home, "reviewer") {
		t.Fatal("nothing saved yet")
	}
	if err := Save(home, validDef("reviewer"), ""); err != nil {
		t.Fatal(err)
	}
	if !Exists(home, "reviewer") {
		t.Fatal("saved agent must exist")
	}
	if err := Validate(AgentDef{Name: "x", Tools: DefaultTools()}); err != nil {
		t.Fatalf("default tools must be valid: %v", err)
	}
}

func TestAdminToolsAreKnown(t *testing.T) {
	d := AgentDef{Name: "admin", Tools: []string{"list_spaces", "create_space", "assign_agent", "unassign_agent", "list_agents", "create_agent"}}
	if err := Validate(d); err != nil {
		t.Fatalf("admin tools must be valid: %v", err)
	}
}

func TestParseMetaReadsAuthorAndCollection(t *testing.T) {
	m, err := ParseMeta([]byte("---\nname: developer\nauthor: \"@tinchoram\"\ncollection: \"@tinchoram\"\ntools: [read_file]\n---\nBody.\n"))
	if err != nil {
		t.Fatal(err)
	}
	if m.Author != "@tinchoram" || m.Collection != "@tinchoram" {
		t.Fatalf("meta = %+v", m)
	}
	m, err = ParseMeta([]byte("---\nname: x\n---\n"))
	if err != nil || m.Author != "" || m.Collection != "" {
		t.Fatalf("missing meta should be empty: %+v (%v)", m, err)
	}
	if _, err := ParseMeta([]byte("no frontmatter")); err == nil {
		t.Fatal("no frontmatter must be an error")
	}
}

func TestAuthorAndCollectionSurviveRenderAndParse(t *testing.T) {
	src := "---\nname: dev\ndescription: d\nauthor: \"@tinchoram\"\ncollection: \"@tinchoram\"\ntools: [read_file]\n---\nprompt\n"
	d, err := ParseAgentFile([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if d.Author != "@tinchoram" || d.Collection != "@tinchoram" {
		t.Fatalf("parsed author/collection = %q/%q", d.Author, d.Collection)
	}
	raw, err := RenderAgentFile(d)
	if err != nil {
		t.Fatal(err)
	}
	again, err := ParseAgentFile(raw)
	if err != nil {
		t.Fatal(err)
	}
	if again.Author != "@tinchoram" || again.Collection != "@tinchoram" {
		t.Fatalf("rendered file lost author/collection:\n%s", raw)
	}
	plain, _ := RenderAgentFile(AgentDef{Name: "x", Description: "d", Prompt: "p"})
	if strings.Contains(string(plain), "author") || strings.Contains(string(plain), "collection") {
		t.Fatalf("empty author/collection must be omitted:\n%s", plain)
	}
}
