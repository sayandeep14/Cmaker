// Package heal implements the "suggest, don't touch" v1 of `cmaker heal`
// (§24): given a failing build/run log (see internal/logs), it extracts the
// file:line references the compiler already reported, reads just those
// files (not an unbounded whole-codebase dump - keeps the request scoped,
// cheaper, and more likely to produce a focused fix), and asks an LLM for a
// unified-diff fix. Nothing is written to disk here - that's deliberately
// deferred to a future --apply (v2), only after this suggest-only path is
// solid. Pure logic, no CLI concerns - the wrapping lives in cmd/heal.go.
package heal

import "regexp"

// fileLineRe matches clang/gcc-style diagnostic locations, e.g.
// "src/main.cpp:12:5: error: ...". The column number is optional (some
// diagnostics only give file:line).
var fileLineRe = regexp.MustCompile(`([\w./\\-]+\.(?:cpp|cxx|cc|c|h|hpp)):(\d+)(?::\d+)?:\s*(?:error|Error)`)

// ExtractReferencedFiles scans log for compiler-style "file:line: error"
// references and returns the unique file paths mentioned, in first-seen
// order (roughly root-cause-first, since compilers report the deepest
// relevant error first), capped at max.
func ExtractReferencedFiles(log string, max int) []string {
	matches := fileLineRe.FindAllStringSubmatch(log, -1)

	seen := make(map[string]bool, len(matches))
	var files []string
	for _, m := range matches {
		file := m[1]
		if seen[file] {
			continue
		}
		seen[file] = true
		files = append(files, file)
		if len(files) >= max {
			break
		}
	}
	return files
}
