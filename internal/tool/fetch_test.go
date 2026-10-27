package tool

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// fetchServer serves the given handler on a loopback address. Loopback is
// private, so the tests either set AllowPrivate or assert the refusal.
func fetchServer(t *testing.T, h http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv
}

func serveBody(contentType, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", contentType)
		fmt.Fprint(w, body)
	}
}

func fetchURL(t *testing.T, f *Fetch, rawURL string, maxChars int) (string, bool) {
	t.Helper()
	input := fmt.Sprintf(`{"url":%q`, rawURL)
	if maxChars > 0 {
		input += fmt.Sprintf(`,"max_chars":%d`, maxChars)
	}
	input += "}"
	return f.Execute(context.Background(), input)
}

func allowPrivate() FetchPolicy { return FetchPolicy{AllowPrivate: true} }

func TestFetchHTMLExtractsReadableText(t *testing.T) {
	page := `<html><head><title>T</title><style>p{color:red}</style></head><body>
<nav>Menu links</nav>
<h1>Title</h1>
<p>First   paragraph.</p>
<h2>Section</h2>
<ul><li>one</li><li>two <a href="/docs">docs</a></li></ul>
<p>See <a href="https://example.com/x">example</a>.</p>
<table><tr><th>Name</th><th>Value</th></tr><tr><td>cpu</td><td>2</td></tr></table>
<script>var x = 1;</script>
<footer>Copyright</footer>
</body></html>`
	srv := fetchServer(t, serveBody("text/html; charset=utf-8", page))

	got, isErr := fetchURL(t, NewFetch("0.3.0-test", allowPrivate()), srv.URL+"/page", 0)
	if isErr {
		t.Fatalf("unexpected error: %s", got)
	}
	want := "# Title\n\nFirst paragraph.\n\n## Section\n\n- one\n- two docs (" + srv.URL + "/docs)\n\n" +
		"See example (https://example.com/x).\n\nName | Value\ncpu | 2"
	if got != want {
		t.Fatalf("html extraction mismatch\n got: %q\nwant: %q", got, want)
	}
	for _, dropped := range []string{"Menu links", "var x", "Copyright", "color:red"} {
		if strings.Contains(got, dropped) {
			t.Fatalf("%q should be dropped from %q", dropped, got)
		}
	}
}

func TestFetchJSONPassesThrough(t *testing.T) {
	body := `{"name": "groot", "tags": ["sre", "cli"]}`
	srv := fetchServer(t, serveBody("application/json", body))

	got, isErr := fetchURL(t, NewFetch("v", allowPrivate()), srv.URL, 0)
	if isErr || got != body {
		t.Fatalf("json passthrough: got %q (err=%v), want %q", got, isErr, body)
	}
}

func TestFetchPlainTextPassesThrough(t *testing.T) {
	body := "line one\n\n  line   two  "
	srv := fetchServer(t, serveBody("text/plain", body))

	got, isErr := fetchURL(t, NewFetch("v", allowPrivate()), srv.URL, 0)
	if isErr || got != "line one\n\n  line   two" {
		t.Fatalf("text passthrough: got %q (err=%v)", got, isErr)
	}
}

func TestFetchRejectsUnsupportedContentType(t *testing.T) {
	srv := fetchServer(t, serveBody("application/pdf", "%PDF-1.4"))

	got, isErr := fetchURL(t, NewFetch("v", allowPrivate()), srv.URL, 0)
	if !isErr || !strings.Contains(got, "unsupported content type") {
		t.Fatalf("want unsupported content type error, got %q (err=%v)", got, isErr)
	}
}

func TestFetchTruncatesToMaxChars(t *testing.T) {
	srv := fetchServer(t, serveBody("text/plain", strings.Repeat("a", 100)))

	got, isErr := fetchURL(t, NewFetch("v", allowPrivate()), srv.URL, 10)
	want := strings.Repeat("a", 10) + "\n[truncated: 90 more chars]"
	if isErr || got != want {
		t.Fatalf("truncation: got %q (err=%v), want %q", got, isErr, want)
	}
}

func TestFetchWithoutMaxCharsSpillsTheWholeText(t *testing.T) {
	srv := fetchServer(t, serveBody("text/plain", strings.Repeat("b", 33000)))
	f := NewFetch("v", allowPrivate())
	f.OutDir = t.TempDir()

	got, isErr := fetchURL(t, f, srv.URL, 0)
	if isErr || !strings.Contains(got, "[output: 33000 characters total;") {
		t.Fatalf("long page must spill: isErr=%v head %.80q", isErr, got)
	}
	saved, err := os.ReadFile(spilledPath(t, got))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(saved), "b") != 33000 {
		t.Fatalf("saved text must hold the whole page, got %d b", strings.Count(string(saved), "b"))
	}
}

func TestFetchCapsBodyAtOneMegabyte(t *testing.T) {
	srv := fetchServer(t, serveBody("text/plain", strings.Repeat("c", 3<<20)))
	f := NewFetch("v", allowPrivate())
	f.OutDir = t.TempDir()

	got, isErr := fetchURL(t, f, srv.URL, 4<<20)
	if isErr {
		t.Fatalf("unexpected error: %s", got)
	}
	if !strings.HasSuffix(got, "[body truncated at 1 MB]") {
		t.Fatalf("missing body cap marker, tail %q", got[len(got)-60:])
	}
	saved, err := os.ReadFile(spilledPath(t, got))
	if err != nil {
		t.Fatal(err)
	}
	body := strings.TrimSuffix(string(saved), "\n[body truncated at 1 MB]")
	if n := strings.Count(body, "c"); n != 1<<20 {
		t.Fatalf("read %d bytes, want exactly 1 MB", n)
	}
}

func TestFetchFollowsFiveRedirectsAndStopsAtSix(t *testing.T) {
	srv := fetchServer(t, func(w http.ResponseWriter, r *http.Request) {
		n, err := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/hop/"))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		if n == 0 {
			w.Header().Set("Content-Type", "text/plain")
			fmt.Fprint(w, "arrived")
			return
		}
		http.Redirect(w, r, fmt.Sprintf("/hop/%d", n-1), http.StatusFound)
	})
	f := NewFetch("v", allowPrivate())

	if got, isErr := fetchURL(t, f, srv.URL+"/hop/5", 0); isErr || got != "arrived" {
		t.Fatalf("five redirects should succeed: got %q (err=%v)", got, isErr)
	}
	got, isErr := fetchURL(t, f, srv.URL+"/hop/6", 0)
	if !isErr || !strings.Contains(got, "stopped after 5 redirects") {
		t.Fatalf("six redirects should stop: got %q (err=%v)", got, isErr)
	}
}

func TestFetchRefusesPrivateAddressByDefault(t *testing.T) {
	srv := fetchServer(t, serveBody("text/plain", "secret"))

	got, isErr := fetchURL(t, NewFetch("v", FetchPolicy{}), srv.URL, 0)
	if !isErr || !strings.Contains(got, "private address") || strings.Contains(got, "secret") {
		t.Fatalf("loopback must be refused by default: got %q (err=%v)", got, isErr)
	}
}

func TestFetchSendsUserAgentAndNoCookies(t *testing.T) {
	var mu sync.Mutex
	var agents, cookies []string
	srv := fetchServer(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		agents = append(agents, r.Header.Get("User-Agent"))
		cookies = append(cookies, r.Header.Get("Cookie"))
		mu.Unlock()
		http.SetCookie(w, &http.Cookie{Name: "sid", Value: "1"})
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, "ok")
	})
	f := NewFetch("0.3.0-test", allowPrivate())
	fetchURL(t, f, srv.URL, 0)
	fetchURL(t, f, srv.URL, 0)

	mu.Lock()
	defer mu.Unlock()
	if len(agents) != 2 || agents[0] != "groot-cli/0.3.0-test" {
		t.Fatalf("user agent: %v", agents)
	}
	if cookies[1] != "" {
		t.Fatalf("cookie jar must not replay cookies, got %q", cookies[1])
	}
}

func TestFetchRejectsNonHTTPSchemes(t *testing.T) {
	f := NewFetch("v", allowPrivate())
	for _, raw := range []string{"file:///etc/hosts", "ftp://example.com/x", "javascript:alert(1)"} {
		got, isErr := fetchURL(t, f, raw, 0)
		if !isErr || !strings.Contains(got, "only http and https") {
			t.Fatalf("%s: got %q (err=%v)", raw, got, isErr)
		}
	}
}

func TestFetchRefusesEmbeddedCredentials(t *testing.T) {
	got, isErr := fetchURL(t, NewFetch("v", allowPrivate()), "https://user:pw@example.com/", 0)
	if !isErr || !strings.Contains(got, "credentials") {
		t.Fatalf("embedded credentials must be refused: got %q (err=%v)", got, isErr)
	}
}

func TestFetchRequiresURL(t *testing.T) {
	got, isErr := NewFetch("v", allowPrivate()).Execute(context.Background(), `{}`)
	if !isErr || !strings.Contains(got, "url is required") {
		t.Fatalf("got %q (err=%v)", got, isErr)
	}
}

func TestFetchReportsHTTPErrorStatus(t *testing.T) {
	srv := fetchServer(t, func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) })

	got, isErr := fetchURL(t, NewFetch("v", allowPrivate()), srv.URL+"/missing", 0)
	if !isErr || !strings.Contains(got, "404") {
		t.Fatalf("got %q (err=%v)", got, isErr)
	}
}

func TestFetchDenyListBlocksRedirectTarget(t *testing.T) {
	target := fetchServer(t, serveBody("text/plain", "inside"))
	_, port, _ := net.SplitHostPort(strings.TrimPrefix(target.URL, "http://"))
	srv := fetchServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://localhost:"+port+"/", http.StatusFound)
	})
	f := NewFetch("v", FetchPolicy{AllowPrivate: true, Deny: []string{"localhost"}})

	got, isErr := fetchURL(t, f, srv.URL, 0)
	if !isErr || !strings.Contains(got, "denied") || strings.Contains(got, "inside") {
		t.Fatalf("redirect into a denied domain must be refused: got %q (err=%v)", got, isErr)
	}
}

func TestFetchDenyListBlocksDomain(t *testing.T) {
	srv := fetchServer(t, serveBody("text/plain", "x"))
	f := NewFetch("v", FetchPolicy{AllowPrivate: true, Deny: []string{"127.0.0.1"}})

	got, isErr := fetchURL(t, f, srv.URL, 0)
	if !isErr || !strings.Contains(got, "denied") {
		t.Fatalf("got %q (err=%v)", got, isErr)
	}
}

func TestFetchAllowListRestrictsDomains(t *testing.T) {
	srv := fetchServer(t, serveBody("text/plain", "x"))

	got, isErr := fetchURL(t, NewFetch("v", FetchPolicy{AllowPrivate: true, Allow: []string{"example.com"}}), srv.URL, 0)
	if !isErr || !strings.Contains(got, "not in fetch.allow") {
		t.Fatalf("got %q (err=%v)", got, isErr)
	}
	got, isErr = fetchURL(t, NewFetch("v", FetchPolicy{AllowPrivate: true, Allow: []string{"127.0.0.1"}}), srv.URL, 0)
	if isErr || got != "x" {
		t.Fatalf("allowed domain should pass: got %q (err=%v)", got, isErr)
	}
}

func TestDomainPolicySuffixMatchAndDenyWins(t *testing.T) {
	p := FetchPolicy{Allow: []string{"example.com"}, Deny: []string{"internal.example.com"}}
	cases := map[string]bool{
		"example.com":               true,
		"www.example.com":           true,
		"internal.example.com":      false,
		"deep.internal.example.com": false,
		"evil-example.com":          false,
		"other.org":                 false,
	}
	for host, want := range cases {
		if err := p.checkDomain(host); (err == nil) != want {
			t.Fatalf("checkDomain(%s) = %v, want allowed=%v", host, err, want)
		}
	}
	open := FetchPolicy{}
	if err := open.checkDomain("anything.test"); err != nil {
		t.Fatalf("empty allow list must allow any domain: %v", err)
	}
}

func TestIsPrivateIP(t *testing.T) {
	cases := map[string]bool{
		"127.0.0.1":       true,
		"10.1.2.3":        true,
		"172.16.0.1":      true,
		"192.168.1.1":     true,
		"169.254.169.254": true,
		"::1":             true,
		"fd00::1":         true,
		"fe80::1":         true,
		"8.8.8.8":         false,
		"2606:4700::1111": false,
	}
	for raw, want := range cases {
		if got := isPrivateIP(net.ParseIP(raw)); got != want {
			t.Fatalf("isPrivateIP(%s) = %v, want %v", raw, got, want)
		}
	}
}

func TestFetchPreviewShowsURL(t *testing.T) {
	f := NewFetch("v", FetchPolicy{})
	if got := f.Preview(`{"url":"https://example.com/a"}`); !strings.Contains(got, "https://example.com/a") {
		t.Fatalf("preview: %q", got)
	}
	if f.Risk() != RiskRead || f.RiskFor(`{}`) != RiskRead {
		t.Fatal("fetch_url must be a read-risk tool")
	}
}
