package ui

import (
	"strconv"
	"strings"
	"sync"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/ansi"
	"github.com/charmbracelet/glamour/styles"
)

// markdownStyle is glamour's dark style with the groot palette: headings and
// bullets green, strong text bright green, emphasis and code tan, links green
// and underlined, quotes and rules in the brown tones.
var markdownStyle = buildMarkdownStyle()

func buildMarkdownStyle() ansi.StyleConfig {
	cfg := styles.DarkStyleConfig
	green := colorGreen
	bright := colorGreenLight
	tan := colorTan
	brown := colorDarkBrown
	quote := "#8A7356"
	errRed := colorError
	yes := true
	zero := uint(0)

	cfg.Document.Margin = &zero
	cfg.Document.BlockPrefix = ""
	cfg.Document.BlockSuffix = ""

	for _, h := range []*ansi.StyleBlock{&cfg.Heading, &cfg.H1, &cfg.H2, &cfg.H3, &cfg.H4, &cfg.H5, &cfg.H6} {
		h.Color = &green
		h.Bold = &yes
		h.BackgroundColor = nil
		h.Prefix = ""
		h.Suffix = ""
	}
	cfg.Strong.Color = &bright
	cfg.Strong.Bold = &yes
	cfg.Emph.Color = &tan
	cfg.Emph.Italic = &yes
	cfg.Code.Color = &tan
	cfg.Code.BackgroundColor = nil
	cfg.Code.Prefix = ""
	cfg.Code.Suffix = ""
	cfg.BlockQuote.Color = &quote
	cfg.BlockQuote.Indent = &zero
	cfg.Item.BlockPrefix = "• "
	cfg.Item.Color = &green
	cfg.Enumeration.Color = &green
	cfg.HorizontalRule.Color = &brown
	cfg.Link.Color = &green
	cfg.Link.Underline = &yes
	cfg.LinkText.Color = &green
	cfg.LinkText.Bold = &yes
	cfg.CodeBlock.Margin = &zero
	cfg.CodeBlock.Chroma = &ansi.Chroma{
		Text:            ansi.StylePrimitive{Color: &tan},
		Keyword:         ansi.StylePrimitive{Color: &green},
		LiteralString:   ansi.StylePrimitive{Color: &tan},
		LiteralNumber:   ansi.StylePrimitive{Color: &bright},
		NameTag:         ansi.StylePrimitive{Color: &bright},
		Comment:         ansi.StylePrimitive{Color: &quote},
		Punctuation:     ansi.StylePrimitive{Color: &brown},
		GenericInserted: ansi.StylePrimitive{Color: &green},
		GenericDeleted:  ansi.StylePrimitive{Color: &errRed},
	}
	return cfg
}

// mdCache keeps rendered blocks by text and width. Rendering a block costs a
// full glamour pass, and the chat redraws on every frame.
type mdCache struct {
	mu sync.Mutex
	m  map[string]string
}

var renderedBlocks = &mdCache{m: map[string]string{}}

func cacheKey(raw string, width int) string {
	return strconv.Itoa(width) + "\x00" + raw
}

func (c *mdCache) get(raw string, width int) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.m[cacheKey(raw, width)]
	return v, ok
}

func (c *mdCache) put(raw string, width int, out string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[cacheKey(raw, width)] = out
}

// renderMarkdown renders raw as styled text wrapped to width. If glamour fails
// the raw text is returned, so a turn never disappears.
func renderMarkdown(raw string, width int) string {
	if width < 10 {
		width = 10
	}
	if out, ok := renderedBlocks.get(raw, width); ok {
		return out
	}
	tr, err := glamour.NewTermRenderer(glamour.WithStyles(markdownStyle), glamour.WithWordWrap(width))
	if err != nil {
		return raw
	}
	out, err := tr.Render(raw)
	if err != nil {
		return raw
	}
	out = strings.Trim(out, "\n")
	renderedBlocks.put(raw, width, out)
	return out
}
