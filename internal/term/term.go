// Package term runs an interactive shell in a pseudo-terminal and turns its
// screen into styled text rows for the UI. The UI depends only on the
// Terminal interface; the PTY adapter and its emulator are hidden here.
package term

// MouseKind is what a mouse event did.
type MouseKind int

const (
	MouseDown MouseKind = iota
	MouseUp
	MouseDrag
	MouseWheelUp
	MouseWheelDown
)

// MouseEvent is a mouse action at a cell of the shell's screen. X and Y are
// zero-based cells; Button is 0 left, 1 middle, 2 right.
type MouseEvent struct {
	Kind   MouseKind
	Button int
	X, Y   int
}

// Terminal is one interactive shell session.
type Terminal interface {
	// Start launches the shell in dir with the given size.
	Start(cols, rows int, dir string) error
	// Resize changes the size of the screen and of the PTY.
	Resize(cols, rows int) error
	// Write sends bytes to the shell as if typed.
	Write(p []byte) (int, error)
	// Paste sends text as a paste. The shell gets bracketed-paste markers when
	// it asked for them.
	Paste(text string) error
	// Tracking reports whether the shell's program asked for mouse events.
	Tracking() bool
	// SendMouse forwards a mouse event to the shell's program.
	SendMouse(ev MouseEvent) error
	// Render returns the screen as one styled line per row.
	Render() []string
	// Output signals that the screen changed and should be redrawn.
	Output() <-chan struct{}
	// Alive reports whether the shell is still running.
	Alive() bool
	// Close stops the shell and releases the PTY.
	Close() error
}
