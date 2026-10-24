package clipboard

import (
	"bytes"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

func TestOSC52SequenceCarriesBase64Text(t *testing.T) {
	var buf bytes.Buffer
	if err := (OSC52{W: &buf}).Copy("héllo\nworld"); err != nil {
		t.Fatal(err)
	}
	want := "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte("héllo\nworld")) + "\x07"
	if buf.String() != want {
		t.Fatalf("sequence = %q", buf.String())
	}
}

func TestPlatformCommandsPerOS(t *testing.T) {
	cases := map[string][]string{
		"darwin":  {"pbcopy"},
		"linux":   {"wl-copy", "xclip -selection clipboard"},
		"windows": nil,
	}
	for goos, want := range cases {
		var got []string
		for _, c := range platformCommands(goos) {
			got = append(got, strings.Join(c, " "))
		}
		if strings.Join(got, "|") != strings.Join(want, "|") {
			t.Errorf("%s: commands = %v want %v", goos, got, want)
		}
	}
}

func TestPlatformUsesFirstWorkingHelper(t *testing.T) {
	var ran []string
	run := func(name string, args []string, stdin string) error {
		ran = append(ran, name+" <- "+stdin)
		if name == "wl-copy" {
			return errors.New("no wayland")
		}
		return nil
	}
	p := Platform{GOOS: "linux", Run: run}
	if err := p.Copy("text"); err != nil {
		t.Fatalf("a working helper should succeed: %v", err)
	}
	if len(ran) != 2 || ran[1] != "xclip <- text" {
		t.Fatalf("helpers tried = %v", ran)
	}
}

func TestPlatformReportsWhenNoHelperWorks(t *testing.T) {
	p := Platform{GOOS: "darwin", Run: func(string, []string, string) error { return errors.New("missing") }}
	if err := p.Copy("text"); err == nil {
		t.Fatal("no working helper must be an error")
	}
}

func TestMultiIgnoresFailures(t *testing.T) {
	fake := &Fake{}
	failing := Platform{GOOS: "darwin", Run: func(string, []string, string) error { return errors.New("x") }}
	if err := (Multi{failing, fake}).Copy("kept"); err != nil {
		t.Fatalf("Multi must ignore helper errors: %v", err)
	}
	if len(fake.Texts) != 1 || fake.Texts[0] != "kept" {
		t.Fatalf("the other clipboard must still receive the text: %v", fake.Texts)
	}
}

func TestFakeRecordsCopies(t *testing.T) {
	f := &Fake{}
	_ = f.Copy("a")
	_ = f.Copy("b")
	if strings.Join(f.Texts, ",") != "a,b" {
		t.Fatalf("texts = %v", f.Texts)
	}
}
