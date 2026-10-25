package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRelnotesPrintsTheBodyOfTheVersionSection(t *testing.T) {
	for _, arg := range []string{"v0.5.3", "0.5.3"} {
		var out, errOut bytes.Buffer
		if code := run([]string{arg}, &out, &errOut); code != 0 {
			t.Fatalf("%s: exit %d: %s", arg, code, errOut.String())
		}
		if !strings.Contains(out.String(), "### Backlog sweep") {
			t.Fatalf("%s: body lacks the 0.5.3 sections:\n%s", arg, out.String())
		}
		if strings.HasPrefix(out.String(), "## ") || strings.Contains(out.String(), "## 0.5.4") {
			t.Fatalf("%s: the notes must start below the heading and stay in their section:\n%s", arg, out.String())
		}
	}
}

func TestRelnotesFailsWhenTheVersionHasNoSection(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"v9.9.9"}, &out, &errOut); code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "no CHANGELOG.md entry for 9.9.9") {
		t.Fatalf("stderr: %q", errOut.String())
	}
	if out.Len() != 0 {
		t.Fatalf("stdout must stay empty on failure: %q", out.String())
	}
}

func TestRelnotesNeedsOneVersion(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run(nil, &out, &errOut); code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	if !strings.Contains(errOut.String(), "usage") {
		t.Fatalf("stderr: %q", errOut.String())
	}
}
