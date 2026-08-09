package cmd

import (
	"bytes"
	"os"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"golang.org/x/term"
)

// highlightStyle is fixed rather than auto light/dark-detected the way
// cmd/markdown.go's glamour rendering is - chroma has no equivalent
// terminal-background probe, and "monokai" (a dark-background style)
// reads fine on light terminals too since it's a full ANSI truecolor
// palette, not just a couple of ambient tone adjustments.
const highlightStyle = "monokai"

// highlightCode syntax-highlights code for 'cmaker read' via chroma
// (already a transitive dependency through glamour, used here directly
// since this is raw source, not markdown to render). filename is used
// only to guess the language via extension - falls back to a C++ lexer
// (this codebase's own domain) when nothing matches, and returns code
// completely unchanged whenever stdout isn't a real terminal, --no-color
// is set, or highlighting fails for any reason - the actual code must
// always still be shown even if it can't be prettified.
func highlightCode(code, filename string) string {
	if flagNoColor || !term.IsTerminal(int(os.Stdout.Fd())) {
		return code
	}

	lexer := lexers.Match(filename)
	if lexer == nil {
		lexer = lexers.Get("cpp")
	}
	if lexer == nil {
		return code
	}
	lexer = chroma.Coalesce(lexer)

	iterator, err := lexer.Tokenise(nil, code)
	if err != nil {
		return code
	}

	style := styles.Get(highlightStyle)
	if style == nil {
		style = styles.Fallback
	}
	formatter := formatters.Get("terminal16m")
	if formatter == nil {
		return code
	}

	var buf bytes.Buffer
	if err := formatter.Format(&buf, style, iterator); err != nil {
		return code
	}
	return buf.String()
}
