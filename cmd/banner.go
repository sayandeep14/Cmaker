package cmd

import (
	"fmt"
	"strings"
)

// banner.go renders a project name as a big, two-color block-letter
// banner for 'cmaker status' (see printStatusHeader) - the "impressive"
// header the dashboard opens with. A small self-contained bitmap font
// (no figlet/ascii-art dependency, matching this codebase's
// dependency-light ethos) rather than an embedded font file.

// bannerGlyphWidth/bannerGlyphHeight is every glyph's fixed bitmap size -
// 5x5 was chosen as the smallest size that still reads clearly as letters
// rather than abstract blocks.
const (
	bannerGlyphWidth  = 5
	bannerGlyphHeight = 5
	// bannerMaxChars caps how long a name can be before falling back to
	// plain bold text (see renderBanner) - past this, a 6-wide-per-glyph
	// banner routinely overflows a normal terminal width and wraps badly,
	// which looks far worse than not banner-ing at all.
	bannerMaxChars = 20
)

// bannerFont maps each supported (uppercase) rune to bannerGlyphHeight
// rows of bannerGlyphWidth characters, '#' for a filled pixel and '.' for
// empty. Any rune not in this map (lowercase is upper-cased first; a
// symbol with no glyph) renders as a blank column - see glyphFor.
var bannerFont = map[rune][]string{
	'A': {".###.", "#...#", "#####", "#...#", "#...#"},
	'B': {"####.", "#...#", "####.", "#...#", "####."},
	'C': {".####", "#....", "#....", "#....", ".####"},
	'D': {"####.", "#...#", "#...#", "#...#", "####."},
	'E': {"#####", "#....", "###..", "#....", "#####"},
	'F': {"#####", "#....", "###..", "#....", "#...."},
	'G': {".####", "#....", "#..##", "#...#", ".####"},
	'H': {"#...#", "#...#", "#####", "#...#", "#...#"},
	'I': {"#####", "..#..", "..#..", "..#..", "#####"},
	'J': {"..###", "...#.", "...#.", "#..#.", ".##.."},
	'K': {"#...#", "#..#.", "###..", "#..#.", "#...#"},
	'L': {"#....", "#....", "#....", "#....", "#####"},
	'M': {"#...#", "##.##", "#.#.#", "#...#", "#...#"},
	'N': {"#...#", "##..#", "#.#.#", "#..##", "#...#"},
	'O': {".###.", "#...#", "#...#", "#...#", ".###."},
	'P': {"####.", "#...#", "####.", "#....", "#...."},
	'Q': {".###.", "#...#", "#.#.#", "#..#.", ".##.#"},
	'R': {"####.", "#...#", "####.", "#..#.", "#...#"},
	'S': {".####", "#....", ".###.", "....#", "####."},
	'T': {"#####", "..#..", "..#..", "..#..", "..#.."},
	'U': {"#...#", "#...#", "#...#", "#...#", ".###."},
	'V': {"#...#", "#...#", "#...#", ".#.#.", "..#.."},
	'W': {"#...#", "#...#", "#.#.#", "##.##", "#...#"},
	'X': {"#...#", ".#.#.", "..#..", ".#.#.", "#...#"},
	'Y': {"#...#", ".#.#.", "..#..", "..#..", "..#.."},
	'Z': {"#####", "...#.", "..#..", ".#...", "#####"},
	'0': {".###.", "#...#", "#.#.#", "#...#", ".###."},
	'1': {"..#..", ".##..", "..#..", "..#..", "#####"},
	'2': {"####.", "....#", "..##.", ".#...", "#####"},
	'3': {"####.", "....#", "..##.", "....#", "####."},
	'4': {"#..#.", "#..#.", "#####", "...#.", "...#."},
	'5': {"#####", "#....", "####.", "....#", "####."},
	'6': {".####", "#....", "####.", "#...#", ".###."},
	'7': {"#####", "....#", "...#.", "..#..", "..#.."},
	'8': {".###.", "#...#", ".###.", "#...#", ".###."},
	'9': {".###.", "#...#", ".####", "....#", ".###."},
	'-': {".....", ".....", "#####", ".....", "....."},
	'_': {".....", ".....", ".....", ".....", "#####"},
	'.': {".....", ".....", ".....", ".....", "..#.."},
	' ': {".....", ".....", ".....", ".....", "....."},
}

// glyphFor returns r's bitmap (uppercased first), or a blank glyph if r
// isn't in bannerFont - so an unsupported character (e.g. an emoji, an
// accented letter) never breaks the banner, it just leaves a gap.
func glyphFor(r rune) []string {
	if g, ok := bannerFont[r]; ok {
		return g
	}
	upper := []rune(strings.ToUpper(string(r)))[0]
	if g, ok := bannerFont[upper]; ok {
		return g
	}
	blank := make([]string, bannerGlyphHeight)
	for i := range blank {
		blank[i] = strings.Repeat(".", bannerGlyphWidth)
	}
	return blank
}

// renderBanner renders name as a two-color block-letter banner: the
// letters themselves in mainColor, plus a one-row/one-column drop shadow
// in shadowColor behind them for a bit of depth - printed directly rather
// than returned, since colorize() already handles --no-color by
// stripping codes (the shadow simply becomes invisible overlap with
// spaces in that case, not broken output).
//
// Names longer than bannerMaxChars fall back to a single bold line -
// letting an arbitrarily long project name wrap across a whole banner's
// width looks far worse than skipping the banner outright.
func renderBanner(name string, mainColor, shadowColor string) {
	if name == "" || len([]rune(name)) > bannerMaxChars {
		fmt.Println(colorize(ansiBold, name))
		return
	}

	runes := []rune(name)
	glyphs := make([][]string, len(runes))
	for i, r := range runes {
		glyphs[i] = glyphFor(r)
	}

	const gap = 1
	totalWidth := len(glyphs)*(bannerGlyphWidth+gap) - gap
	if totalWidth <= 0 {
		return
	}

	// filled[row][col] is the main banner's own pixel grid, built once so
	// the shadow pass (offset +1 row, +1 col) can just re-index into it
	// rather than re-deriving glyph positions a second time.
	filled := make([][]bool, bannerGlyphHeight)
	for r := range filled {
		filled[r] = make([]bool, totalWidth)
	}
	col := 0
	for _, g := range glyphs {
		for r := 0; r < bannerGlyphHeight; r++ {
			for c := 0; c < bannerGlyphWidth; c++ {
				if g[r][c] == '#' {
					filled[r][col+c] = true
				}
			}
		}
		col += bannerGlyphWidth + gap
	}

	// The drop shadow only reads as depth when it's a distinct color from
	// the main glyph - with --no-color both layers would render as the
	// same solid block, one column/row apart, blurring letters together
	// rather than adding depth. Skip the shadow pass entirely in that
	// case (canvas shrinks back to exactly the glyph size) rather than
	// print something worse than no effect at all.
	withShadow := !flagNoColor
	canvasHeight, canvasWidth := bannerGlyphHeight, totalWidth
	if withShadow {
		canvasHeight, canvasWidth = bannerGlyphHeight+1, totalWidth+1
	}
	at := func(r, c int) bool {
		if r < 0 || r >= bannerGlyphHeight || c < 0 || c >= totalWidth {
			return false
		}
		return filled[r][c]
	}

	for r := 0; r < canvasHeight; r++ {
		var b strings.Builder
		// runStart/runIsMain/runIsShadow track the current same-color run
		// so consecutive block pixels are wrapped in one colorize() call
		// each, instead of one escape-code pair per single character.
		var run strings.Builder
		state := 0 // 0=space, 1=main, 2=shadow
		flush := func() {
			if run.Len() == 0 {
				return
			}
			switch state {
			case 1:
				b.WriteString(colorize(mainColor, run.String()))
			case 2:
				b.WriteString(colorize(shadowColor, run.String()))
			default:
				b.WriteString(run.String())
			}
			run.Reset()
		}
		for c := 0; c < canvasWidth; c++ {
			var next int
			switch {
			case at(r, c):
				next = 1
			case withShadow && at(r-1, c-1):
				next = 2
			default:
				next = 0
			}
			if next != state {
				flush()
				state = next
			}
			if next == 0 {
				run.WriteByte(' ')
			} else {
				run.WriteString("█")
			}
		}
		flush()
		fmt.Println(b.String())
	}
}
