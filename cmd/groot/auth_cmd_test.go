package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestAuthLoginFromStdinStoresTheKeyAndNeverPrintsIt(t *testing.T) {
	getenv, kc := isolate(t, "")
	var out, errOut bytes.Buffer
	code := run([]string{"auth", "login", "--stdin"}, strings.NewReader("sk-ant-login-test-1234\nignored\n"), &out, &errOut, getenv)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if kc.key != "sk-ant-login-test-1234" {
		t.Fatalf("keychain holds %q", kc.key)
	}
	if strings.Contains(out.String()+errOut.String(), "sk-ant-login-test-1234") {
		t.Fatal("login must never print the key")
	}
	if !strings.Contains(out.String(), "keychain") {
		t.Fatalf("login should say where the key went: %q", out.String())
	}
}

func TestAuthLoginRejectsAKeyWithSpaces(t *testing.T) {
	getenv, kc := isolate(t, "")
	var out, errOut bytes.Buffer
	code := run([]string{"auth", "login", "--stdin"}, strings.NewReader("not a key\n"), &out, &errOut, getenv)
	if code == 0 {
		t.Fatal("a key with spaces must be refused")
	}
	if kc.key != "" {
		t.Fatal("nothing may be stored on refusal")
	}
}

func TestAuthLoginNeedsStdinOrATerminal(t *testing.T) {
	getenv, _ := isolate(t, "")
	var out, errOut bytes.Buffer
	code := run([]string{"auth", "login"}, strings.NewReader("sk-ant-x\n"), &out, &errOut, getenv)
	if code == 0 || !strings.Contains(errOut.String(), "--stdin") {
		t.Fatalf("a non-terminal login without --stdin must say how to pass the key: code=%d err=%q", code, errOut.String())
	}
}

func TestAuthStatusShowsSourceAndOnlyTheLastFour(t *testing.T) {
	getenv, _ := isolate(t, "sk-ant-status-test-ab12")
	var out, errOut bytes.Buffer
	if code := run([]string{"auth", "status"}, nil, &out, &errOut, getenv); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	got := out.String()
	if !strings.Contains(got, "environment") || !strings.Contains(got, "…ab12") {
		t.Fatalf("status should name the source and the last four: %q", got)
	}
	if strings.Contains(got, "sk-ant-status-test") {
		t.Fatal("status must never print the full key")
	}
}

func TestAuthStatusNoneWhenNothingIsSet(t *testing.T) {
	getenv, _ := isolate(t, "")
	var out, errOut bytes.Buffer
	run([]string{"auth", "status"}, nil, &out, &errOut, getenv)
	if !strings.Contains(out.String(), "none") || !strings.Contains(out.String(), "groot auth login") {
		t.Fatalf("status with no key should say so and name login: %q", out.String())
	}
}

func TestAuthLogoutRemovesTheStoredKey(t *testing.T) {
	getenv, kc := isolate(t, "")
	var out, errOut bytes.Buffer
	run([]string{"auth", "login", "--stdin"}, strings.NewReader("sk-ant-logout-test-5678\n"), &out, &errOut, getenv)

	out.Reset()
	if code := run([]string{"auth", "logout"}, nil, &out, &errOut, getenv); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if kc.key != "" {
		t.Fatal("logout must remove the keychain entry")
	}
	if !strings.Contains(out.String(), "removed") {
		t.Fatalf("logout should say what it removed: %q", out.String())
	}
}

func TestAuthUnknownSubcommandIsAUsageError(t *testing.T) {
	getenv, _ := isolate(t, "")
	var out, errOut bytes.Buffer
	if code := run([]string{"auth", "frobnicate"}, nil, &out, &errOut, getenv); code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
}
