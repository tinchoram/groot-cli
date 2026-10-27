package tool

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	// outputInlineChars is the most characters a tool result carries inline.
	outputInlineChars = 30000
	// spillHeadChars and spillTailChars are the preview kept inline when the
	// full text is saved. The tail keeps an exit line or a final error visible.
	spillHeadChars = 2000
	spillTailChars = 500
	// outputsMaxFiles bounds the saved outputs of one space; the oldest go first.
	outputsMaxFiles = 50
	// outputsMaxAge removes saved outputs older than this when a new one is written.
	outputsMaxAge = 7 * 24 * time.Hour
	// maxCapturedOutput bounds the memory one command may fill before it is
	// stopped. Output up to this size is saved in full.
	maxCapturedOutput = 16 << 20
)

// spillMu serializes writes and rotation of the outputs directories.
var spillMu sync.Mutex

// finishOutput turns the raw text of one tool call into the text the model
// sees. The text is redacted first, so the saved file is redacted too. Text up
// to outputInlineChars characters is returned as is. Longer text is saved in
// full under <dir>/.groot/outputs and the result becomes a preview, a line that
// names the file, and the end of the output. An empty dir means the working
// directory.
func finishOutput(dir, toolName, text string) string {
	text = Redact(text)
	total := utf8.RuneCountInString(text)
	if total <= outputInlineChars {
		return text
	}
	head := firstRunes(text, spillHeadChars)
	tail := lastRunes(text, spillTailChars)

	path, err := saveOutput(dir, toolName, text)
	if err != nil {
		return fmt.Sprintf("%s\n[output: %d characters total; the full text could not be saved: %v]\n%s", head, total, err, tail)
	}
	return fmt.Sprintf("%s\n[output: %d characters total; full text saved to %s; read it with read_file, for example offset/limit windows]\n%s", head, total, path, tail)
}

// saveOutput writes text to a new file under <dir>/.groot/outputs, then
// rotates the directory. It returns the absolute path of the file.
func saveOutput(dir, toolName, text string) (string, error) {
	if dir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		dir = wd
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	outDir := filepath.Join(abs, ".groot", "outputs")

	spillMu.Lock()
	defer spillMu.Unlock()
	if err := os.MkdirAll(outDir, 0o700); err != nil {
		return "", err
	}
	stamp := time.Now().UTC().Format("20060102-150405")
	var (
		path string
		f    *os.File
	)
	for n := 1; ; n++ {
		path = filepath.Join(outDir, fmt.Sprintf("%s-%s-%d.txt", toolName, stamp, n))
		f, err = os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			break
		}
		if !os.IsExist(err) {
			return "", err
		}
	}
	_, werr := f.WriteString(text)
	cerr := f.Close()
	if werr != nil {
		_ = os.Remove(path)
		return "", werr
	}
	if cerr != nil {
		_ = os.Remove(path)
		return "", cerr
	}
	rotateOutputs(outDir, path, time.Now())
	return path, nil
}

// rotateOutputs removes saved outputs older than outputsMaxAge, then the
// oldest ones beyond outputsMaxFiles. keep is never removed. Only the .txt
// files that finishOutput writes are considered.
func rotateOutputs(dir, keep string, now time.Time) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	type saved struct {
		path string
		mod  time.Time
	}
	var files []saved
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".txt") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		p := filepath.Join(dir, e.Name())
		if p != keep && now.Sub(info.ModTime()) > outputsMaxAge {
			_ = os.Remove(p)
			continue
		}
		files = append(files, saved{path: p, mod: info.ModTime()})
	}
	if len(files) <= outputsMaxFiles {
		return
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mod.Before(files[j].mod) })
	for _, f := range files[:len(files)-outputsMaxFiles] {
		if f.path != keep {
			_ = os.Remove(f.path)
		}
	}
}

// firstRunes returns the first n characters of s.
func firstRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	i := 0
	for pos := range s {
		if i == n {
			return s[:pos]
		}
		i++
	}
	return s
}

// lastRunes returns the last n characters of s.
func lastRunes(s string, n int) string {
	count := utf8.RuneCountInString(s)
	if count <= n {
		return s
	}
	i := 0
	for pos := range s {
		if i == count-n {
			return s[pos:]
		}
		i++
	}
	return s
}
