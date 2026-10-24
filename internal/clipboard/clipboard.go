// Package clipboard puts text on the system clipboard. Two routes are used
// together: an OSC 52 escape sequence, which terminals such as iTerm2, kitty,
// WezTerm, Ghostty and Windows Terminal honour, and a platform helper program.
package clipboard

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// Clipboard receives text to copy.
type Clipboard interface {
	Copy(text string) error
}

// OSC52 writes the copy escape sequence to W.
type OSC52 struct {
	W io.Writer
}

// Copy writes "ESC ] 52 ; c ; base64 BEL".
func (o OSC52) Copy(text string) error {
	_, err := fmt.Fprintf(o.W, "\x1b]52;c;%s\x07", base64.StdEncoding.EncodeToString([]byte(text)))
	return err
}

// RunFunc runs a helper program with text on its standard input.
type RunFunc func(name string, args []string, stdin string) error

func execRun(name string, args []string, stdin string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdin = strings.NewReader(stdin)
	return cmd.Run()
}

// platformCommands lists the helpers to try, in order, for an OS.
func platformCommands(goos string) [][]string {
	switch goos {
	case "darwin":
		return [][]string{{"pbcopy"}}
	case "linux":
		return [][]string{{"wl-copy"}, {"xclip", "-selection", "clipboard"}}
	}
	return nil
}

// Platform copies with the first helper for the OS that succeeds.
type Platform struct {
	GOOS string // defaults to runtime.GOOS
	Run  RunFunc
}

// Copy tries each helper in turn and reports an error only when none works.
func (p Platform) Copy(text string) error {
	goos := p.GOOS
	if goos == "" {
		goos = runtime.GOOS
	}
	run := p.Run
	if run == nil {
		run = execRun
	}
	cmds := platformCommands(goos)
	if len(cmds) == 0 {
		return errors.New("no clipboard helper for " + goos)
	}
	var last error
	for _, c := range cmds {
		if err := run(c[0], c[1:], text); err != nil {
			last = err
			continue
		}
		return nil
	}
	return last
}

// Multi copies to every clipboard and ignores their errors: the copy is best
// effort, and the UI reports the number of characters either way.
type Multi []Clipboard

// Copy calls each clipboard.
func (m Multi) Copy(text string) error {
	for _, c := range m {
		_ = c.Copy(text)
	}
	return nil
}

// Fake records copies for tests.
type Fake struct {
	Texts []string
}

// Copy records text.
func (f *Fake) Copy(text string) error {
	f.Texts = append(f.Texts, text)
	return nil
}

// Default returns the clipboard used by the program: OSC 52 to stdout plus the
// platform helper.
func Default() Clipboard {
	return Multi{OSC52{W: os.Stdout}, Platform{}}
}
