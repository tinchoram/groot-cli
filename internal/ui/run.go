package ui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

// Run starts the full-screen interface and blocks until the user quits.
// Events from the agent goroutines flow through opts.Bridge into the program.
func Run(opts Options) error {
	if opts.Bridge == nil || opts.Workspace == nil {
		return fmt.Errorf("ui: bridge and workspace are required")
	}
	model := NewModel(opts)
	p := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseAllMotion())

	done := make(chan struct{})
	defer close(done)
	go func() {
		for {
			select {
			case msg := <-opts.Bridge.Events():
				p.Send(msg)
			case <-done:
				return
			}
		}
	}()

	final, err := p.Run()
	if err != nil {
		return err
	}
	if fm, ok := final.(Model); ok && fm.endErr != nil {
		return fmt.Errorf("end session: %w", fm.endErr)
	}
	return nil
}
