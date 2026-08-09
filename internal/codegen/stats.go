package codegen

import "strings"

// CountGeneratedBlocksInContent counts markerBegin/markerEnd pairs (and
// the lines strictly between each pair) in one file's content - a real,
// exact count of `generate accessors`' own output, used by 'cmaker status
// --detailed' (see cmd/status.go) as one (honest, narrow) signal for "how
// much of this codebase is cmaker-generated." Not a claim about every
// AI-assisted command: codegen/fix/migrate/improve/document --apply all
// write plain code with no distinguishing marker left behind, so this
// only ever counts what's actually greppable today.
//
// Doesn't walk the filesystem itself (unlike this package's other
// exported functions) so it has no need to depend on internal/explain's
// file walker - internal/explain already depends on this package (for its
// own class-body scanner), so the reverse import would cycle; the caller
// (cmd/status.go) already has internal/explain in scope for listing files
// and calls this once per file itself.
func CountGeneratedBlocksInContent(content string) (blocks, lines int) {
	lineList := strings.Split(content, "\n")
	i := 0
	for i < len(lineList) {
		if !strings.Contains(lineList[i], markerBegin) {
			i++
			continue
		}
		end := -1
		for j := i + 1; j < len(lineList); j++ {
			if strings.Contains(lineList[j], markerEnd) {
				end = j
				break
			}
		}
		if end == -1 {
			i++
			continue
		}
		blocks++
		lines += end - i - 1
		i = end + 1
	}
	return blocks, lines
}
