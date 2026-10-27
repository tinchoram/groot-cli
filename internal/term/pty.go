package term

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
	"github.com/creack/pty"
)

// mouseModes are the DEC modes that make a program ask for mouse events.
var mouseModes = []ansi.DECMode{
	ansi.ModeMouseX10,
	ansi.ModeMouseNormal,
	ansi.ModeMouseHighlight,
	ansi.ModeMouseButtonEvent,
	ansi.ModeMouseAnyEvent,
}

// ptyTerminal runs the user's shell on a pseudo-terminal and feeds its output
// to a VT emulator. The emulator is guarded by mu: the output pump writes to it,
// while the UI renders it and sends paste and mouse input.
type ptyTerminal struct {
	out chan struct{}

	mu    sync.Mutex
	ptmx  *os.File
	cmd   *exec.Cmd
	emu   *vt.Emulator
	alive atomic.Bool

	// closing is set by Close. The reply pump owns the emulator's reader side,
	// so it is the one that closes the emulator: vt.Emulator keeps its closed
	// flag without a lock, and Read and Close from two goroutines would race.
	closing     atomic.Bool
	repliesDone chan struct{}

	// Mode flags kept up to date by the emulator's callbacks.
	tracking  atomic.Bool
	bracketed atomic.Bool
	mouseOn   map[ansi.DECMode]bool
}

// NewPTY returns a Terminal backed by a real shell process.
func NewPTY() Terminal {
	return &ptyTerminal{out: make(chan struct{}, 1), mouseOn: map[ansi.DECMode]bool{}}
}

// shellPath is $SHELL, or /bin/sh when it is not set.
func shellPath() string {
	if s := os.Getenv("SHELL"); s != "" {
		return s
	}
	return "/bin/sh"
}

// setMode records a DEC mode change reported by the emulator.
func (p *ptyTerminal) setMode(mode ansi.Mode, on bool) {
	dec, ok := mode.(ansi.DECMode)
	if !ok {
		return
	}
	if p.mouseOn == nil {
		p.mouseOn = map[ansi.DECMode]bool{}
	}
	for _, m := range mouseModes {
		if m == dec {
			p.mouseOn[m] = on
		}
	}
	if dec == ansi.ModeBracketedPaste {
		p.bracketed.Store(on)
	}
	any := false
	for _, m := range mouseModes {
		any = any || p.mouseOn[m]
	}
	p.tracking.Store(any)
}

func (p *ptyTerminal) Tracking() bool { return p.tracking.Load() }

func (p *ptyTerminal) Start(cols, rows int, dir string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ptmx != nil {
		return errors.New("terminal already started")
	}
	cmd := exec.Command(shellPath())
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	if err != nil {
		return fmt.Errorf("start %s: %w", shellPath(), err)
	}
	emu := vt.NewEmulator(cols, rows)
	emu.SetCallbacks(vt.Callbacks{
		EnableMode:  func(mode ansi.Mode) { p.setModeLocked(mode, true) },
		DisableMode: func(mode ansi.Mode) { p.setModeLocked(mode, false) },
	})
	p.ptmx, p.cmd, p.emu = ptmx, cmd, emu
	p.alive.Store(true)
	p.repliesDone = make(chan struct{})
	go p.pumpOutput(ptmx)
	go p.pumpReplies(emu, ptmx, p.repliesDone)
	return nil
}

// setModeLocked is the callback path: the emulator calls it while p.mu is
// held by the output pump, so it must not lock again.
func (p *ptyTerminal) setModeLocked(mode ansi.Mode, on bool) {
	p.setMode(mode, on)
}

// pumpOutput copies shell output into the emulator until the shell exits.
func (p *ptyTerminal) pumpOutput(ptmx *os.File) {
	buf := make([]byte, 4096)
	for {
		n, err := ptmx.Read(buf)
		if n > 0 {
			p.mu.Lock()
			if p.emu != nil {
				_, _ = p.emu.Write(buf[:n])
			}
			p.mu.Unlock()
			p.signal()
		}
		if err != nil {
			p.alive.Store(false)
			p.signal()
			return
		}
	}
}

// pumpReplies sends the emulator's answers (cursor reports, pastes and mouse
// reports written into its input pipe) back to the shell. It is the only
// reader of the emulator, so it closes the emulator when Close asks for it:
// Close wakes it with one byte that is dropped instead of forwarded.
func (p *ptyTerminal) pumpReplies(emu *vt.Emulator, ptmx *os.File, done chan<- struct{}) {
	defer close(done)
	buf := make([]byte, 256)
	for {
		n, err := emu.Read(buf)
		if p.closing.Load() {
			// Under mu, so the output pump's writes are ordered before it.
			p.mu.Lock()
			_ = emu.Close()
			p.mu.Unlock()
			return
		}
		if n > 0 {
			_, _ = ptmx.Write(buf[:n])
		}
		if err != nil {
			return
		}
	}
}

func (p *ptyTerminal) signal() {
	select {
	case p.out <- struct{}{}:
	default:
	}
}

func (p *ptyTerminal) Resize(cols, rows int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ptmx == nil {
		return nil
	}
	if err := pty.Setsize(p.ptmx, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)}); err != nil {
		return err
	}
	p.emu.Resize(cols, rows)
	return nil
}

func (p *ptyTerminal) Write(b []byte) (int, error) {
	p.mu.Lock()
	ptmx := p.ptmx
	p.mu.Unlock()
	if ptmx == nil {
		return 0, errors.New("terminal not started")
	}
	return ptmx.Write(b)
}

func (p *ptyTerminal) Paste(text string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.emu == nil {
		return errors.New("terminal not started")
	}
	p.emu.Paste(text)
	return nil
}

func (p *ptyTerminal) SendMouse(ev MouseEvent) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.emu == nil {
		return errors.New("terminal not started")
	}
	m := uv.Mouse{X: ev.X, Y: ev.Y, Button: mouseButton(ev.Button)}
	switch ev.Kind {
	case MouseDown:
		p.emu.SendMouse(uv.MouseClickEvent(m))
	case MouseUp:
		p.emu.SendMouse(uv.MouseReleaseEvent(m))
	case MouseDrag:
		p.emu.SendMouse(uv.MouseMotionEvent(m))
	case MouseWheelUp:
		m.Button = uv.MouseWheelUp
		p.emu.SendMouse(uv.MouseWheelEvent(m))
	case MouseWheelDown:
		m.Button = uv.MouseWheelDown
		p.emu.SendMouse(uv.MouseWheelEvent(m))
	}
	return nil
}

func mouseButton(b int) uv.MouseButton {
	switch b {
	case 1:
		return uv.MouseMiddle
	case 2:
		return uv.MouseRight
	}
	return uv.MouseLeft
}

func (p *ptyTerminal) Render() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.emu == nil {
		return nil
	}
	return renderScreen(p.emu)
}

func (p *ptyTerminal) Output() <-chan struct{} { return p.out }

func (p *ptyTerminal) Alive() bool { return p.alive.Load() }

func (p *ptyTerminal) Close() error {
	p.mu.Lock()
	p.alive.Store(false)
	if p.ptmx != nil {
		_ = p.ptmx.Close()
		p.ptmx = nil
	}
	if p.cmd != nil && p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
		_ = p.cmd.Wait()
	}
	emu, done := p.emu, p.repliesDone
	p.mu.Unlock()
	if emu == nil {
		return nil
	}
	if p.closing.CompareAndSwap(false, true) {
		// Wake the reply pump outside mu: the pipe write returns once the
		// pump has read it, and the pump then takes mu to close the emulator.
		// When the pump is already gone the pipe is closed and the write
		// returns at once.
		_, _ = emu.InputPipe().Write([]byte{0})
	}
	<-done
	return nil
}
