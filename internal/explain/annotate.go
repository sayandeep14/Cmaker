package explain

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Annotation is one LLM-proposed explanatory comment to insert immediately
// before a specific line of the original source - never a rewrite of the
// code itself. This is what makes 'cmaker read --explain' safe to use
// unmodified alongside the exact code: the model is never asked to
// reproduce the code (the one thing every other AI feature in this
// codebase has learned, sometimes the hard way, not to trust an LLM to do
// verbatim), only to say where a comment belongs and what it should say.
// InsertAnnotations is the deterministic Go-side splice that actually
// applies them.
type Annotation struct {
	Line    int    `json:"line"`    // 1-based line number (in the numbered source given to the model) this comment goes immediately before
	Comment string `json:"comment"` // plain comment text, no comment syntax (//, /* */) - InsertAnnotations adds that
}

// annotateSystemPrompt deliberately asks for line-anchored comments, not a
// rewritten copy of the source - see Annotation's own doc for why.
const annotateSystemPrompt = `You are a C/C++ code reading assistant. You will be given source code with each line prefixed by its line number (e.g. "12: some code"). Identify the points where a brief explanatory comment would meaningfully help a reader understand the code - not every line, only where it adds real value: non-obvious logic, the purpose of a block, a subtle detail a reader might miss.

Respond with ONLY a JSON array (no markdown code fence, no prose before or after) of objects shaped exactly like:
[{"line": 12, "comment": "explanation text, no comment syntax"}]

"line" is the line number (from the given numbering) the comment should appear immediately before. Do not modify, rewrite, or repeat the code itself - you are only proposing comments to insert around it. If no comment would genuinely help, respond with an empty array: []`

// Annotate asks completer to propose explanatory comments for code (via
// annotateSystemPrompt) and returns the parsed annotations - the one call
// 'cmaker read --explain' actually needs; callers splice the result into
// the original source themselves via InsertAnnotations (kept as a
// separate deterministic step, not folded into this function, so it's
// independently testable without a Completer).
func Annotate(ctx context.Context, completer Completer, code string) ([]Annotation, error) {
	raw, err := completer.Complete(ctx, annotateSystemPrompt, BuildAnnotatePrompt(code))
	if err != nil {
		return nil, fmt.Errorf("LLM request failed: %w", err)
	}
	return ParseAnnotations(raw)
}

// BuildAnnotatePrompt numbers each line of code (1-based) and returns the
// user-content string ready for Annotate - the numbering is what lets
// ParseAnnotations' Line field map back to an exact position in the
// original, unmodified source.
func BuildAnnotatePrompt(code string) string {
	lines := strings.Split(code, "\n")
	var b strings.Builder
	b.WriteString("Explain this code by proposing where inline comments would help:\n\n")
	for i, line := range lines {
		fmt.Fprintf(&b, "%d: %s\n", i+1, line)
	}
	return b.String()
}

// ParseAnnotations decodes Ask's raw response into a slice of Annotation.
// Uses json.Decoder (not json.Unmarshal) for the same reason every other
// structured-output parser in this codebase does: a live model response
// has been observed wrapping JSON in a code fence and/or appending
// trailing prose after it (see internal/improvise, internal/describe) -
// Decoder parses just the first JSON value and tolerates anything after.
func ParseAnnotations(raw string) ([]Annotation, error) {
	raw = stripLeadingCodeFence(strings.TrimSpace(raw))
	var annotations []Annotation
	dec := json.NewDecoder(strings.NewReader(raw))
	if err := dec.Decode(&annotations); err != nil {
		return nil, fmt.Errorf("model response wasn't a valid JSON annotation array: %w\nresponse was:\n%s", err, raw)
	}
	return annotations, nil
}

// stripLeadingCodeFence removes a single leading "```" (optionally
// "```json") line, if present - the same defensive tolerance
// internal/improvise/internal/describe already apply to their own JSON
// responses.
func stripLeadingCodeFence(s string) string {
	if !strings.HasPrefix(s, "```") {
		return s
	}
	_, rest, ok := strings.Cut(s, "\n")
	if !ok {
		return s
	}
	return rest
}

// InsertAnnotations splices annotations into code as "// <comment>" lines,
// each indented to match the line it precedes and inserted immediately
// before that line - deterministic, Go-side, and untouched by anything the
// model said beyond the comment text itself. Annotations referencing a
// line outside code's actual range are silently dropped rather than
// corrupting the output; multiple annotations on the same line are all
// kept, in the order given.
func InsertAnnotations(code string, annotations []Annotation) string {
	lines := strings.Split(code, "\n")

	byLine := make(map[int][]string, len(annotations))
	for _, a := range annotations {
		if a.Line < 1 || a.Line > len(lines) {
			continue
		}
		comment := strings.TrimSpace(a.Comment)
		if comment == "" {
			continue
		}
		byLine[a.Line] = append(byLine[a.Line], comment)
	}
	if len(byLine) == 0 {
		return code
	}

	targetLines := make([]int, 0, len(byLine))
	for line := range byLine {
		targetLines = append(targetLines, line)
	}
	sort.Ints(targetLines)

	var out strings.Builder
	nextTarget := 0
	for i, line := range lines {
		lineNum := i + 1
		for nextTarget < len(targetLines) && targetLines[nextTarget] == lineNum {
			indent := leadingWhitespace(line)
			for _, comment := range byLine[lineNum] {
				out.WriteString(indent)
				out.WriteString("// ")
				out.WriteString(comment)
				out.WriteString("\n")
			}
			nextTarget++
		}
		out.WriteString(line)
		if i < len(lines)-1 {
			out.WriteString("\n")
		}
	}
	return out.String()
}

// leadingWhitespace returns s's leading run of spaces/tabs, used so an
// inserted comment lines up with the code it explains instead of always
// starting at column 0.
func leadingWhitespace(s string) string {
	i := 0
	for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	return s[:i]
}
