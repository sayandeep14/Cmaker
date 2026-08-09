package docsgen

import (
	"regexp"
	"strings"
)

// DriftIssue is one detected mismatch between a Doxygen comment's @param
// list and the actual parameter names of the function/method it
// documents immediately above it - a fast, fully local (no LLM) heuristic
// check for 'cmaker doctor --docs', not a C++ parser: it only flags
// clear, low-risk mismatches and skips anything ambiguous (unnamed
// parameters, function pointers, variadic templates) rather than risk
// false positives that would erode trust in the check.
type DriftIssue struct {
	File         string
	Line         int      // 1-based line of the function's signature
	Signature    string   // trimmed first line of the signature, for display
	Missing      []string // @param names that no longer match a real parameter
	Undocumented []string // real parameter names with no matching @param
}

var (
	docCommentStartRe = regexp.MustCompile(`^\s*/\*\*\s*$`)
	docParamRe        = regexp.MustCompile(`@param\s+(?:\[[a-zA-Z,]+\]\s+)?(\w+)`)
	identifierRe      = regexp.MustCompile(`^[A-Za-z_]\w*$`)
)

// CountDocCommentBlocks counts "/**"-started Doxygen comment blocks in
// content - a cheap, deterministic proxy for "how much of this file has
// been documented at all," used by 'cmaker status --detailed' alongside
// FindDriftIssues and (optionally) AssessQuality's LLM-based judgment.
// Deliberately not a full documented-vs-undocumented function ratio: that
// would need a real signature parser this codebase doesn't have (see
// internal/explain's own name-targeted, not exhaustive, scanner) - a
// block count is honest about being a count, not a coverage percentage
// this heuristic can't actually back up.
func CountDocCommentBlocks(content string) int {
	count := 0
	for _, line := range strings.Split(content, "\n") {
		if docCommentStartRe.MatchString(line) {
			count++
		}
	}
	return count
}

// FindDriftIssues scans content (one source file's text) for Doxygen
// "/**"-"*/" blocks immediately followed (after any blank lines) by a
// function/method signature, and reports any @param name that doesn't
// match the signature's actual parameter names - in either direction.
func FindDriftIssues(file, content string) []DriftIssue {
	lines := strings.Split(content, "\n")
	var issues []DriftIssue

	for i := 0; i < len(lines); i++ {
		if !docCommentStartRe.MatchString(lines[i]) {
			continue
		}
		commentEnd := -1
		for j := i + 1; j < len(lines); j++ {
			if strings.Contains(lines[j], "*/") {
				commentEnd = j
				break
			}
		}
		if commentEnd == -1 {
			continue
		}

		docParams := paramNamesFromComment(strings.Join(lines[i:commentEnd+1], "\n"))
		if len(docParams) > 0 {
			if sigLine, sigText, ok := signatureAfter(lines, commentEnd+1); ok {
				if realParams, ok := parseParamNames(sigText); ok {
					missing := diffSet(docParams, realParams)
					undocumented := diffSet(realParams, docParams)
					if len(missing) > 0 || len(undocumented) > 0 {
						issues = append(issues, DriftIssue{
							File:         file,
							Line:         sigLine + 1,
							Signature:    strings.TrimSpace(lines[sigLine]),
							Missing:      missing,
							Undocumented: undocumented,
						})
					}
				}
			}
		}
		i = commentEnd
	}
	return issues
}

func paramNamesFromComment(comment string) []string {
	matches := docParamRe.FindAllStringSubmatch(comment, -1)
	names := make([]string, 0, len(matches))
	for _, m := range matches {
		names = append(names, m[1])
	}
	return names
}

// signatureAfter finds the next non-blank line at or after idx (a doc
// comment must sit directly above its target, not floating above
// unrelated code) and gathers lines until one ends the declaration's
// header ('{' or ';'), returning the 0-based line index the signature
// starts on and the joined header text.
func signatureAfter(lines []string, idx int) (line int, text string, ok bool) {
	for idx < len(lines) && strings.TrimSpace(lines[idx]) == "" {
		idx++
	}
	if idx >= len(lines) {
		return 0, "", false
	}
	start := idx
	var b strings.Builder
	// A real signature spanning more than 10 lines isn't realistic -
	// bail rather than scan arbitrarily far into the file.
	for idx < len(lines) && idx < start+10 {
		b.WriteString(lines[idx])
		b.WriteString("\n")
		if strings.ContainsAny(lines[idx], "{;") {
			return start, b.String(), true
		}
		idx++
	}
	return 0, "", false
}

// parseParamNames extracts parameter names from a function header's
// parameter list (the text between its first '(' and matching ')'),
// tracking paren depth so a default argument like "int x = foo(1, 2)"
// isn't mistaken for two parameters. Returns ok=false (skip, don't flag)
// for shapes this heuristic doesn't confidently handle: no matching
// close paren, or any parameter it can't extract a trailing identifier
// from (e.g. a function-pointer parameter, or a lone type keyword for an
// unnamed parameter).
func parseParamNames(header string) ([]string, bool) {
	open := strings.Index(header, "(")
	if open == -1 {
		return nil, false
	}
	depth := 0
	closeIdx := -1
	for i := open; i < len(header); i++ {
		switch header[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				closeIdx = i
			}
		}
		if closeIdx != -1 {
			break
		}
	}
	if closeIdx == -1 {
		return nil, false
	}

	inner := strings.TrimSpace(header[open+1 : closeIdx])
	if inner == "" || inner == "void" {
		return []string{}, true
	}

	var names []string
	for _, part := range splitTopLevelCommas(inner) {
		part = strings.TrimSpace(part)
		if part == "" || part == "..." {
			continue
		}
		if eq := strings.Index(part, "="); eq != -1 {
			part = strings.TrimSpace(part[:eq])
		}
		name, ok := trailingIdentifier(part)
		if !ok {
			return nil, false
		}
		names = append(names, name)
	}
	return names, true
}

// splitTopLevelCommas splits s on commas that aren't nested inside
// (), <>, or [] - so a template parameter type like "std::map<int, int>
// counts" doesn't get split in the middle of its template arguments.
func splitTopLevelCommas(s string) []string {
	var parts []string
	depth := 0
	last := 0
	for i, r := range s {
		switch r {
		case '(', '<', '[':
			depth++
		case ')', '>', ']':
			depth--
		case ',':
			if depth == 0 {
				parts = append(parts, s[last:i])
				last = i + 1
			}
		}
	}
	parts = append(parts, s[last:])
	return parts
}

var commonTypeKeywords = map[string]bool{
	"int": true, "float": true, "double": true, "char": true, "bool": true,
	"void": true, "long": true, "short": true, "unsigned": true, "signed": true,
	"auto": true, "size_t": true,
}

// trailingIdentifier extracts a parameter's name as its last whitespace-
// separated token, stripped of trailing array brackets ("int arr[]" ->
// "arr") and leading pointer/reference sigils ("int* x", "int &x" ->
// "x"). Returns ok=false if what's left doesn't look like a plain
// identifier, or is a single bare type keyword with no separate name
// (an unnamed parameter, e.g. "int" alone in a declaration) - treating
// every single-token parameter as "named" would false-positive on the
// common case of unnamed declaration parameters.
func trailingIdentifier(param string) (string, bool) {
	param = strings.TrimSpace(param)
	if idx := strings.Index(param, "["); idx != -1 {
		param = strings.TrimSpace(param[:idx])
	}
	fields := strings.Fields(param)
	if len(fields) == 0 {
		return "", false
	}
	last := strings.TrimLeft(fields[len(fields)-1], "*&")
	if !identifierRe.MatchString(last) {
		return "", false
	}
	if len(fields) == 1 && commonTypeKeywords[last] {
		return "", false
	}
	return last, true
}

func diffSet(a, b []string) []string {
	bSet := make(map[string]bool, len(b))
	for _, x := range b {
		bSet[x] = true
	}
	var diff []string
	for _, x := range a {
		if !bSet[x] {
			diff = append(diff, x)
		}
	}
	return diff
}
