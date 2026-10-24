package ui

// upgradeAgents handles /agent upgrade <name>|--all. It prints one line per agent:
// the tools added, or that the agent is up to date.
func (m *Model) upgradeAgents(rest []string) {
	if len(rest) != 1 {
		m.append(styleError.Render("usage: /agent upgrade <name>|--all"))
		return
	}
	name := rest[0]
	if name == "--all" {
		name = ""
	}
	lines, err := m.ws.UpgradeAgents(name)
	if err != nil {
		m.append(styleError.Render("error: " + err.Error()))
		return
	}
	for _, line := range lines {
		m.append(styleDim.Render(line))
	}
}
