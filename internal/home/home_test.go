package home

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestResolveUsesGrootHomeOverride(t *testing.T) {
	env := func(k string) string {
		if k == "GROOT_HOME" {
			return "relative/dir"
		}
		return ""
	}
	got, err := Resolve(env, func() (string, error) { return "/should/not/be/used", nil })
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(got) || filepath.Base(got) != "dir" {
		t.Fatalf("override should be made absolute, got %q", got)
	}
}

func TestResolveDefaultsToDotGrootInHomeDir(t *testing.T) {
	got, err := Resolve(func(string) string { return "" }, func() (string, error) { return "/home/sre", nil })
	if err != nil {
		t.Fatal(err)
	}
	if got != "/home/sre/.groot" {
		t.Fatalf("got %q", got)
	}
}

func TestResolvePropagatesHomeError(t *testing.T) {
	_, err := Resolve(func(string) string { return "" }, func() (string, error) { return "", errors.New("no home") })
	if err == nil {
		t.Fatal("expected error")
	}
}
