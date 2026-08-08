package codegen

import (
	"regexp"
)

// FunctionMatch is one function *definition* (not a mere declaration or
// call site) found by FindFunctionDefinitions - bodyOpen/bodyClose are the
// byte offsets of its opening `{` and matching closing `}` (both
// inclusive), the same convention ExtractClassBody uses.
type FunctionMatch struct {
	NameStart           int // byte offset of the first character of funcName itself, e.g. the 'a' in "add("
	BodyOpen, BodyClose int
}

// funcNameRe is built per-call (the name varies), matching a bare
// identifier immediately followed by "(" - a plain call site, a
// declaration, and a definition all start this way, which is exactly why
// this alone isn't enough: FindFunctionDefinitions still has to look past
// the parameter list to tell them apart.
func funcCallRe(name string) *regexp.Regexp {
	return regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\s*\(`)
}

// FindFunctionDefinitions locates every *definition* (not declaration, not
// a call site) of a free function or method named funcName in src, and
// returns each one's body byte-range. There can genuinely be more than one
// in a single file (overloads), which is why this returns a slice rather
// than the single (bodyOpen, bodyClose) ExtractClassBody does for a class.
//
// This is the same class of heuristic scan ExtractClassBody already is,
// not a real C++ parser (see ROADMAP.md §19 for why a full parser was
// deliberately avoided): after finding "funcName(", it brace/paren-balances
// past the parameter list (skipping comments/string literals the same way
// ExtractClassBody does), then skips trailing qualifiers (const, noexcept,
// override, final, and whitespace/comments) - if what's left is a "{", this
// is a genuine definition and its body is brace-balanced out; if it's ";"
// or anything else, this occurrence is a declaration or a call site and is
// skipped. Doesn't understand templates, trailing return types, or macros
// that themselves expand to braces - a definition using those may be
// missed rather than mis-scanned (matching ExtractClassBody's own
// documented posture: heuristic, not exhaustive).
func FindFunctionDefinitions(src []byte, funcName string) []FunctionMatch {
	re := funcCallRe(funcName)
	var matches []FunctionMatch

	searchFrom := 0
	for {
		loc := re.FindIndex(src[searchFrom:])
		if loc == nil {
			break
		}
		nameStart := searchFrom + loc[0]
		nameEnd := searchFrom + loc[1] // just past "funcName(", i.e. the "(" itself is the last matched char
		searchFrom = nameEnd           // resume the next search right after this "(", regardless of whether this occurrence turns out to be a definition

		parenClose := balanceFrom(src, nameEnd-1, '(', ')')
		if parenClose == -1 {
			continue
		}

		bodyStart := skipQualifiers(src, parenClose+1)
		if bodyStart >= len(src) || src[bodyStart] != '{' {
			continue // declaration, call site, or "= default"/"= delete" - not a definition with a real body
		}

		bodyClose := balanceFrom(src, bodyStart, '{', '}')
		if bodyClose == -1 {
			continue
		}
		matches = append(matches, FunctionMatch{NameStart: nameStart, BodyOpen: bodyStart, BodyClose: bodyClose})
	}
	return matches
}

// balanceFrom starts at src[i] (which must be openCh) and scans forward,
// skipping comments/string/char literals via skipNonCode, until the
// matching closeCh is found at depth 0 - returns its index, or -1 if
// unbalanced. Shared by both the "(...)" parameter-list skip and the
// "{...}" body-extraction step, mirroring ExtractClassBody's own brace
// walk exactly (generalized to any open/close pair).
func balanceFrom(src []byte, i int, openCh, closeCh byte) int {
	if i >= len(src) || src[i] != openCh {
		return -1
	}
	depth := 0
	for i < len(src) {
		if ni := skipNonCode(src, i); ni != -1 {
			i = ni
			continue
		}
		switch src[i] {
		case openCh:
			depth++
		case closeCh:
			depth--
			if depth == 0 {
				return i
			}
		}
		i++
	}
	return -1
}

// qualifierWordRe matches one trailing function-signature qualifier word
// (const, noexcept, override, final) that can legally appear between a
// parameter list's closing ")" and a definition's opening "{".
var qualifierWordRe = regexp.MustCompile(`^(const|noexcept|override|final)\b`)

// skipQualifiers advances past whitespace and any of the qualifier words
// above (in any order/repetition - real code only uses a sensible subset,
// but this doesn't need to validate that) starting at src[i], returning the
// index of the first remaining non-whitespace, non-qualifier byte.
func skipQualifiers(src []byte, i int) int {
	for i < len(src) {
		switch {
		case src[i] == ' ' || src[i] == '\t' || src[i] == '\n' || src[i] == '\r':
			i++
		case qualifierWordRe.Match(src[i:]):
			i += len(qualifierWordRe.Find(src[i:]))
		default:
			return i
		}
	}
	return i
}
