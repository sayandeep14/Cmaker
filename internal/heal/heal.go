package heal

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// maxReferencedFiles bounds how many distinct source files a heal request
// reads - keeps the request scoped to what actually failed, not the whole
// project.
const maxReferencedFiles = 5

// maxLogChars bounds how much of a (possibly very verbose) failing log gets
// sent - the tail is what matters most for a build failure, since that's
// where the actual fatal error is.
const maxLogChars = 6000

// systemPrompt deliberately asks for each changed file's *entire corrected
// content*, not a hand-written diff. An earlier version asked for a unified
// diff directly; a live end-to-end test caught a real claude-haiku-4-5
// response with a hunk header whose line count didn't match its own hunk
// body (git apply rejected it as corrupt) - models get diff-format
// arithmetic wrong. Asking for full file content instead makes the model's
// job "write correct code" and lets cmaker compute a guaranteed-consistent
// diff itself (see diff.go's unifiedDiff), the same "don't trust the LLM to
// emit the final artifact directly" principle §19's accessor generator
// already established.
const systemPrompt = `You are a C/C++ build-failure triage assistant. You will be given a failing build/run log and the full contents of the source file(s) the compiler's error output referenced.

Identify the root cause of the failure and fix it. For each file that needs a change, output its ENTIRE corrected file content (not a diff, not a snippet - the complete file from its first line to its last), in exactly this format, one block per changed file:

--- file: <path> ---
<complete corrected file content>

Only include a block for a file you are actually changing - omit files that don't need changes. Output nothing else: no prose, no explanation, no markdown code fences, nothing outside the "--- file: <path> ---" blocks.

If you cannot determine a fix from the given context, output exactly: NO_FIX_FOUND`

// Completer is the minimal interface heal needs from an LLM backend - a
// single-turn system+user prompt in, text out. Declared here (mirroring
// internal/codegen.Completer, not imported from it, so this package stays
// independent) rather than depending on internal/codegen; internal/llm's
// Anthropic client satisfies both by duck typing. Kept separate so tests
// can supply a fake with no network access.
type Completer interface {
	Complete(ctx context.Context, system, user string) (string, error)
}

// Suggestion is the result of asking an LLM to diagnose a failure log.
type Suggestion struct {
	Diff            string   // empty if the model reported no fix, or every proposed change was a no-op
	ReferencedFiles []string // files the log pointed at and were actually read
}

// Suggest reads logPath and every file it references in compiler-style
// "file:line: error" output (bounded to maxReferencedFiles, resolved
// relative to root), asks completer for each changed file's corrected full
// content, and returns a unified diff computed deterministically from the
// before/after content (see diff.go) - never a diff trusted verbatim from
// the model. Nothing is written to disk - the caller presents the diff.
func Suggest(ctx context.Context, completer Completer, root, logPath string) (Suggestion, error) {
	logData, err := os.ReadFile(logPath)
	if err != nil {
		return Suggestion{}, fmt.Errorf("failed to read %s: %w", logPath, err)
	}
	logText := string(logData)
	if len(logText) > maxLogChars {
		logText = "...(truncated)...\n" + logText[len(logText)-maxLogChars:]
	}

	referenced := ExtractReferencedFiles(logText, maxReferencedFiles)
	if len(referenced) == 0 {
		return Suggestion{}, fmt.Errorf("couldn't find any file:line error references in %s - nothing to scope the request to", logPath)
	}

	var b strings.Builder
	b.WriteString("Build/run log:\n")
	b.WriteString(logText)
	b.WriteString("\n\n")

	// absRoot lets an absolute compiler-reported path be converted back to
	// something relative to the project - used both for the diff's a/b
	// headers (git apply rejects an absolute path there outright: "invalid
	// path") and so the prompt/response don't deal in noisy tmpdir-style
	// absolute paths at all. Falls back to using paths exactly as the log
	// reported them if root's own absolute form can't be resolved.
	absRoot, absRootErr := filepath.Abs(root)

	var readFiles []string
	original := map[string]string{}
	for _, f := range referenced {
		// Compilers often report an absolute path (e.g. when CMake invokes
		// them with an absolute source dir, common on macOS/Xcode-style
		// builds) - filepath.Join(root, f) would silently mangle that into
		// a bogus relative path (Join(".", "/tmp/x/y") = "tmp/x/y", not
		// "/tmp/x/y"), so an already-absolute reference is used as-is to
		// actually read the file.
		readPath := f
		if !filepath.IsAbs(f) {
			readPath = filepath.Join(root, f)
		}
		data, err := os.ReadFile(readPath)
		if err != nil {
			continue // referenced file not found/readable - skip it, don't fail the whole request over one bad path
		}
		content := string(data)

		displayPath := f
		if filepath.IsAbs(f) && absRootErr == nil {
			if rel, err := filepath.Rel(absRoot, f); err == nil && !strings.HasPrefix(rel, "..") {
				displayPath = rel
			}
		}

		fmt.Fprintf(&b, "--- file: %s ---\n%s\n\n", displayPath, content)
		readFiles = append(readFiles, displayPath)
		original[displayPath] = content
	}
	if len(readFiles) == 0 {
		return Suggestion{}, fmt.Errorf("found file references in the log (%s) but none of them exist under %s", strings.Join(referenced, ", "), root)
	}

	raw, err := completer.Complete(ctx, systemPrompt, b.String())
	if err != nil {
		return Suggestion{}, fmt.Errorf("LLM request failed: %w", err)
	}

	raw = strings.TrimSpace(raw)
	if raw == "NO_FIX_FOUND" {
		return Suggestion{ReferencedFiles: readFiles}, nil
	}

	proposed := parseFileBlocks(raw)
	if len(proposed) == 0 {
		return Suggestion{}, fmt.Errorf("model response didn't contain any recognizable \"--- file: <path> ---\" blocks:\n%s", raw)
	}

	var diffBuilder strings.Builder
	for _, f := range readFiles { // stable order: the same order files were read in
		newContent, ok := proposed[f]
		if !ok {
			continue // model didn't propose a change to this file
		}
		d := unifiedDiff(f, original[f], newContent)
		if d == "" {
			continue // proposed content was identical to the original - not an actual change
		}
		if diffBuilder.Len() > 0 {
			diffBuilder.WriteString("\n")
		}
		diffBuilder.WriteString(d)
	}

	return Suggestion{Diff: diffBuilder.String(), ReferencedFiles: readFiles}, nil
}

// fileBlockRe matches this package's own "--- file: <path> ---" delimiter,
// used both to build the request (see Suggest above) and parse the
// response (see parseFileBlocks) - one consistent format on both sides of
// the conversation.
var fileBlockRe = regexp.MustCompile(`(?m)^--- file: (.+) ---$\n`)

// parseFileBlocks splits raw LLM output into path -> proposed full file
// content, per the systemPrompt's requested format. Tolerates two real,
// observed-live model quirks despite the prompt explicitly saying not to:
// wrapping a block in a markdown code fence, and (only possible on the
// *last* block, since it has no following "--- file: ---" delimiter to
// bound it) echoing a stray trailing separator line that isn't part of the
// actual file - a live end-to-end run against claude-haiku-4-5 produced a
// corrected file whose very last line was a bare "---", which silently
// became part of the "fixed" file and broke compilation even though the
// resulting diff applied perfectly cleanly. Both are stripped if present.
func parseFileBlocks(raw string) map[string]string {
	locs := fileBlockRe.FindAllStringSubmatchIndex(raw, -1)
	result := make(map[string]string, len(locs))
	for i, loc := range locs {
		path := raw[loc[2]:loc[3]]
		contentStart := loc[1]
		contentEnd := len(raw)
		isLast := i+1 >= len(locs)
		if !isLast {
			contentEnd = locs[i+1][0]
		}
		content := strings.TrimRight(raw[contentStart:contentEnd], "\n")
		content = stripFence(content)
		if isLast {
			content = stripStrayTrailingSeparator(content)
		}
		result[path] = content
	}
	return result
}

// strayFileBlockMarkerRe matches a line that looks like this package's own
// "--- file: <path> ---" delimiter (see fileBlockRe below) but wasn't
// followed by a newline in the raw response - which is exactly why
// fileBlockRe itself (anchored on a trailing "\n") never matched it as a
// genuine new block header, leaving it stuck as trailing content of the
// last real block instead. Observed live: a real claude-haiku-4-5 response
// closed its output with "--- file: end ---", seemingly imitating its own
// required delimiter syntax as an ad hoc "no more files" marker despite the
// prompt never asking for one.
var strayFileBlockMarkerRe = regexp.MustCompile(`^--- file: .+ ---$`)

// stripStrayTrailingSeparator removes a final line that's either just
// dashes (e.g. "---", "----------") or a stray "--- file: ... ---"-shaped
// marker (see strayFileBlockMarkerRe) - neither is ever valid content in a
// C/C++ file, so both are safe to treat as stray artifacts rather than an
// intended change.
func stripStrayTrailingSeparator(s string) string {
	lines := strings.Split(s, "\n")
	if len(lines) == 0 {
		return s
	}
	trimmed := strings.TrimSpace(lines[len(lines)-1])
	isDashesOnly := len(trimmed) >= 3 && strings.Trim(trimmed, "-") == ""
	if isDashesOnly || strayFileBlockMarkerRe.MatchString(trimmed) {
		lines = lines[:len(lines)-1]
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n")
}

// stripFence removes a single leading and/or trailing markdown code-fence
// line (e.g. "```cpp" / "```") from s, if present.
func stripFence(s string) string {
	lines := strings.Split(s, "\n")
	if len(lines) > 0 && strings.HasPrefix(strings.TrimSpace(lines[0]), "```") {
		lines = lines[1:]
	}
	if len(lines) > 0 && strings.HasPrefix(strings.TrimSpace(lines[len(lines)-1]), "```") {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
}
