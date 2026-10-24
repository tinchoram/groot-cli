package tool

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/tinchoram/groot-cli/internal/api"
)

type fakeTool struct{ name string }

func (f fakeTool) Definition() api.ToolDef {
	return api.ToolDef{Name: f.name, Description: "fake", InputSchema: json.RawMessage(`{"type":"object"}`)}
}
func (f fakeTool) Risk() Risk                                     { return RiskRead }
func (f fakeTool) RiskFor(string) Risk                            { return RiskRead }
func (f fakeTool) Preview(input string) string                    { return input }
func (f fakeTool) Execute(context.Context, string) (string, bool) { return "ok", false }

func TestRegistryRegisterGetNamesDefinitions(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(fakeTool{name: "alpha"}); err != nil {
		t.Fatal(err)
	}
	if err := r.Register(fakeTool{name: "beta"}); err != nil {
		t.Fatal(err)
	}
	if err := r.Register(fakeTool{name: "alpha"}); err == nil {
		t.Fatal("duplicate registration must fail")
	}
	if _, ok := r.Get("beta"); !ok {
		t.Fatal("beta not found")
	}
	if _, ok := r.Get("missing"); ok {
		t.Fatal("missing tool should not be found")
	}
	names := r.Names()
	if strings.Join(names, ",") != "alpha,beta" {
		t.Fatalf("names must keep registration order, got %v", names)
	}
	defs := r.Definitions()
	if len(defs) != 2 || defs[0].Name != "alpha" {
		t.Fatalf("unexpected definitions: %+v", defs)
	}
}

func TestRiskString(t *testing.T) {
	if RiskRead.String() != "read" || RiskWrite.String() != "write" || RiskExec.String() != "exec" {
		t.Fatal("risk names drifted")
	}
}

func TestRedact(t *testing.T) {
	cases := []struct {
		name, in, wantAbsent, wantPresent string
	}{
		{"password assignment", "db password = hunter2 and more", "hunter2", "password"},
		{"api key colon", "api_key: sk-abc123", "sk-abc123", "api_key"},
		{"apikey upper", "APIKEY=zzz999", "zzz999", "APIKEY"},
		{"secret", "client_secret: topsecret", "topsecret", "secret"},
		{"token", "token=eyJhbGciOi", "eyJhbGciOi", "token"},
		{"bearer", "Authorization: Bearer abc.def.ghi", "abc.def.ghi", "Bearer"},
		{"untouched", "pod-a Running 3/3", "", "pod-a Running 3/3"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Redact(c.in)
			if c.wantAbsent != "" && strings.Contains(got, c.wantAbsent) {
				t.Fatalf("secret leaked: %q", got)
			}
			if !strings.Contains(got, c.wantPresent) {
				t.Fatalf("expected %q in %q", c.wantPresent, got)
			}
		})
	}
}

func TestUnifiedDiff(t *testing.T) {
	old := "a\nb\nc\nd\ne\nf\ng\n"
	updated := "a\nb\nC\nd\ne\nf\ng\n"
	d := UnifiedDiff("file.txt", "file.txt", old, updated)
	for _, want := range []string{"--- file.txt", "+++ file.txt", "@@", "-c", "+C", " b"} {
		if !strings.Contains(d, want) {
			t.Errorf("diff missing %q:\n%s", want, d)
		}
	}
	if strings.Contains(d, "-a") || strings.Contains(d, "-g") {
		t.Errorf("unchanged lines should not be marked removed:\n%s", d)
	}
}

func TestUnifiedDiffIdenticalAndNewFile(t *testing.T) {
	if d := UnifiedDiff("f", "f", "same\n", "same\n"); d != "" {
		t.Fatalf("identical input should produce no diff, got %q", d)
	}
	d := UnifiedDiff("f", "f", "", "hello\n")
	if !strings.Contains(d, "+hello") {
		t.Fatalf("new file diff should add lines:\n%s", d)
	}
}

func TestRedactJSONAndEnvForms(t *testing.T) {
	cases := []struct {
		name string
		in   string
		leak string // must not appear in output
		keep string // must still appear in output
	}{
		{"json token", `{"token": "abc123"}`, "abc123", `"token"`},
		{"json apiKey compact", `{"apiKey":"sk-live-999"}`, "sk-live-999", `"apiKey"`},
		{"json password spaced colon", `{"password" : "hunter3"}`, "hunter3", `"password"`},
		{"json client_secret", `{"client_secret": "zz9"}`, "zz9", `"client_secret"`},
		{"json access key", `{"access_key": "AKIAEXAMPLE"}`, "AKIAEXAMPLE", `"access_key"`},
		{"authorization bearer header", `Authorization: Bearer eyJhbGciOi.payload.sig`, "eyJhbGciOi.payload.sig", "Authorization"},
		{"aws env line", "AWS_SECRET_ACCESS_KEY=wJalrXUtnFEMI/K7MDENG", "wJalrXUtnFEMI", "AWS_SECRET_ACCESS_KEY"},
		{"github token env", "GITHUB_TOKEN=ghp_abcdef123", "ghp_abcdef123", "GITHUB_TOKEN"},
		{"kubectl secret dump", "data:\n  password: cGFzc3dvcmQ=\n  username: YWRtaW4=", "cGFzc3dvcmQ=", "username: YWRtaW4="},
		{"plain text untouched", `{"namespace": "prod", "replicas": 3}`, "", `"replicas": 3`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Redact(c.in)
			if c.leak != "" && strings.Contains(got, c.leak) {
				t.Fatalf("secret leaked: %q", got)
			}
			if !strings.Contains(got, c.keep) {
				t.Fatalf("expected %q to stay visible in %q", c.keep, got)
			}
		})
	}
}

func TestRedactJSONKeepsKeyAndMasksValue(t *testing.T) {
	got := Redact(`{"token": "abc123", "ns": "prod"}`)
	if !strings.Contains(got, `"token": "***"`) || !strings.Contains(got, `"ns": "prod"`) {
		t.Fatalf("unexpected JSON redaction: %s", got)
	}
}
