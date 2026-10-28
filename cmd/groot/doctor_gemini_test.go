package main

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestDoctorGeminiKeyIsOptionalAndSaysSo(t *testing.T) {
	env := fakeDoctorEnv(t, t.TempDir())
	env.EnvKey = "sk-ant-test-key-1234"

	out, code := runDoctorWith(t, env)
	if code != 0 {
		t.Fatalf("a missing Gemini key must not block when the Anthropic key is set: exit %d", code)
	}
	if l := lineWith(out, "gemini key:"); !strings.Contains(l, "missing (optional") {
		t.Fatalf("gemini key line %q", l)
	}
	if l := lineWith(out, "gemini models:"); !strings.Contains(l, "not checked") {
		t.Fatalf("gemini models line %q, want not checked without a key", l)
	}
}

func TestDoctorProbesGeminiWithItsKeyAndCountsModels(t *testing.T) {
	env := fakeDoctorEnv(t, t.TempDir())
	env.EnvKey = "sk-ant-test-key-1234"
	env.GeminiEnvKey = "gem-key-abcd-5678"
	env.ProbeGemini = func(_ context.Context, key string) (int, error) {
		if key != "gem-key-abcd-5678" {
			t.Errorf("probe used %q, want the Gemini key", key)
		}
		return 3, nil
	}

	out, code := runDoctorWith(t, env)
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if l := lineWith(out, "gemini key:"); !strings.Contains(l, "environment") || !strings.Contains(l, "5678") {
		t.Fatalf("gemini key line %q, want the source and last four characters", l)
	}
	if l := lineWith(out, "gemini models:"); !strings.Contains(l, "reachable, 3 models") {
		t.Fatalf("gemini models line %q", l)
	}
	if strings.Contains(out, "gem-key-abcd-5678") {
		t.Fatal("doctor printed the whole Gemini key")
	}
}

func TestDoctorGeminiUnreachableDoesNotBlock(t *testing.T) {
	env := fakeDoctorEnv(t, t.TempDir())
	env.EnvKey = "sk-ant-test-key-1234"
	env.GeminiEnvKey = "gem-key-abcd-5678"
	env.ProbeGemini = func(context.Context, string) (int, error) {
		return 0, errors.New("HTTP 403")
	}

	out, code := runDoctorWith(t, env)
	if code != 0 {
		t.Fatalf("an unreachable Gemini API is a note, not a block: exit %d", code)
	}
	if l := lineWith(out, "gemini models:"); !strings.Contains(l, "unreachable: HTTP 403") {
		t.Fatalf("gemini models line %q", l)
	}
}
