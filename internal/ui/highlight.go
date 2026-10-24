package ui

import (
	"bytes"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/charmbracelet/lipgloss"
)

// grootStyle colors syntax tokens with the green and brown palette. Diff
// additions are green, deletions red, and hunk and file headers tan.
var grootStyle = chroma.MustNewStyle("groot", chroma.StyleEntries{
	chroma.Keyword:           colorGreen,
	chroma.LiteralString:     colorTan,
	chroma.LiteralNumber:     colorGreenLight,
	chroma.NameTag:           colorGreenLight,
	chroma.Punctuation:       colorDarkBrown,
	chroma.GenericInserted:   colorGreen,
	chroma.GenericDeleted:    colorError,
	chroma.GenericSubheading: colorTan,
	chroma.GenericHeading:    colorTan,
})

// HighlightPayload colors content when it looks like JSON or a unified diff,
// and returns it unchanged otherwise. When width is positive the result is
// also wrapped to it. The wrap keeps color codes intact.
func HighlightPayload(content string, width int) string {
	if lang := detectLanguage(content); lang != "" {
		content = highlightAs(lang, content)
	}
	if width > 0 {
		content = lipgloss.NewStyle().Width(width).Render(content)
	}
	return content
}

// highlightAs runs content through the lexer for lang. On any failure it
// returns the input unchanged.
func highlightAs(lang, content string) string {
	lexer := lexers.Get(lang)
	if lexer == nil {
		return content
	}
	it, err := lexer.Tokenise(nil, content)
	if err != nil {
		return content
	}
	var buf bytes.Buffer
	if err := formatters.Get("terminal16m").Format(&buf, grootStyle, it); err != nil {
		return content
	}
	return buf.String()
}

// detectLanguage picks a lexer name from the first non-space character, or
// from a leading "--- " diff header. It returns "" when no lexer applies.
func detectLanguage(s string) string {
	t := strings.TrimSpace(s)
	switch {
	case t == "":
		return ""
	case t[0] == '{' || t[0] == '[':
		return "json"
	case strings.HasPrefix(t, "--- "):
		return "diff"
	}
	return ""
}
