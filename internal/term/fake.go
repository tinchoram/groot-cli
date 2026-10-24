package term

import "sync"

// Fake is a Terminal for tests. It records what it is asked to do and shows
// the lines it is given.
type Fake struct {
	mu       sync.Mutex
	Started  bool
	Dir      string
	Cols     int
	Rows     int
	Written  []byte
	Pasted   []string
	Mouse    []MouseEvent
	TrackOn  bool
	Closed   bool
	StartErr error
	alive    bool
	lines    []string
	out      chan struct{}
}

// NewFake returns a fake terminal that shows lines once started.
func NewFake(lines ...string) *Fake {
	return &Fake{lines: lines, out: make(chan struct{}, 1)}
}

func (f *Fake) Start(cols, rows int, dir string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.StartErr != nil {
		return f.StartErr
	}
	f.Started, f.Dir, f.Cols, f.Rows, f.alive = true, dir, cols, rows, true
	return nil
}

func (f *Fake) Resize(cols, rows int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Cols, f.Rows = cols, rows
	return nil
}

func (f *Fake) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Written = append(f.Written, p...)
	return len(p), nil
}

func (f *Fake) Paste(text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Pasted = append(f.Pasted, text)
	return nil
}

func (f *Fake) Tracking() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.TrackOn
}

func (f *Fake) SendMouse(ev MouseEvent) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Mouse = append(f.Mouse, ev)
	return nil
}

func (f *Fake) Render() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.lines...)
}

func (f *Fake) Output() <-chan struct{} { return f.out }

func (f *Fake) Alive() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.alive
}

func (f *Fake) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Closed, f.alive = true, false
	return nil
}

// Emit replaces the shown lines and signals a redraw.
func (f *Fake) Emit(lines ...string) {
	f.mu.Lock()
	f.lines = lines
	f.mu.Unlock()
	f.signal()
}

// Exit marks the shell as finished and signals a redraw.
func (f *Fake) Exit() {
	f.mu.Lock()
	f.alive = false
	f.mu.Unlock()
	f.signal()
}

func (f *Fake) signal() {
	select {
	case f.out <- struct{}{}:
	default:
	}
}
