package heal

import (
	"fmt"
	"strings"
)

// diffContext is how many unchanged lines of context surround a change,
// matching the conventional `diff -u`/`git diff` default.
const diffContext = 3

// diffOp is one line-level edit operation in an LCS-based edit script.
type diffOp struct {
	kind byte // ' ' (unchanged), '-' (removed), '+' (added)
	text string
}

// computeLineDiff returns the line-level edit script transforming oldLines
// into newLines, via a classic LCS dynamic-programming diff. Computing this
// ourselves - rather than asking an LLM to hand-write a unified diff
// directly - is deliberate: a model can and does get hunk-header line-count
// arithmetic wrong (observed live: a real claude-haiku-4-5 response
// produced a hunk claiming 6 original lines when 7 were actually listed,
// which `git apply` rejected as corrupt). Asking for the corrected full
// file content instead and diffing it ourselves makes the model's job
// "write correct code" (a strength) rather than "count lines correctly in
// a diff format" (not), and guarantees a structurally valid diff either way.
func computeLineDiff(oldLines, newLines []string) []diffOp {
	n, m := len(oldLines), len(newLines)
	lcs := make([][]int, n+1)
	for i := range lcs {
		lcs[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			switch {
			case oldLines[i] == newLines[j]:
				lcs[i][j] = lcs[i+1][j+1] + 1
			case lcs[i+1][j] >= lcs[i][j+1]:
				lcs[i][j] = lcs[i+1][j]
			default:
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}

	var ops []diffOp
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case oldLines[i] == newLines[j]:
			ops = append(ops, diffOp{' ', oldLines[i]})
			i++
			j++
		case lcs[i+1][j] >= lcs[i][j+1]:
			ops = append(ops, diffOp{'-', oldLines[i]})
			i++
		default:
			ops = append(ops, diffOp{'+', newLines[j]})
			j++
		}
	}
	for ; i < n; i++ {
		ops = append(ops, diffOp{'-', oldLines[i]})
	}
	for ; j < m; j++ {
		ops = append(ops, diffOp{'+', newLines[j]})
	}
	return ops
}

// unifiedDiff renders oldContent -> newContent as a standard unified diff
// for path (the same shape `git diff`/`diff -u` produce), with hunk-header
// line numbers computed directly from the edit script rather than trusted
// from an LLM. Returns "" if the two contents are identical. Deliberately
// emits a single hunk per file (rather than splitting into several when
// changes are far apart) - simpler to get exactly right, and more than
// sufficient for the small, focused fixes this feature targets.
func unifiedDiff(path, oldContent, newContent string) string {
	if oldContent == newContent {
		return ""
	}
	oldLines := splitLines(oldContent)
	newLines := splitLines(newContent)
	ops := computeLineDiff(oldLines, newLines)

	first, last := -1, -1
	for i, op := range ops {
		if op.kind != ' ' {
			if first == -1 {
				first = i
			}
			last = i
		}
	}
	if first == -1 {
		return ""
	}

	start := max(0, first-diffContext)
	end := min(len(ops), last+1+diffContext)

	oldStart, newStart := 1, 1
	for i := range start {
		if ops[i].kind != '+' {
			oldStart++
		}
		if ops[i].kind != '-' {
			newStart++
		}
	}

	// A file not ending in a newline needs an explicit "\ No newline at end
	// of file" marker on whichever hunk line represents its final line -
	// without it, `git apply` expects a trailing newline that isn't
	// actually there and rejects the patch as a context mismatch (caught by
	// a live test that actually ran `git apply`, not just eyeballing the
	// diff text). lastOldIdx/lastNewIdx locate the op that renders each
	// side's true final line, wherever the LCS alignment placed it.
	oldEndsInNewline := strings.HasSuffix(oldContent, "\n")
	newEndsInNewline := strings.HasSuffix(newContent, "\n")
	lastOldIdx, lastNewIdx := -1, -1
	for i, op := range ops {
		if op.kind != '+' {
			lastOldIdx = i
		}
		if op.kind != '-' {
			lastNewIdx = i
		}
	}

	var hunkLines []string
	oldCount, newCount := 0, 0
	for i := start; i < end; i++ {
		var prefix string
		switch ops[i].kind {
		case ' ':
			prefix = " "
			oldCount++
			newCount++
		case '-':
			prefix = "-"
			oldCount++
		case '+':
			prefix = "+"
			newCount++
		}
		hunkLines = append(hunkLines, prefix+ops[i].text)

		noNewline := (i == lastOldIdx && !oldEndsInNewline) || (i == lastNewIdx && !newEndsInNewline)
		if noNewline {
			hunkLines = append(hunkLines, `\ No newline at end of file`)
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "--- a/%s\n+++ b/%s\n@@ -%d,%d +%d,%d @@\n", path, path, oldStart, oldCount, newStart, newCount)
	for _, l := range hunkLines {
		b.WriteString(l)
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// splitLines splits s into lines, treating a single trailing newline as not
// introducing an extra trailing empty line - so "a\nb\n" and "a\nb" both
// split into ["a", "b"], matching how most editors/tools treat "a file
// ending in a newline" as having no extra blank final line.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.Split(s, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}
