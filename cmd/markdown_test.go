package cmd

import "testing"

// TestRenderMarkdownFallsBackWhenNotATerminal exercises the same fast path
// a piped/redirected 'cmaker explain ... | less' (or `go test` itself,
// whose stdout is never a real terminal) hits - raw markdown returned
// unchanged, never handed to glamour at all.
func TestRenderMarkdownFallsBackWhenNotATerminal(t *testing.T) {
	in := "## Header\n\nSome **bold** text."
	got := renderMarkdown(in)
	if got != in {
		t.Errorf("renderMarkdown() = %q, want unchanged input when stdout isn't a terminal", got)
	}
}

func TestRenderMarkdownFallsBackWithNoColor(t *testing.T) {
	orig := flagNoColor
	flagNoColor = true
	defer func() { flagNoColor = orig }()

	in := "**bold**"
	if got := renderMarkdown(in); got != in {
		t.Errorf("renderMarkdown() with --no-color = %q, want unchanged input", got)
	}
}
