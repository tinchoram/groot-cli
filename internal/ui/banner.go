package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// wordmarkLetters holds the block letters G, R, O, O, T. Each letter is six
// rows, nine cells wide. Solid blocks render green; shadow glyphs brown.
var wordmarkLetters = [5][6]string{
	{" ██████╗ ", "██╔════╝ ", "██║  ███╗", "██║   ██║", "╚██████╔╝", " ╚═════╝ "},
	{"██████╗  ", "██╔══██╗ ", "██████╔╝ ", "██╔══██╗ ", "██║  ██║ ", "╚═╝  ╚═╝ "},
	{" ██████╗ ", "██╔═══██╗", "██║   ██║", "██║   ██║", "╚██████╔╝", " ╚═════╝ "},
	{" ██████╗ ", "██╔═══██╗", "██║   ██║", "██║   ██║", "╚██████╔╝", " ╚═════╝ "},
	{"████████╗", "╚══██╔══╝", "   ██║   ", "   ██║   ", "   ██║   ", "   ╚═╝   "},
}

// bannerArt is the assembled wordmark: one string per row, letters separated
// by one cell.
var bannerArt = assembleWordmark()

func assembleWordmark() [6]string {
	var rows [6]string
	for r := range rows {
		parts := make([]string, 0, len(wordmarkLetters)*2)
		for i, letter := range wordmarkLetters {
			if i > 0 {
				parts = append(parts, " ")
			}
			parts = append(parts, letter[r])
		}
		rows[r] = strings.Join(parts, "")
	}
	return rows
}

// tagline is the one-line description printed under the wordmark.
const tagline = "Terminal Agent for Devs and Platform engineering"

// mascotPixels is a pixel-art sapling in a pot, drawn to the left of the
// wordmark. One character per pixel: G leaf, B bark, D brow, K outline/eye,
// W eye shine, P pot soil, . transparent. Rows are paired into half-block
// cells at render time, so 20 pixel rows become 10 terminal rows.
var mascotPixels = [20]string{
	"..G...G.G.",
	".GBG.GBGG.",
	".BBBBBBB..",
	"GBBBBBBBG.",
	"BBDBBDBB..",
	"BKKBBKKB..",
	"BKWBBKWB..",
	"BBBBBBBB..",
	"BBBKKBBB..",
	".BBBBBB...",
	"..BBBB....",
	"GBBBBBBBG.",
	".B..BB..B.",
	"....BB....",
	"....BB....",
	"..KKKKK...",
	"..KPPPK...",
	"..KPPPK...",
	"..KPPPK...",
	"..KKKKK...",
}

// mascotPalette maps a pixel code to its color. Transparent pixels are absent.
var mascotPalette = map[byte]string{
	'G': colorGreen,
	'B': colorBrown,
	'D': colorDarkBrown,
	'K': "#111111",
	'W': "#F5F5F5",
	'P': colorTan,
}

// mascotArt is the mascot rendered as plain half-block rows, without color.
// It is kept for layout math and tests; renderMascotRow adds the colors.
var mascotArt = assembleMascot()

func assembleMascot() []string {
	rows := make([]string, 0, len(mascotPixels)/2)
	for r := 0; r+1 < len(mascotPixels); r += 2 {
		var sb strings.Builder
		for c := range mascotPixels[r] {
			sb.WriteString(strings.Repeat(halfBlock(mascotPixels[r][c], mascotPixels[r+1][c]), mascotPixelWidth))
		}
		rows = append(rows, sb.String())
	}
	return rows
}

// halfBlock picks the glyph for one terminal cell holding two vertical pixels.
func halfBlock(top, bottom byte) string {
	_, topOn := mascotPalette[top]
	_, bottomOn := mascotPalette[bottom]
	switch {
	case topOn && bottomOn, topOn:
		return "▀"
	case bottomOn:
		return "▄"
	default:
		return " "
	}
}

// mascotGap is the number of cells between the mascot and the wordmark.
const mascotGap = 3

// mascotPixelWidth is how many cells wide one pixel is. Two cells per pixel
// keeps the figure chunky and readable next to the wordmark.
const mascotPixelWidth = 2

// renderMascotRow colors one terminal row of the mascot: the upper half-block
// takes the top pixel as foreground and the bottom pixel as background.
func renderMascotRow(row int) string {
	top := mascotPixels[row*2]
	bottom := mascotPixels[row*2+1]
	var sb strings.Builder
	for c := range top {
		fg, topOn := mascotPalette[top[c]]
		bg, bottomOn := mascotPalette[bottom[c]]
		style := lipgloss.NewStyle()
		switch {
		case topOn && bottomOn:
			style = style.Foreground(lipgloss.Color(fg)).Background(lipgloss.Color(bg))
		case topOn:
			style = style.Foreground(lipgloss.Color(fg))
		case bottomOn:
			style = style.Foreground(lipgloss.Color(bg))
		}
		sb.WriteString(style.Render(strings.Repeat(halfBlock(top[c], bottom[c]), mascotPixelWidth)))
	}
	return sb.String()
}

const (
	// bannerMinWidth is the narrowest terminal that gets the mascot and the
	// full wordmark.
	bannerMinWidth = 76
	// wordmarkMinWidth is the narrowest terminal that gets the block letters
	// (without the mascot); below it the banner is a single line.
	wordmarkMinWidth = 52
	// shineTotalTicks and shineFrameMs set the startup sweep: about 2.4 s.
	shineTotalTicks = 60
	shineFrameMs    = 40
	// shineEdgePad is how far the sweep overshoots each side of the wordmark.
	shineEdgePad = 15
	// shineDisabled is the shine column that renders the settled banner.
	shineDisabled = -1
)

// shineGradient runs from the sweep core to the resting green. The last entry
// is the resting color.
var shineGradient = []string{"#F0FDF4", "#DCFCE7", "#BBF7D0", "#86EFAC", "#4ADE80", colorGreen}

var shineStyles = func() []lipgloss.Style {
	out := make([]lipgloss.Style, len(shineGradient))
	for i, c := range shineGradient {
		out[i] = lipgloss.NewStyle().Foreground(lipgloss.Color(c)).Bold(true)
	}
	return out
}()

const shadowGlyphs = "╗╝╔╚═║"

func isShadowGlyph(r rune) bool {
	return strings.ContainsRune(shadowGlyphs, r)
}

// shineIndex picks the gradient entry for a wordmark cell. The sweep is
// diagonal: each row is offset by one column, so the streak slants. Cells
// behind the sweep fade at half the rate of cells ahead, which leaves a trail.
func shineIndex(col, row, shine int) int {
	last := len(shineGradient) - 1
	if shine < 0 {
		return last
	}
	diff := col + row - shine
	d := diff
	if diff < 0 {
		d = (-diff + 1) / 2
	}
	if d >= last {
		return last
	}
	return d
}

// easeInOutCubic maps t in [0,1] to [0,1] with a slow entrance and exit.
func easeInOutCubic(t float64) float64 {
	if t <= 0 {
		return 0
	}
	if t >= 1 {
		return 1
	}
	if t < 0.5 {
		return 4 * t * t * t
	}
	p := 2*t - 2
	return 0.5*p*p*p + 1
}

func renderBannerRow(line string, row, shine int) string {
	var sb strings.Builder
	col := 0
	for _, r := range line {
		switch {
		case r == ' ':
			sb.WriteRune(r)
		case isShadowGlyph(r):
			sb.WriteString(styleShadow.Render(string(r)))
		default:
			sb.WriteString(shineStyles[shineIndex(col, row, shine)].Render(string(r)))
		}
		col++
	}
	return sb.String()
}

// bannerHint is the key hint printed under the tagline.
const bannerHint = "type a message · /help for commands · ctrl-d to exit"

// versionTag is the version as the banner prints it, or "" when the build
// carries no version. A terminal has one font size, so "smaller" is faint.
func versionTag(version string) string {
	if strings.TrimSpace(version) == "" {
		return ""
	}
	return styleDim.Render("v" + version)
}

// underName are the lines printed under the name, left aligned with it: the
// tagline, the hint, then the version when the build has one.
func underName(version, hint string) []string {
	lines := []string{styleDim.Render(tagline), styleDim.Render(hint)}
	if v := versionTag(version); v != "" {
		lines = append(lines, v)
	}
	return lines
}

// bannerView renders the banner for a terminal of the given width. shine is
// the sweep column; pass shineDisabled for the settled frame. version is
// printed under the two text lines.
func bannerView(width, shine int, version string) string {
	if width < wordmarkMinWidth {
		var sb strings.Builder
		sb.WriteString("\n" + styleBanner.Render("  GROOT") + "\n")
		for _, line := range underName(version, "type a message or /help") {
			sb.WriteString("  " + line + "\n")
		}
		sb.WriteString("\n")
		return sb.String()
	}
	if width < bannerMinWidth {
		// Room for the letters but not the mascot: wordmark only.
		var sb strings.Builder
		sb.WriteString("\n")
		for row, line := range bannerArt {
			sb.WriteString(renderBannerRow(line, row, shine))
			sb.WriteString("\n")
		}
		for _, line := range underName(version, bannerHint) {
			sb.WriteString(line + "\n")
		}
		sb.WriteString("\n")
		return sb.String()
	}
	// The mascot is taller than the wordmark: the letters sit beside its head
	// and the tagline, hint and version beside its trunk and pot. The lines
	// under the name take the rows the mascot leaves, so the wordmark starts
	// one row higher when there is a version.
	under := underName(version, bannerHint)
	wordmarkTop := len(mascotArt) - len(bannerArt) - len(under)
	gap := strings.Repeat(" ", mascotGap)
	right := make([]string, len(mascotArt))
	for row, line := range bannerArt {
		right[wordmarkTop+row] = renderBannerRow(line, row, shine)
	}
	for i, line := range under {
		right[wordmarkTop+len(bannerArt)+i] = line
	}

	var sb strings.Builder
	sb.WriteString("\n")
	for row := range mascotArt {
		sb.WriteString(renderMascotRow(row))
		sb.WriteString(gap)
		sb.WriteString(right[row])
		sb.WriteString("\n")
	}
	sb.WriteString("\n")
	return sb.String()
}
