package mcp

import (
	"reflect"
	"strings"
	"testing"

	"github.com/tinchoram/groot-cli/internal/config"
)

func lookupFrom(env map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) {
		v, ok := env[k]
		return v, ok
	}
}

func TestStdioEnvironmentIsCleanAndHoldsOnlyTheConfiguredValues(t *testing.T) {
	host := map[string]string{
		"PATH":         "/usr/bin:/bin",
		"HOME":         "/home/u",
		"SECRET_TOKEN": "leak-me",
		"AWS_SECRET":   "leak-me-too",
	}
	s := config.MCPServer{
		Transport: "stdio",
		Command:   "mcp-kubernetes",
		Args:      []string{"--kubeconfig=${HOME}/.kube/config"},
		Env:       map[string]string{"KUBECONFIG": "${HOME}/.kube/config"},
		Tools:     []string{"list_pods"},
	}

	r, err := resolve("k8s", s, lookupFrom(host), "/work/space")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"HOME=/home/u",
		"KUBECONFIG=/home/u/.kube/config",
		"PATH=/usr/bin:/bin",
	}
	if !reflect.DeepEqual(r.Env, want) {
		t.Fatalf("env must hold only PATH, HOME and the configured values\nwant %v\ngot  %v", want, r.Env)
	}
	if r.Dir != "/work/space" {
		t.Fatalf("the working directory is the space path, got %q", r.Dir)
	}
	if r.Args[0] != "--kubeconfig=/home/u/.kube/config" {
		t.Fatalf("args expand from the process env, got %v", r.Args)
	}
}

func TestStdioEnvironmentOmitsPathAndHomeWhenTheProcessLacksThem(t *testing.T) {
	r, err := resolve("k8s", config.MCPServer{
		Transport: "stdio", Command: "x", Tools: []string{"a"},
		Env: map[string]string{"ONLY": "this"},
	}, lookupFrom(map[string]string{"SECRET": "no"}), "")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"ONLY=this"}; !reflect.DeepEqual(r.Env, want) {
		t.Fatalf("want %v, got %v", want, r.Env)
	}
}

func TestStdioUnsetVariableIsAnError(t *testing.T) {
	_, err := resolve("k8s", config.MCPServer{
		Transport: "stdio", Command: "x", Tools: []string{"a"},
		Env: map[string]string{"TOKEN": "${MISSING_TOKEN}"},
	}, lookupFrom(nil), "")
	if err == nil || err.Error() != "${MISSING_TOKEN} is not set" {
		t.Fatalf("an unset secret must fail, got %v", err)
	}
}

func TestHTTPResolveExpandsURLAndHeaders(t *testing.T) {
	host := map[string]string{"GRAFANA_TOKEN": "tok", "GRAFANA_HOST": "grafana.internal"}
	r, err := resolve("grafana", config.MCPServer{
		Transport: "http",
		URL:       "https://${GRAFANA_HOST}/mcp",
		Headers:   map[string]string{"Authorization": "Bearer ${GRAFANA_TOKEN}"},
		Tools:     []string{"a"},
	}, lookupFrom(host), "")
	if err != nil {
		t.Fatal(err)
	}
	if r.URL != "https://grafana.internal/mcp" {
		t.Fatalf("url not expanded: %q", r.URL)
	}
	if got := r.Headers["Authorization"]; got != "Bearer tok" {
		t.Fatalf("header not expanded: %q", got)
	}
}

func TestHTTPUnsetHeaderVariableIsAnError(t *testing.T) {
	_, err := resolve("grafana", config.MCPServer{
		Transport: "http", URL: "https://g.internal/mcp",
		Headers: map[string]string{"Authorization": "Bearer ${GRAFANA_TOKEN}"},
		Tools:   []string{"a"},
	}, lookupFrom(nil), "")
	if err == nil || !strings.Contains(err.Error(), "${GRAFANA_TOKEN} is not set") {
		t.Fatalf("an unset header secret must fail, got %v", err)
	}
}

func TestHTTPTransportSendsTheConfiguredHeaders(t *testing.T) {
	var got string
	server := newHeaderEchoServer(t, func(auth string) { got = auth })

	tr := newHTTPTransport(resolved{
		Transport: "http",
		URL:       server.URL + "/mcp",
		Headers:   map[string]string{"Authorization": "Bearer abc"},
	})
	if tr == nil {
		t.Fatal("transport not built")
	}
	// The server answers 500, so the handshake fails quickly; the test only
	// needs the request that carried the header.
	client := newClientForTest(t)
	_, _ = client.Connect(t.Context(), tr, nil)
	if got != "Bearer abc" {
		t.Fatalf("header must reach the server, got %q", got)
	}
}
