package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/net/html"

	"github.com/tinchoram/groot-cli/internal/api"
)

const (
	fetchTimeout      = 15 * time.Second
	fetchMaxRedirects = 5
	fetchMaxBody      = 1 << 20
	fetchDefaultChars = 32000
)

// FetchPolicy constrains fetch_url. Allow and Deny are domain lists matched
// by suffix: "example.com" covers "docs.example.com". Deny wins over Allow, and
// an empty Allow means any domain. AllowPrivate lifts the refusal of loopback,
// RFC1918, link-local and unique-local addresses.
type FetchPolicy struct {
	AllowPrivate bool
	Allow        []string
	Deny         []string
}

// Fetch reads a web page or API response for the model.
type Fetch struct {
	client  *http.Client
	policy  FetchPolicy
	version string
}

// NewFetch returns the fetch_url tool. The User-Agent carries version.
func NewFetch(version string, policy FetchPolicy) *Fetch {
	f := &Fetch{policy: policy, version: version}
	f.client = &http.Client{
		Timeout:       fetchTimeout,
		CheckRedirect: f.checkRedirect,
		// No cookie jar, no proxy and no credentials: the request carries
		// only the User-Agent and Accept headers. The proxy stays off so the
		// dialer below always sees the real target address.
		Transport: &http.Transport{
			DialContext:     f.dialContext,
			IdleConnTimeout: 30 * time.Second,
		},
	}
	return f
}

func (*Fetch) Definition() api.ToolDef {
	return api.ToolDef{
		Name:        "fetch_url",
		Description: "Read a web page or API response over http or https (GET only). HTML is returned as readable text with links and tables kept; JSON and text pass through. Use it when the answer lives at a URL the user gave you.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"url":{"type":"string"},"max_chars":{"type":"integer"}},"required":["url"]}`),
	}
}

func (*Fetch) Risk() Risk            { return RiskRead }
func (f *Fetch) RiskFor(string) Risk { return f.Risk() }

func (*Fetch) Preview(input string) string {
	var in struct {
		URL string `json:"url"`
	}
	if json.Unmarshal([]byte(input), &in) != nil || in.URL == "" {
		return "fetch_url"
	}
	return "fetch " + in.URL
}

// Execute fetches the URL and returns its readable content.
func (f *Fetch) Execute(ctx context.Context, input string) (string, bool) {
	var in struct {
		URL      string `json:"url"`
		MaxChars int    `json:"max_chars"`
	}
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return "invalid input: " + err.Error(), true
	}
	if strings.TrimSpace(in.URL) == "" {
		return "url is required", true
	}
	u, err := url.Parse(strings.TrimSpace(in.URL))
	if err != nil {
		return "invalid url: " + err.Error(), true
	}
	if err := f.checkURL(u); err != nil {
		return err.Error(), true
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "invalid request: " + err.Error(), true
	}
	req.Header.Set("User-Agent", "groot-cli/"+f.version)
	req.Header.Set("Accept", "text/html, application/json, text/*;q=0.8")

	resp, err := f.client.Do(req)
	if err != nil {
		return "fetch failed: " + err.Error(), true
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "HTTP " + resp.Status, true
	}

	mediaType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil {
		mediaType = ""
	}
	isHTML := mediaType == "text/html"
	if !isHTML && mediaType != "application/json" && !strings.HasPrefix(mediaType, "text/") {
		return "unsupported content type: " + orNone(mediaType), true
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, fetchMaxBody+1))
	if err != nil {
		return "read body: " + err.Error(), true
	}
	capped := len(body) > fetchMaxBody
	if capped {
		body = body[:fetchMaxBody]
	}

	var text string
	if isHTML {
		text, err = htmlToText(body, resp.Request.URL)
		if err != nil {
			return "parse html: " + err.Error(), true
		}
	} else {
		text = strings.TrimSpace(string(body))
	}
	text = strings.ToValidUTF8(Redact(text), "�")
	if capped {
		text += "\n[body truncated at 1 MB]"
	}

	max := in.MaxChars
	if max <= 0 {
		max = fetchDefaultChars
	}
	return truncateRunes(text, max), false
}

func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}

// checkURL refuses anything the tool will not request: non-http schemes,
// embedded credentials and denied domains. Private addresses are checked at
// dial time, where the resolved IP is known.
func (f *Fetch) checkURL(u *url.URL) error {
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("only http and https URLs are supported, got %q", u.Scheme)
	}
	if u.User != nil {
		return errors.New("URLs with embedded credentials are refused")
	}
	if u.Hostname() == "" {
		return errors.New("url has no host")
	}
	return f.policy.checkDomain(u.Hostname())
}

func (f *Fetch) checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) > fetchMaxRedirects {
		return fmt.Errorf("stopped after %d redirects", fetchMaxRedirects)
	}
	return f.checkURL(req.URL)
}

// dialContext resolves the host, refuses private addresses unless allowed, and
// dials the resolved IP. Every connection passes here, so redirects and DNS
// answers cannot reach a private address through a name.
func (f *Fetch) dialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	if len(addrs) == 0 {
		return nil, fmt.Errorf("no addresses for %s", host)
	}
	if !f.policy.AllowPrivate {
		for _, a := range addrs {
			if isPrivateIP(a.IP) {
				return nil, fmt.Errorf("refusing to connect to private address %s (set fetch.allow_private to allow it)", a.IP)
			}
		}
	}
	d := &net.Dialer{Timeout: fetchTimeout}
	return d.DialContext(ctx, network, net.JoinHostPort(addrs[0].IP.String(), port))
}

// isPrivateIP covers loopback, RFC1918, link-local (169.254/16, fe80::/10),
// unique-local (fc00::/7) and the unspecified address.
func isPrivateIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified()
}

// checkDomain applies the deny and allow lists to host.
func (p FetchPolicy) checkDomain(host string) error {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	for _, d := range p.Deny {
		if domainMatches(host, d) {
			return fmt.Errorf("domain %s is denied by fetch.deny", host)
		}
	}
	if len(p.Allow) == 0 {
		return nil
	}
	for _, d := range p.Allow {
		if domainMatches(host, d) {
			return nil
		}
	}
	return fmt.Errorf("domain %s is not in fetch.allow", host)
}

func domainMatches(host, domain string) bool {
	domain = strings.TrimSuffix(strings.TrimPrefix(strings.ToLower(strings.TrimSpace(domain)), "."), ".")
	if domain == "" {
		return false
	}
	return host == domain || strings.HasSuffix(host, "."+domain)
}

// truncateRunes keeps the first max runes and notes how many were dropped.
func truncateRunes(s string, max int) string {
	n := utf8.RuneCountInString(s)
	if n <= max {
		return s
	}
	runes := []rune(s)
	return string(runes[:max]) + fmt.Sprintf("\n[truncated: %d more chars]", n-max)
}

// blockTags start and end a line of their own.
var blockTags = map[string]bool{
	"p": true, "div": true, "section": true, "article": true, "main": true,
	"header": true, "aside": true, "blockquote": true, "pre": true, "figure": true,
	"figcaption": true, "ul": true, "ol": true, "dl": true, "dt": true, "dd": true,
	"table": true, "caption": true, "hr": true, "form": true, "address": true,
	"details": true, "summary": true,
}

// skipTags hold no readable text for the model.
var skipTags = map[string]bool{
	"script": true, "style": true, "nav": true, "footer": true, "head": true,
	"noscript": true, "template": true,
}

// htmlToText extracts readable text. Headings become #, list items become -,
// links become "text (url)" with relative URLs resolved against base, and table
// rows become cells joined by " | ".
func htmlToText(body []byte, base *url.URL) (string, error) {
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	x := &htmlText{base: base}
	x.walk(doc)
	return normalizeLines(x.b.String()), nil
}

// newlineToSpace keeps source line breaks inside text from becoming blocks.
var newlineToSpace = strings.NewReplacer("\n", " ", "\r", " ", "\t", " ")

type htmlText struct {
	b    strings.Builder
	base *url.URL
}

func (x *htmlText) walk(n *html.Node) {
	if n.Type == html.TextNode {
		x.b.WriteString(newlineToSpace.Replace(n.Data))
		return
	}
	if n.Type != html.ElementNode {
		x.children(n)
		return
	}
	tag := n.Data
	if skipTags[tag] {
		return
	}
	switch {
	case isHeading(tag):
		x.para()
		x.b.WriteString(strings.Repeat("#", int(tag[1]-'0')) + " ")
		x.children(n)
		x.para()
	case tag == "li":
		x.block()
		x.b.WriteString("- ")
		x.children(n)
	case tag == "br":
		x.block()
	case tag == "tr":
		x.row(n)
	case tag == "a":
		x.link(n)
	case blockTags[tag]:
		x.para()
		x.children(n)
		x.para()
	default:
		x.children(n)
	}
}

func isHeading(tag string) bool {
	return len(tag) == 2 && tag[0] == 'h' && tag[1] >= '1' && tag[1] <= '6'
}

func (x *htmlText) children(n *html.Node) {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		x.walk(c)
	}
}

// block ends the current line. para also leaves a blank line between blocks.
func (x *htmlText) block() { x.b.WriteByte('\n') }

func (x *htmlText) para() { x.b.WriteString("\n\n") }

func (x *htmlText) row(n *html.Node) {
	var cells []string
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && (c.Data == "td" || c.Data == "th") {
			cells = append(cells, cellText(c, x.base))
		}
	}
	if len(cells) == 0 {
		return
	}
	x.block()
	x.b.WriteString(strings.Join(cells, " | "))
}

func cellText(n *html.Node, base *url.URL) string {
	sub := &htmlText{base: base}
	sub.children(n)
	return strings.Join(strings.Fields(sub.b.String()), " ")
}

func (x *htmlText) link(n *html.Node) {
	start := x.b.Len()
	x.children(n)
	text := strings.Join(strings.Fields(x.b.String()[start:]), " ")

	href := ""
	for _, a := range n.Attr {
		if a.Key == "href" {
			href = strings.TrimSpace(a.Val)
		}
	}
	if href == "" || strings.HasPrefix(href, "#") || strings.HasPrefix(strings.ToLower(href), "javascript:") {
		return
	}
	target, err := url.Parse(href)
	if err != nil {
		return
	}
	if x.base != nil {
		target = x.base.ResolveReference(target)
	}
	s := target.String()
	if text == "" || text == s {
		return
	}
	x.b.WriteString(" (" + s + ")")
}

// normalizeLines collapses whitespace in each line and keeps at most one blank
// line between blocks.
func normalizeLines(s string) string {
	var out []string
	blank := false
	for _, line := range strings.Split(s, "\n") {
		l := strings.Join(strings.Fields(line), " ")
		if l == "" {
			if len(out) > 0 && !blank {
				out = append(out, "")
				blank = true
			}
			continue
		}
		blank = false
		out = append(out, l)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}
