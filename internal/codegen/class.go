// Package codegen implements cmaker's `generate accessors` command: it
// locates a class/struct's body in a C++ source file (ExtractClassBody),
// asks an LLM to identify which data members should get accessors
// (ExtractMembers, member.go), and deterministically renders + inserts the
// resulting getter/setter methods (RenderAccessors/InsertAccessors,
// render.go). The LLM is only ever trusted to answer "what members exist
// and what shape are they" - the actual C++ text that lands in the file is
// always produced by Go code from a fixed template, never LLM output
// verbatim.
package codegen

import (
	"fmt"
	"regexp"
)

// ExtractClassBody locates a `class <className>`/`struct <className>`
// declaration in src and returns the byte offsets of its opening `{` and
// matching closing `}` (bodyOpen, bodyClose - both inclusive of the brace
// characters themselves).
//
// This is a heuristic scan, not a real C++ parser (see ROADMAP.md §19 for
// why a full parser was deliberately avoided): it balances braces while
// skipping over line/block comments and string/char literals, which is
// enough to find a class's true extent even when its body contains nested
// braces (member function bodies, nested types, initializer lists). It does
// not understand templates, preprocessor conditionals, or macros that
// themselves expand to braces - a class body containing those may be
// mis-scanned.
func ExtractClassBody(src []byte, className string) (bodyOpen, bodyClose int, err error) {
	re := regexp.MustCompile(`\b(class|struct)\s+` + regexp.QuoteMeta(className) + `\b`)
	loc := re.FindIndex(src)
	if loc == nil {
		return 0, 0, fmt.Errorf("no 'class %s' or 'struct %s' declaration found", className, className)
	}

	openIdx := -1
	for i := loc[1]; i < len(src); {
		if ni := skipNonCode(src, i); ni != -1 {
			i = ni
			continue
		}
		switch src[i] {
		case ';':
			return 0, 0, fmt.Errorf("found 'class/struct %s' but no class body (forward declaration only?)", className)
		case '{':
			openIdx = i
		}
		if openIdx != -1 {
			break
		}
		i++
	}
	if openIdx == -1 {
		return 0, 0, fmt.Errorf("could not find opening '{' for class %q", className)
	}

	depth := 0
	for i := openIdx; i < len(src); {
		if ni := skipNonCode(src, i); ni != -1 {
			i = ni
			continue
		}
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return openIdx, i, nil
			}
		}
		i++
	}
	return 0, 0, fmt.Errorf("unbalanced braces while scanning class %q body", className)
}

// skipNonCode returns the index just past a line comment, block comment, or
// string/char literal starting at src[i], or -1 if src[i] doesn't start one
// of those - used by ExtractClassBody so brace-counting never trips over a
// '{'/'}' that only appears inside a comment or literal.
func skipNonCode(src []byte, i int) int {
	if i+1 < len(src) && src[i] == '/' && src[i+1] == '/' {
		for i < len(src) && src[i] != '\n' {
			i++
		}
		return i
	}
	if i+1 < len(src) && src[i] == '/' && src[i+1] == '*' {
		i += 2
		for i+1 < len(src) && !(src[i] == '*' && src[i+1] == '/') {
			i++
		}
		return min(i+2, len(src))
	}
	if src[i] == '"' || src[i] == '\'' {
		quote := src[i]
		i++
		for i < len(src) && src[i] != quote {
			if src[i] == '\\' {
				i++
			}
			i++
		}
		return i + 1
	}
	return -1
}
