package tool

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// spillPathPattern pulls the saved path out of the note: the file name is
// <tool>-<timestamp>-<n>.txt under <space>/.groot/outputs.
var spillPathPattern = regexp.MustCompile(`full text saved to (\S+?); read it with read_file`)

func spilledPath(t *testing.T, out string) string {
	t.Helper()
	m := spillPathPattern.FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("no saved-path note in %.200q", out)
	}
	return m[1]
}

func outputsDirOf(space string) string {
	return filepath.Join(space, ".groot", "outputs")
}

func countTxt(t *testing.T, dir string) int {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".txt") {
			n++
		}
	}
	return n
}

func TestFinishOutputKeepsSmallOutputInline(t *testing.T) {
	space := t.TempDir()
	text := strings.Repeat("a", outputInlineChars-1)

	got := finishOutput(space, "bash", text)
	if got != text {
		t.Fatalf("output under the limit must be unchanged; got %d chars", len(got))
	}
	if _, err := os.Stat(outputsDirOf(space)); !os.IsNotExist(err) {
		t.Fatalf("no outputs dir may be created for inline output: %v", err)
	}
}

func TestFinishOutputKeepsExactlyTheLimitInline(t *testing.T) {
	space := t.TempDir()
	text := strings.Repeat("é", outputInlineChars) // 2 bytes per character

	if got := finishOutput(space, "bash", text); got != text {
		t.Fatalf("exactly %d characters must stay inline, got %d characters", outputInlineChars, len([]rune(got)))
	}
}

func TestFinishOutputSpillsWithTheExactShape(t *testing.T) {
	space := t.TempDir()
	head := strings.Repeat("h", spillHeadChars)
	tail := strings.Repeat("z", spillTailChars)
	middle := strings.Repeat("x", 40000)
	text := head + middle + tail
	total := len([]rune(text))

	got := finishOutput(space, "bash", text)

	path := spilledPath(t, got)
	wantNote := fmt.Sprintf("[output: %d characters total; full text saved to %s; read it with read_file, for example offset/limit windows]", total, path)
	want := head + "\n" + wantNote + "\n" + tail
	if got != want {
		t.Fatalf("spilled shape mismatch:\n got %d chars\nwant %d chars", len(got), len(want))
	}
	if filepath.Dir(path) != outputsDirOf(space) {
		t.Fatalf("saved under %q, want %q", filepath.Dir(path), outputsDirOf(space))
	}
	if !regexp.MustCompile(`^bash-\d{8}-\d{6}-\d+\.txt$`).MatchString(filepath.Base(path)) {
		t.Fatalf("file name %q does not match <tool>-<timestamp>-<n>.txt", filepath.Base(path))
	}
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(saved) != text {
		t.Fatalf("saved file holds %d bytes, want the full %d characters", len(saved), total)
	}
}

func TestFinishOutputCountsCharactersNotBytes(t *testing.T) {
	space := t.TempDir()
	text := strings.Repeat("é", outputInlineChars+1)

	got := finishOutput(space, "fetch_url", text)
	if !strings.Contains(got, fmt.Sprintf("[output: %d characters total;", outputInlineChars+1)) {
		t.Fatalf("the note must count characters: %.120q", got)
	}
}

func TestFinishOutputRedactsInlineAndSavedText(t *testing.T) {
	space := t.TempDir()
	short := finishOutput(space, "bash", "password=hunter2")
	if strings.Contains(short, "hunter2") {
		t.Fatalf("inline text leaked a secret: %q", short)
	}

	text := "token: sk-live-123\n" + strings.Repeat("x", 40000)
	got := finishOutput(space, "bash", text)
	if strings.Contains(got, "sk-live-123") {
		t.Fatal("spilled preview leaked a secret")
	}
	saved, err := os.ReadFile(spilledPath(t, got))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(saved), "sk-live-123") || !strings.Contains(string(saved), "[REDACTED]") {
		t.Fatalf("saved text must be redacted: %.80q", saved)
	}
}

func TestFinishOutputKeepsTheTailVisible(t *testing.T) {
	space := t.TempDir()
	text := strings.Repeat("log line\n", 6000) + "[exit 2]"

	got := finishOutput(space, "kubectl", text)
	if !strings.HasSuffix(got, "[exit 2]") {
		t.Fatalf("the exit line must stay visible at the end: %q", got[len(got)-40:])
	}
}

func TestFinishOutputFilesAreOwnerOnly(t *testing.T) {
	space := t.TempDir()
	got := finishOutput(space, "bash", strings.Repeat("x", 40000))

	fi, err := os.Stat(spilledPath(t, got))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("file mode %o, want 600", fi.Mode().Perm())
	}
	di, err := os.Stat(outputsDirOf(space))
	if err != nil {
		t.Fatal(err)
	}
	if di.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode %o, want 700", di.Mode().Perm())
	}
}

func TestFinishOutputKeepsAtMostFiftyFiles(t *testing.T) {
	space := t.TempDir()
	for i := 0; i < outputsMaxFiles+5; i++ {
		finishOutput(space, "bash", strings.Repeat("x", 31000))
	}
	if n := countTxt(t, outputsDirOf(space)); n != outputsMaxFiles {
		t.Fatalf("%d files kept, want %d", n, outputsMaxFiles)
	}
}

func TestFinishOutputRemovesTheOldestBeyondTheCount(t *testing.T) {
	space := t.TempDir()
	dir := outputsDirOf(space)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	base := time.Now().Add(-time.Hour)
	first := filepath.Join(dir, "bash-00000000-000000-1.txt")
	for i := 0; i < outputsMaxFiles; i++ {
		p := filepath.Join(dir, fmt.Sprintf("bash-00000000-000000-%d.txt", i+2))
		if i == 0 {
			p = first
		}
		if err := os.WriteFile(p, []byte("old"), 0o600); err != nil {
			t.Fatal(err)
		}
		// Keep the set ordered: index 0 is the oldest.
		if err := os.Chtimes(p, base.Add(time.Duration(i)*time.Second), base.Add(time.Duration(i)*time.Second)); err != nil {
			t.Fatal(err)
		}
	}

	got := finishOutput(space, "bash", strings.Repeat("x", 40000))
	if _, err := os.Stat(first); !os.IsNotExist(err) {
		t.Fatalf("the oldest file must be removed beyond the count: %v", err)
	}
	if _, err := os.Stat(spilledPath(t, got)); err != nil {
		t.Fatalf("the new file must survive rotation: %v", err)
	}
	if n := countTxt(t, dir); n != outputsMaxFiles {
		t.Fatalf("%d files kept, want %d", n, outputsMaxFiles)
	}
}

func TestFinishOutputRemovesFilesOlderThanSevenDays(t *testing.T) {
	space := t.TempDir()
	dir := outputsDirOf(space)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(dir, "bash-20000101-000000-1.txt")
	recent := filepath.Join(dir, "bash-20000102-000000-1.txt")
	for p, age := range map[string]time.Duration{stale: 8 * 24 * time.Hour, recent: 6 * 24 * time.Hour} {
		if err := os.WriteFile(p, []byte("old"), 0o600); err != nil {
			t.Fatal(err)
		}
		when := time.Now().Add(-age)
		if err := os.Chtimes(p, when, when); err != nil {
			t.Fatal(err)
		}
	}

	finishOutput(space, "bash", strings.Repeat("x", 40000))
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatal("a file older than seven days must be removed when a new one is written")
	}
	if _, err := os.Stat(recent); err != nil {
		t.Fatalf("a six-day-old file must stay: %v", err)
	}
}

func TestFinishOutputSaveFailureStillShowsPreviewAndTail(t *testing.T) {
	space := t.TempDir()
	// A plain file where the .groot directory must be makes the save fail.
	if err := os.WriteFile(filepath.Join(space, ".groot"), []byte("not a dir"), 0o600); err != nil {
		t.Fatal(err)
	}
	text := strings.Repeat("h", spillHeadChars) + strings.Repeat("m", 40000) + strings.Repeat("z", spillTailChars)

	got := finishOutput(space, "bash", text)
	if !strings.Contains(got, "could not be saved") || !strings.HasPrefix(got, strings.Repeat("h", spillHeadChars)) || !strings.HasSuffix(got, strings.Repeat("z", spillTailChars)) {
		t.Fatalf("a failed save must say so and keep the preview and tail: %.120q", got)
	}
}

func TestCappedBufferStopsAtTheCaptureCeiling(t *testing.T) {
	c := &cappedBuffer{limit: maxCapturedOutput}
	chunk := make([]byte, 1<<20)
	for i := 0; i < 20; i++ {
		if _, err := c.Write(chunk); err != nil {
			t.Fatal(err)
		}
	}
	s := c.String()
	if !strings.Contains(s, "output capture stopped at 16 MiB") || len(s) > maxCapturedOutput+200 {
		t.Fatalf("capture must stop at the ceiling and say so; len=%d", len(s))
	}
}
