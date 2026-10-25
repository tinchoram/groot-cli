package ui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/tinchoram/groot-cli/internal/workspace"
)

// toggleSidebar shows or hides the sidebar. The chat cannot be left without a
// sidebar when the chat itself is hidden, so this is a no-op then.
func (m *Model) toggleSidebar() {
	if !m.chatOpen {
		return
	}
	m.sidebarOpen = !m.sidebarOpen
	if !m.sidebarOpen {
		m.focusSidebar = false
	}
	m.resize()
	m.saveLayout()
}

// toggleChat hides the chat column; the sidebar then fills the screen. When
// the chat comes back, focus returns to the input.
func (m *Model) toggleChat() {
	m.chatOpen = !m.chatOpen
	if m.chatOpen {
		m.focusSidebar = false
		m.focusTerm = false
	} else {
		m.focusTerm = false
		m.sidebarOpen = true
		m.focusSidebar = true
		m.section = sectionSpaces
		m.cursor = m.activeIndex()
	}
	m.resize()
}

// handleSidebarKey moves the cursor, switches sections, and selects.
func (m Model) handleSidebarKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.focusSidebar = false
		m.refresh()
		return m, nil
	case tea.KeyUp:
		m.cursor = max(m.cursor-1, 0)
		return m, nil
	case tea.KeyDown:
		m.cursor = min(m.cursor+1, max(m.sectionLen()-1, 0))
		return m, nil
	case tea.KeyEnter:
		m.selectAtCursor()
		return m, nil
	}
	switch msg.String() {
	case "s":
		m.section = sectionSpaces
		m.cursor = m.activeIndex()
	case "a":
		m.section = sectionAgents
		m.cursor = m.activeIndex()
	}
	return m, nil
}

func (m *Model) sectionLen() int {
	if m.section == sectionSpaces {
		return len(m.ws.Spaces())
	}
	return len(m.ws.Agents())
}

// activeIndex is the position of the active space or agent in its section.
func (m *Model) activeIndex() int {
	if m.section == sectionSpaces {
		for i, sp := range m.ws.Spaces() {
			if sp.Active {
				return i
			}
		}
		return 0
	}
	for i, a := range m.ws.Agents() {
		if a.Active {
			return i
		}
	}
	return 0
}

func (m *Model) selectAtCursor() {
	if m.section == sectionSpaces {
		spaces := m.ws.Spaces()
		if m.cursor < len(spaces) {
			m.useSpace(spaces[m.cursor].Name)
			m.section = sectionAgents
			m.cursor = m.activeIndex()
		}
		return
	}
	agents := m.ws.Agents()
	if m.cursor < len(agents) {
		m.selectAgent(agents[m.cursor].Name)
		m.focusSidebar = false
	}
}

// useSpace switches space. Conversations keep running in the background, so
// switching is always allowed.
func (m *Model) useSpace(name string) {
	if err := m.ws.UseSpace(name); err != nil {
		m.append(styleError.Render("error: " + err.Error()))
		return
	}
	m.syncConversation()
	m.refreshUnread()
	m.cursor = m.activeIndex()
}

// selectAgent makes an agent of the active space the current conversation.
func (m *Model) selectAgent(name string) {
	if err := m.ws.SelectAgent(name); err != nil {
		m.append(styleError.Render("error: " + err.Error()))
		return
	}
	m.syncConversation()
}

// syncConversation stores the transcript of the conversation being left and
// loads the one being entered. It also refreshes the session handle.
func (m *Model) syncConversation() {
	key := m.ws.ConversationKey()
	if key != m.key {
		m.transcripts[m.key] = m.lines
		m.park(m.key)
		m.lines = m.transcripts[key]
		m.key = key
		m.unpark(key)
	}
	m.session = m.ws.Chat()
	m.restoreTranscript(key)
	if review := m.session.TakeReview(); review != "" {
		m.append(styleDim.Render(review))
	}
	m.refresh()
}

// runSpaceCommand handles /space and its subcommands.
func (m *Model) runSpaceCommand(arg string) {
	fields := strings.Fields(arg)
	usage := "usage: /space list | new <name> [path] | use <name> | assign <agent> | unassign <agent> | rename <old> <new> | delete <name> | routes [name]"
	if len(fields) == 0 {
		m.append(styleError.Render(usage))
		return
	}
	sub, rest := fields[0], fields[1:]
	switch sub {
	case "list":
		m.append(spaceListBlock(m.ws.Spaces()))
	case "routes":
		name := ""
		if len(rest) >= 1 {
			name = rest[0]
		} else {
			active, ok := m.ws.ActiveSpace()
			if !ok {
				m.append(styleError.Render("no space selected: use /space use <name>"))
				return
			}
			name = active.Name
		}
		view, err := m.ws.SpaceFlow(name)
		if err != nil {
			m.append(styleError.Render("error: " + err.Error()))
			return
		}
		m.append(spaceRoutesBlock(view))
	case "new":
		if len(rest) < 1 {
			m.append(styleError.Render("usage: /space new <name> [path]"))
			return
		}
		path := ""
		if len(rest) >= 2 {
			path = rest[1]
		} else {
			cwd, err := os.Getwd()
			if err != nil {
				m.append(styleError.Render("error: " + err.Error()))
				return
			}
			path = cwd
		}
		abs, err := filepath.Abs(path)
		if err != nil {
			m.append(styleError.Render("error: " + err.Error()))
			return
		}
		if err := m.ws.NewSpace(rest[0], abs); err != nil {
			m.append(styleError.Render("error: " + err.Error()))
			return
		}
		m.append(styleDim.Render(fmt.Sprintf("space %s created at %s (switch with /space use %s)", rest[0], abs, rest[0])))
	case "use":
		if len(rest) < 1 {
			m.append(styleError.Render("usage: /space use <name>"))
			return
		}
		m.useSpace(rest[0])
	case "assign", "unassign":
		if len(rest) < 1 {
			m.append(styleError.Render("usage: /space " + sub + " <agent>"))
			return
		}
		var err error
		if sub == "assign" {
			err = m.ws.AssignAgent(rest[0])
		} else {
			err = m.ws.UnassignAgent(rest[0])
		}
		if err != nil {
			m.append(styleError.Render("error: " + err.Error()))
			return
		}
		m.syncConversation()
		m.append(styleDim.Render(fmt.Sprintf("%s %s", map[string]string{"assign": "assigned", "unassign": "unassigned"}[sub], rest[0])))
	case "delete":
		if len(rest) != 1 {
			m.append(styleError.Render("usage: /space delete <name>"))
			return
		}
		out, err := m.ws.DeleteSpace(rest[0])
		m.finishEdit(out, err)
	case "rename":
		if len(rest) != 2 {
			m.append(styleError.Render("usage: /space rename <old> <new>"))
			return
		}
		out, err := m.ws.EditSpace(rest[0], rest[1], "")
		if err == nil {
			m.rekeyTranscripts(spaceRenamed(rest[0], rest[1]))
		}
		m.finishEdit(out, err)
	default:
		m.append(styleError.Render(usage))
	}
}

// runAgentCommand handles /agent and its subcommands. It returns a command
// when the work (an import fetches a file) must not block the screen.
func (m *Model) runAgentCommand(arg string) tea.Cmd {
	fields := strings.Fields(arg)
	sub := "list"
	var rest []string
	if len(fields) > 0 {
		sub, rest = fields[0], fields[1:]
	}
	switch sub {
	case "library":
		entries, err := m.ws.LibraryAgents()
		if err != nil {
			m.append(styleError.Render("error: " + err.Error()))
			return nil
		}
		if filter := strings.Join(rest, " "); filter != "" {
			var kept []workspace.LibraryInfo
			for _, e := range entries {
				if e.Collection == filter {
					kept = append(kept, e)
				}
			}
			entries = kept
		}
		m.append(libraryBlock(entries))
		return nil
	case "tools":
		m.agentToolsCommand(rest)
		return nil
	case "upgrade":
		m.upgradeAgents(rest)
		return nil
	case "install":
		force := slices.Contains(rest, "--force")
		var args []string
		for _, f := range rest {
			if f != "--force" {
				args = append(args, f)
			}
		}
		if len(args) < 1 {
			m.append(styleError.Render("usage: /agent install <name> [space] [--force]"))
			return nil
		}
		space := ""
		name := args[0]
		if qualified := strings.Join(args, " "); strings.Contains(qualified, "/") {
			// A public agent is named "<source>/<agent>", and its source name may contain spaces.
			name = qualified
		} else if len(args) == 2 {
			space = args[1]
		} else if len(args) > 2 {
			m.append(styleError.Render("usage: /agent install <name> [space] [--force]"))
			return nil
		}
		if err := m.ws.InstallLibraryAgent(name, space, force); err != nil {
			m.append(styleError.Render("error: " + err.Error()))
			return nil
		}
		msg := "installed " + name + " into the catalog"
		if space != "" {
			msg += " and assigned it to " + space
		}
		m.append(styleDim.Render(msg))
		return nil
	case "import":
		force := slices.Contains(rest, "--force")
		var sources []string
		for _, f := range rest {
			if f != "--force" {
				sources = append(sources, f)
			}
		}
		if len(sources) != 1 {
			m.append(styleError.Render("usage: /agent import <path-or-url> [--force]"))
			return nil
		}
		return importAgentCmd(m.ws, sources[0], force)
	case "list":
		m.append(agentListBlock(m.ws.CatalogAgents()))
	case "new":
		if len(rest) < 1 {
			m.append(styleError.Render("usage: /agent new <name> [description]"))
			return nil
		}
		path, err := m.ws.NewAgent(rest[0], strings.Join(rest[1:], " "))
		if err != nil {
			m.append(styleError.Render("error: " + err.Error()))
			return nil
		}
		m.append(styleDim.Render(fmt.Sprintf("agent %s created at %s. Edit the file there, then run /space assign %s", rest[0], path, rest[0])))
	case "show":
		if qualified := strings.Join(rest, " "); strings.Contains(qualified, "/") {
			detail, err := m.ws.LibraryAgent(qualified)
			if err != nil {
				m.append(styleError.Render("error: " + err.Error()))
				return nil
			}
			m.append(libraryDetailBlock(detail))
			return nil
		}
		name := m.ws.ActiveAgent()
		if len(rest) > 0 {
			name = rest[0]
		}
		if name == "" {
			m.append(styleError.Render("no agent selected: pass a name, e.g. /agent show groot"))
			return nil
		}
		info, err := m.ws.Agent(name)
		if err != nil {
			m.append(styleError.Render("error: " + err.Error()))
			return nil
		}
		m.append(agentDetailBlock(info))
		if tools, err := m.ws.AgentTools(name); err == nil {
			m.append(styleDim.Render("tools: " + strings.Join(tools, ", ")))
		}
	case "delete":
		if len(rest) != 1 {
			m.append(styleError.Render("usage: /agent delete <name>"))
			return nil
		}
		out, err := m.ws.DeleteAgent(rest[0])
		m.finishEdit(out, err)
	case "rename":
		if len(rest) != 2 {
			m.append(styleError.Render("usage: /agent rename <old> <new>"))
			return nil
		}
		out, err := m.ws.EditAgent(workspace.AgentEdit{Name: rest[0], NewName: rest[1]})
		if err == nil {
			m.rekeyTranscripts(agentRenamed(rest[0], rest[1]))
		}
		m.finishEdit(out, err)
	default:
		m.append(styleError.Render("usage: /agent list | new <name> [description] | show [name] | tools <name> add|remove <tool>[,<tool>…] | upgrade | rename <old> <new> | delete <name>"))
	}
	return nil
}

// importAgentCmd fetches and saves an agent file in the background and reports the result.
// finishEdit prints the report of a delete or rename, or its error, and brings
// the open conversation up to date with the change.
func (m *Model) finishEdit(out string, err error) {
	if err != nil {
		m.append(styleError.Render("error: " + err.Error()))
		return
	}
	m.syncConversation()
	m.append(styleDim.Render(out))
}

// rekeyTranscripts renames the transcript keys that a rename changed, the
// parked ones and the open one, so a conversation keeps its transcript. newKey
// returns the key after the rename and whether the key changed.
func (m *Model) rekeyTranscripts(newKey func(key string) (string, bool)) {
	moved := map[string][]string{}
	for key, lines := range m.transcripts {
		if k, ok := newKey(key); ok {
			delete(m.transcripts, key)
			moved[k] = lines
		}
	}
	for k, lines := range moved {
		m.transcripts[k] = lines
	}
	if k, ok := newKey(m.key); ok {
		m.key = k
	}
}

// spaceRenamed maps the keys "<space>/<agent>" of a renamed space.
func spaceRenamed(oldName, newName string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		sp, agent, ok := strings.Cut(key, "/")
		if !ok || sp != oldName {
			return key, false
		}
		return newName + "/" + agent, true
	}
}

// agentRenamed maps the keys "<space>/<agent>" of a renamed agent, in every space.
func agentRenamed(oldName, newName string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		sp, agent, ok := strings.Cut(key, "/")
		if !ok || agent != oldName {
			return key, false
		}
		return sp + "/" + newName, true
	}
}

func importAgentCmd(ws Workspace, source string, force bool) tea.Cmd {
	return func() tea.Msg {
		res, err := ws.ImportAgent(context.Background(), source, force)
		if err != nil {
			return noteMsg{err: err}
		}
		lines := []string{fmt.Sprintf("imported agent %s to %s", res.Name, res.Path)}
		for _, w := range res.Warnings {
			lines = append(lines, "warning: "+w)
		}
		return noteMsg{text: styleDim.Render(strings.Join(lines, "\n"))}
	}
}

func spaceListBlock(spaces []workspace.SpaceInfo) string {
	if len(spaces) == 0 {
		return styleDim.Render("no spaces yet: create one with /space new <name>")
	}
	var sb strings.Builder
	sb.WriteString(styleDim.Render("spaces:") + "\n")
	for _, sp := range spaces {
		mark := "  "
		if sp.Active {
			mark = styleAccentBold.Render("▸ ")
		}
		agents := strings.Join(sp.Agents, ", ")
		if agents == "" {
			agents = "no agents"
		}
		fmt.Fprintf(&sb, "%s%-14s %s  %s\n", mark, sp.Name, styleDim.Render(sp.Path), styleDim.Render("("+agents+")"))
	}
	return strings.TrimRight(sb.String(), "\n")
}

// spaceRoutesBlock renders the merged flow of a space. Each value shows the
// layer it came from: space, global or default.
func spaceRoutesBlock(v workspace.FlowView) string {
	var sb strings.Builder
	sb.WriteString(styleDim.Render("routes of space "+v.Space+":") + "\n")
	fmt.Fprintf(&sb, "%-8s %s\n", "mode", originOf(v.Mode))
	fmt.Fprintf(&sb, "%-8s model %s\n", "agents", originOf(v.Model))
	fmt.Fprintf(&sb, "%-8s %s\n", "delegate", originOf(v.Delegation))
	fmt.Fprintf(&sb, "%-8s %s\n", "manager", originOf(v.Manager))
	if v.ManagerNote != "" {
		fmt.Fprintf(&sb, "%-8s %s\n", "", v.ManagerNote)
	}
	for _, r := range v.Routes {
		fmt.Fprintf(&sb, "%-8s model %s · agent %s\n", r.Size, originOf(r.Model), originOf(r.Agent))
	}
	return strings.TrimRight(sb.String(), "\n")
}

// originOf shows a value with its origin, such as "claude-haiku-5-5 (space)".
func originOf(s workspace.Setting) string {
	value := s.Value
	if value == "" {
		value = "none"
	}
	return fmt.Sprintf("%s (%s)", value, s.From)
}

func agentListBlock(agents []workspace.AgentInfo) string {
	var sb strings.Builder
	sb.WriteString(styleDim.Render("agents in catalog (✓ = assigned to the active space):") + "\n")
	for _, a := range agents {
		mark := "  "
		if a.Assigned {
			mark = styleAccentBold.Render("✓ ")
		}
		fmt.Fprintf(&sb, "%s%-14s %s\n", mark, a.Name, styleDim.Render(a.Description))
	}
	return strings.TrimRight(sb.String(), "\n")
}

func libraryBlock(entries []workspace.LibraryInfo) string {
	var sb strings.Builder
	sb.WriteString(styleDim.Render("agent library (install with /agent install <name> [space]):") + "\n")
	groups := map[string][]workspace.LibraryInfo{}
	var order []string
	for _, e := range entries {
		key := e.Collection + "\x00" + e.Author + "\x00" + e.License + "\x00" + e.ValidatedBy
		if _, ok := groups[key]; !ok {
			order = append(order, key)
		}
		groups[key] = append(groups[key], e)
	}
	for _, key := range order {
		parts := strings.Split(key, "\x00")
		header := "collection " + parts[0] + " by " + parts[1]
		if parts[2] != "" {
			header += " · license " + parts[2]
		}
		if parts[3] != "" {
			header += " · validated by " + parts[3]
		}
		fmt.Fprintf(&sb, "%s\n", styleAccentBold.Render(header))
		for _, e := range groups[key] {
			fmt.Fprintf(&sb, "  %-18s %s %s\n", e.Name, styleDim.Render(e.Description), styleDim.Render("("+e.Source+")"))
		}
	}
	return strings.TrimRight(sb.String(), "\n")
}

// libraryDetailBlock shows a library agent's description and the first lines of its prompt.
func libraryDetailBlock(d workspace.LibraryDetail) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s %s\n", styleAccentBold.Render(d.Name), styleDim.Render("· "+d.Description))
	lines := strings.Split(strings.TrimSpace(d.Prompt), "\n")
	if len(lines) > 8 {
		lines = append(lines[:8], "…")
	}
	sb.WriteString(styleDim.Render(strings.Join(lines, "\n")))
	return sb.String()
}

func agentDetailBlock(a workspace.AgentInfo) string {
	firstLine := strings.SplitN(strings.TrimSpace(a.Prompt), "\n", 2)[0]
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s %s\n", styleAccentBold.Render(a.Name), styleDim.Render("· "+a.Description))
	fmt.Fprintf(&sb, "  %-8s %s\n", "model", a.Model)
	fmt.Fprintf(&sb, "  %-8s %s\n", "tools", strings.Join(a.Tools, ", "))
	fmt.Fprintf(&sb, "  %-8s %s", "prompt", styleDim.Render(firstLine))
	return sb.String()
}

// sidebarLines renders the sidebar body. Full mode (chat hidden) adds the
// active space's path and each agent's model and tools.
func (m Model) sidebarLines(inner int, full bool) []string {
	rows := m.sidebarRows(inner, full)
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.text
	}
	return out
}

// rowKind says what a sidebar row stands for, so that a click can find it.
type rowKind int

const (
	rowNone rowKind = iota
	rowHeading
	rowSpace
	rowAgent
)

// sidebarRow is one line of the sidebar. index is the position of its space or
// agent in the list that the sidebar shows.
type sidebarRow struct {
	text  string
	kind  rowKind
	index int
}

// sidebarRows builds the sidebar lines, each tagged with what it shows.
func (m Model) sidebarRows(inner int, full bool) []sidebarRow {
	var rows []sidebarRow
	add := func(kind rowKind, index int, s string) {
		rows = append(rows, sidebarRow{text: truncateCells(s, inner), kind: kind, index: index})
	}
	// row adds a name row. The cursor row of the focused section is drawn as a
	// full-width highlight of its plain text, so the focus is obvious.
	row := func(kind rowKind, index int, section sidebarSection, active bool, s string) {
		if (section == m.section && index == m.cursor) || (section != m.section && active) {
			text := selectedBand(m.focusSidebar && m.section == section).Width(inner).Render(truncateCells(stripCells(s), inner))
			rows = append(rows, sidebarRow{text: text, kind: kind, index: index})
			return
		}
		add(kind, index, s)
	}

	add(rowHeading, 0, styleSectionHeading(m.section == sectionSpaces && m.focusSidebar).Render("SPACES"))
	for i, sp := range m.ws.Spaces() {
		row(rowSpace, i, sectionSpaces, sp.Active, m.marker(i, sp.Active, m.section == sectionSpaces)+nameStyle(sp.Active).Render(sp.Name)+m.spaceBadge(sp.Name)+joinBadges(m.spaceSpendBadge(sp.Name), m.taskBadge(sp.Name)))
		if full && sp.Active {
			add(rowSpace, i, "    "+styleDim.Render(sp.Path))
		}
	}
	add(rowNone, 0, "")
	add(rowHeading, 0, styleSectionHeading(m.section == sectionAgents && m.focusSidebar).Render("AGENTS"))
	activeSpace, _ := m.ws.ActiveSpace()
	for i, a := range m.ws.Agents() {
		row(rowAgent, i, sectionAgents, a.Active, m.marker(i, a.Active, m.section == sectionAgents)+nameStyle(a.Active).Render(a.Name)+m.unreadBadge(activeSpace.Name, a.Name)+m.stateBadge(activeSpace.Name, a.Name)+m.workBadge(activeSpace.Name, a.Name)+m.agentBadges(a.Name))
		add(rowAgent, i, "    "+styleDim.Render(a.Description))
		if full {
			add(rowAgent, i, "    "+styleDim.Render("model "+a.Model))
			add(rowAgent, i, "    "+styleDim.Render("tools "+strings.Join(a.Tools, ", ")))
		}
	}
	return rows
}

// marker is the two-cell prefix: a cursor when this row has keyboard focus,
// and a green ▸ on the active row.
func (m Model) marker(index int, active, inSection bool) string {
	cursor := " "
	if m.focusSidebar && inSection && index == m.cursor {
		cursor = "›"
	}
	if active {
		return cursor + styleTool.Render("▸") + " "
	}
	return cursor + " "
}

func nameStyle(active bool) lipgloss.Style {
	if active {
		return styleAccentBold
	}
	return lipgloss.NewStyle()
}

func styleSectionHeading(focused bool) lipgloss.Style {
	if focused {
		return styleAccentBold
	}
	return styleDim
}

// selectedBand is the band behind the selected row of a sidebar section. The
// focused section gets the lighter band; the other section keeps the darker one.
func selectedBand(focused bool) lipgloss.Style {
	bg := "#14532D"
	if focused {
		bg = "#166534"
	}
	return lipgloss.NewStyle().Background(lipgloss.Color(bg)).Foreground(lipgloss.Color("#F0FDF4"))
}

// truncateCells cuts s to n display cells, adding an ellipsis when it was cut.
func truncateCells(s string, n int) string {
	if lipgloss.Width(s) <= n {
		return s
	}
	runes := []rune(stripCells(s))
	if n <= 1 {
		return "…"
	}
	if len(runes) > n-1 {
		runes = runes[:n-1]
	}
	return string(runes) + "…"
}

// stripCells is the plain text of a styled string, used when truncating.
func stripCells(s string) string {
	var b strings.Builder
	in := false
	for _, r := range s {
		switch {
		case r == '\x1b':
			in = true
		case in && r == 'm':
			in = false
		case !in:
			b.WriteRune(r)
		}
	}
	return b.String()
}
