package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/tinchoram/groot-cli/internal/api"
)

func TestWordmarkRowsAreAlignedAndSixHigh(t *testing.T) {
	if len(bannerArt) != 6 {
		t.Fatalf("wordmark must be 6 rows, got %d", len(bannerArt))
	}
	want := lipgloss.Width(bannerArt[0])
	for i, row := range bannerArt {
		if got := lipgloss.Width(row); got != want {
			t.Fatalf("row %d is %d cells, want %d", i, got, want)
		}
	}
	if want > bannerMinWidth {
		t.Fatalf("wordmark (%d cells) does not fit the minimum width %d", want, bannerMinWidth)
	}
}

func TestShadowGlyphsAreBrownAndBlocksAreGreen(t *testing.T) {
	for _, r := range "╗╝╔╚═║" {
		if !isShadowGlyph(r) {
			t.Errorf("%q should be a shadow glyph", r)
		}
	}
	for _, r := range "█ " {
		if isShadowGlyph(r) {
			t.Errorf("%q should not be a shadow glyph", r)
		}
	}
}

func TestShineColorPeaksAtCoreAndRestsAtBase(t *testing.T) {
	if got := shineIndex(10, 0, 10); got != 0 {
		t.Fatalf("peak cell should use the core color, got index %d", got)
	}
	if got := shineIndex(3, 0, -1); got != len(shineGradient)-1 {
		t.Fatalf("settled banner should use the base color, got %d", got)
	}
}

func TestUsageBlockFormatsCountsAndCost(t *testing.T) {
	out := usageBlock(api.Usage{InputTokens: 1234, OutputTokens: 56, CacheReadTokens: 7}, 0.0054)
	for _, want := range []string{"session usage:", "1,234", "56", "7", "$0.0054"} {
		if !strings.Contains(out, want) {
			t.Errorf("usage block missing %q:\n%s", want, out)
		}
	}
}

func TestCompactionNoticeShowsBeforeAndAfter(t *testing.T) {
	if got := compactionNotice(12, 5); !strings.Contains(got, "compacted: 12 → 5 messages") {
		t.Fatalf("notice = %q", got)
	}
}

func TestFormatThousands(t *testing.T) {
	cases := map[int]string{0: "0", 999: "999", 1000: "1,000", 1234567: "1,234,567"}
	for in, want := range cases {
		if got := formatThousands(in); got != want {
			t.Errorf("formatThousands(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestHighlightColorsDiffAndLeavesPlainTextAlone(t *testing.T) {
	diff := "--- a.txt\n+++ a.txt\n@@ -1 +1 @@\n-old\n+new\n"
	out := HighlightPayload(diff, 0)
	if !strings.Contains(out, "\x1b[") {
		t.Fatalf("diff should be colored, got %q", out)
	}
	if !strings.Contains(stripANSI(out), "+new") {
		t.Fatalf("diff text lost: %q", stripANSI(out))
	}
	plain := "just some words"
	if HighlightPayload(plain, 0) != plain {
		t.Fatal("plain text must pass through unchanged")
	}
}

func TestHighlightJSON(t *testing.T) {
	out := HighlightPayload(`{"replicas": 3}`, 0)
	if !strings.Contains(stripANSI(out), `"replicas"`) || !strings.Contains(out, "\x1b[") {
		t.Fatalf("json should be highlighted: %q", out)
	}
}

// stripANSI removes SGR sequences so tests can compare visible text.
func stripANSI(s string) string {
	var b strings.Builder
	in := false
	for _, r := range s {
		switch {
		case r == '\x1b':
			in = true
		case in && r == 'm':
			in = false
		case !in:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func TestMascotRowsAreAlignedAndTallerThanWordmark(t *testing.T) {
	if len(mascotPixels)%2 != 0 {
		t.Fatalf("mascot pixel rows must pair into half blocks, got %d rows", len(mascotPixels))
	}
	if len(mascotArt) < len(bannerArt)+4 {
		t.Fatalf("mascot (%d rows) must leave room for the wordmark, tagline and hint", len(mascotArt))
	}
	want := lipgloss.Width(mascotArt[0])
	for i, row := range mascotArt {
		if got := lipgloss.Width(row); got != want {
			t.Fatalf("mascot row %d is %d cells, want %d", i, got, want)
		}
	}
	if want+mascotGap+lipgloss.Width(bannerArt[0]) > bannerMinWidth {
		t.Fatalf("mascot and wordmark do not fit the minimum width %d", bannerMinWidth)
	}
}

// col returns the column at which s first appears in its line of out, or -1.
func col(out, s string) int {
	for _, line := range strings.Split(out, "\n") {
		if i := strings.Index(line, s); i >= 0 {
			return lipgloss.Width(line[:i])
		}
	}
	return -1
}

func TestBannerShowsTaglineAndMascot(t *testing.T) {
	for _, width := range []int{30, 120} {
		out := bannerView(width, shineDisabled, "0.5.4-dev")
		if !strings.Contains(out, tagline) {
			t.Errorf("width %d: banner lacks the tagline %q", width, tagline)
		}
	}
	wide := bannerView(120, shineDisabled, "0.5.4-dev")
	for _, width := range []int{30, 60, 120} {
		out := stripANSI(bannerView(width, shineDisabled, "0.5.4-dev"))
		if !strings.Contains(out, "v0.5.4-dev") {
			t.Errorf("width %d: banner lacks the version", width)
		}
		if strings.Index(out, "v0.5.4-dev") < strings.Index(out, "GROOT") && strings.Index(out, "v0.5.4-dev") < strings.Index(out, "██") {
			t.Errorf("width %d: the version goes under the name, not before it", width)
		}
		if strings.Index(out, "v0.5.4-dev") > strings.Index(out, tagline) {
			t.Errorf("width %d: the version goes above the tagline", width)
		}
		// Left aligned with the tagline: both lines start at the same column.
		if col(out, "v0.5.4-dev") != col(out, tagline) {
			t.Errorf("width %d: version at column %d, tagline at %d", width, col(out, "v0.5.4-dev"), col(out, tagline))
		}
	}
	if tagline != "Terminal Agent for Devs and Platform engineering" {
		t.Errorf("tagline = %q", tagline)
	}
	if out := stripANSI(bannerView(120, shineDisabled, "")); strings.Contains(out, " v") {
		t.Errorf("empty version prints nothing: %q", out)
	}
	if !strings.Contains(wide, "▀") && !strings.Contains(wide, "▄") {
		t.Errorf("wide banner lacks the pixel-art mascot")
	}
}
