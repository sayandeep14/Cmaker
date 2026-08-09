package cmd

import (
	"os"

	"github.com/charmbracelet/glamour"
	"golang.org/x/term"
)

// maxMarkdownWidth caps rendered width for readability on very wide
// terminals - matches common practice in other markdown-rendering CLIs
// (gh, glow) rather than word-wrapping prose across a whole ultrawide
// monitor.
const maxMarkdownWidth = 120

// renderMarkdown renders LLM-authored markdown prose (currently just
// 'cmaker explain's answers) into styled terminal output via glamour -
// headers, bold/italic, code fences (syntax-highlighted), lists, etc.
// rendered properly instead of printed as literal markdown syntax.
//
// Falls back to the raw, unrendered markdown text whenever stdout isn't a
// real terminal (piped/redirected - matches printDiff's own "never
// colorize when piped" reasoning in cmd/heal.go, so e.g. `cmaker explain
// ... > notes.md` still saves clean, valid, human-readable markdown
// source, not ANSI escape codes) or --no-color is set, and on any render
// error (never lose the actual answer just because rendering failed).
func renderMarkdown(s string) string {
	if flagNoColor || !term.IsTerminal(int(os.Stdout.Fd())) {
		return s
	}

	width := maxMarkdownWidth
	if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 0 && w < maxMarkdownWidth {
		width = w
	}

	renderer, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return s
	}

	rendered, err := renderer.Render(s)
	if err != nil {
		return s
	}
	return rendered
}
