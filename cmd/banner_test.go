package cmd

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

// captureStdout redirects os.Stdout for the duration of fn and returns
// everything it printed - renderBanner writes via fmt.Println directly
// (matching every other status.go print helper), so tests need to
// capture the real stdout rather than a passed-in io.Writer.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	old := os.Stdout
	os.Stdout = w
	fn()
	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	io.Copy(&buf, r)
	return buf.String()
}

func stripANSI(s string) string {
	var b strings.Builder
	inEscape := false
	for _, r := range s {
		switch {
		case r == '\x1b':
			inEscape = true
		case inEscape && r == 'm':
			inEscape = false
		case !inEscape:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func TestRenderBannerNoColorHasNoShadowBleed(t *testing.T) {
	flagNoColor = true
	defer func() { flagNoColor = false }()

	out := captureStdout(t, func() { renderBanner("HI", ansiCyan, ansiBlue) })
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != bannerGlyphHeight {
		t.Fatalf("renderBanner() with --no-color printed %d lines, want %d (no shadow row)", len(lines), bannerGlyphHeight)
	}
	for _, l := range lines {
		if strings.Contains(l, "\x1b") {
			t.Errorf("renderBanner() with --no-color emitted an ANSI escape: %q", l)
		}
	}
}

func TestRenderBannerWithColorAddsShadowRow(t *testing.T) {
	out := captureStdout(t, func() { renderBanner("HI", ansiCyan, ansiBlue) })
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != bannerGlyphHeight+1 {
		t.Fatalf("renderBanner() with color printed %d lines, want %d (glyph height + shadow row)", len(lines), bannerGlyphHeight+1)
	}
}

func TestRenderBannerLowercaseMatchesUppercase(t *testing.T) {
	flagNoColor = true
	defer func() { flagNoColor = false }()

	lower := captureStdout(t, func() { renderBanner("hi", ansiCyan, ansiBlue) })
	upper := captureStdout(t, func() { renderBanner("HI", ansiCyan, ansiBlue) })
	if lower != upper {
		t.Errorf("renderBanner(%q) != renderBanner(%q):\n%s\nvs\n%s", "hi", "HI", lower, upper)
	}
}

func TestRenderBannerUnsupportedCharDoesntPanic(t *testing.T) {
	flagNoColor = true
	defer func() { flagNoColor = false }()

	out := captureStdout(t, func() { renderBanner("A@Z", ansiCyan, ansiBlue) })
	if strings.TrimSpace(stripANSI(out)) == "" {
		t.Error("renderBanner() with an unsupported character produced no output at all")
	}
}

func TestRenderBannerFallsBackForLongNames(t *testing.T) {
	longName := strings.Repeat("x", bannerMaxChars+5)
	out := captureStdout(t, func() { renderBanner(longName, ansiCyan, ansiBlue) })
	if !strings.Contains(stripANSI(out), longName) {
		t.Errorf("renderBanner() with a name over bannerMaxChars should fall back to plain text containing the name, got %q", out)
	}
	if strings.Count(out, "\n") != 1 {
		t.Errorf("renderBanner() fallback should print exactly one line, got %d newlines: %q", strings.Count(out, "\n"), out)
	}
}

func TestRenderBannerEmptyName(t *testing.T) {
	out := captureStdout(t, func() { renderBanner("", ansiCyan, ansiBlue) })
	if strings.TrimSpace(stripANSI(out)) != "" {
		t.Errorf("renderBanner(\"\") = %q, want effectively empty (ANSI codes aside)", out)
	}
}

func TestGlyphForKnownAndUnknown(t *testing.T) {
	if g := glyphFor('A'); len(g) != bannerGlyphHeight {
		t.Errorf("glyphFor('A') has %d rows, want %d", len(g), bannerGlyphHeight)
	}
	unknown := glyphFor('@')
	if len(unknown) != bannerGlyphHeight {
		t.Errorf("glyphFor('@') has %d rows, want %d", len(unknown), bannerGlyphHeight)
	}
	for _, row := range unknown {
		if strings.Contains(row, "#") {
			t.Errorf("glyphFor('@') (unsupported) should be a blank glyph, got row %q", row)
		}
	}
}

func TestBannerFontGlyphsAreWellFormed(t *testing.T) {
	for ch, glyph := range bannerFont {
		if len(glyph) != bannerGlyphHeight {
			t.Errorf("bannerFont[%q] has %d rows, want %d", ch, len(glyph), bannerGlyphHeight)
		}
		for _, row := range glyph {
			if len(row) != bannerGlyphWidth {
				t.Errorf("bannerFont[%q] row %q has length %d, want %d", ch, row, len(row), bannerGlyphWidth)
			}
			for _, c := range row {
				if c != '#' && c != '.' {
					t.Errorf("bannerFont[%q] row %q has unexpected character %q", ch, row, c)
				}
			}
		}
	}
}
