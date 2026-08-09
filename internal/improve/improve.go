// Package improve implements `cmaker improve function=<name>|class=<name>
// --intent="..."` - always scoped to one named function or class, never a
// whole project (that's `cmaker suggest`, or the future `cmaker codegen`).
// Given an intent ("improve time complexity by using a more efficient data
// structure"), it asks an LLM for a proposed change and returns it - the
// caller (cmd/improve.go) is responsible for locating the symbol
// (internal/explain.FindFunction/FindClass already does this), computing
// the diff (internal/heal.UnifiedDiff/ParseFileBlocks, reused rather than
// reinvented), and applying it.
//
// Two modes, both real code in this package:
//   - Strict (Suggest): the reply is spliced verbatim into the exact byte
//     range the symbol occupies - nothing else is ever touched. The
//     original, narrower behavior; still the default with --strict.
//   - Non-strict (SuggestFile, the default): the model sees the *whole*
//     file the symbol lives in and may change anywhere within it (add an
//     #include, add a private helper, ...) and/or propose brand-new files
//     (e.g. a new helper header) - but never an existing file other than
//     the one it was shown, since it never saw that file's content.
//
// Same "ask for the complete corrected content, diff it in Go" principle
// internal/heal and internal/improvise both already established - a model
// asked to author code directly is playing to a strength; a model asked to
// also get diff-hunk arithmetic right is not (see internal/heal's own doc
// for the live-observed failure that taught this codebase that lesson).
package improve

import (
	"context"
	"fmt"
	"strings"

	"cmaker/internal/heal"
)

// Completer is the minimal interface improve needs from an LLM backend -
// declared here (mirroring internal/heal.Completer/internal/explain.
// Completer, not imported from them) so this package stays independent;
// internal/llm's Anthropic client satisfies it by duck typing.
type Completer interface {
	Complete(ctx context.Context, system, user string) (string, error)
}

const strictSystemPrompt = `You are a C/C++ code improvement assistant. You will be given a single function or class/struct definition and an intent describing how it should be improved.

Your reply will be spliced verbatim into the exact byte range the original function/class currently occupies in its file - nothing outside that range is touched. Because of this:
- Do NOT include anything outside the function/class itself: no #include lines, no other declarations, no surrounding code.
- If the improvement genuinely requires a new #include or an external declaration, do not add it directly - instead add a single-line comment INSIDE the replacement noting exactly what's needed (e.g. "// NOTE: requires #include <unordered_set> near the top of this file"), so the change is visible in the diff without being silently placed in the wrong location.

Output the COMPLETE replacement for it (the entire function or class, from its first line to its last) that satisfies the intent, in exactly this format:

--- replacement ---
<complete replacement code>

Output nothing else: no prose, no explanation, no markdown code fences, nothing outside that one block - and never more than one such block. If you cannot find a way to satisfy the intent, or the code already satisfies it, output exactly: NO_CHANGE`

// Suggest is improve's strict mode: asks completer to improve original (a
// single function or class body, exactly as extracted by
// internal/explain.FindFunction/FindClass) per intent, and returns the
// proposed replacement - "" (with no error) if the model reports no
// change is needed or possible. The reply is meant to be spliced verbatim
// into original's exact byte range - see this package's own doc.
func Suggest(ctx context.Context, completer Completer, kind, name, original, intent string) (string, error) {
	if strings.TrimSpace(intent) == "" {
		return "", fmt.Errorf("--intent is required, e.g. --intent=\"improve time complexity by using a more efficient data structure\"")
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Intent: %s\n\n", intent)
	fmt.Fprintf(&b, "Current %s %q:\n\n%s\n", kind, name, original)

	raw, err := completer.Complete(ctx, strictSystemPrompt, b.String())
	if err != nil {
		return "", fmt.Errorf("LLM request failed: %w", err)
	}

	raw = strings.TrimSpace(raw)
	if raw == "NO_CHANGE" {
		return "", nil
	}

	replacement, err := parseReplacement(raw)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(replacement) == strings.TrimSpace(original) {
		return "", nil
	}
	return replacement, nil
}

const fileSystemPrompt = `You are a C/C++ code improvement assistant. You will be given the complete content of one source file and an intent describing how a specific function or class/struct within that file should be improved.

Unlike a narrowly-scoped edit, you may change anywhere in this file that's needed to satisfy the intent well - add a #include, add a private helper function, refactor supporting code near the target, and so on - not just the target function/class itself. You may also propose brand-new files (e.g. a new helper header) if that's the cleanest way to satisfy the intent, with a sensible path relative to the project root (e.g. alongside the given file).

Do NOT propose changes to any other *existing* file besides the one given to you - you have not seen its content, so any such change would be unfounded and will be discarded.

For each file you are changing or creating, output its ENTIRE content (not a diff, not a snippet - the complete file from its first line to its last), in exactly this format, one block per file:

--- file: <path> ---
<complete file content>

Only include a block for a file you're actually changing or creating - omit files that don't need changes. Output nothing else: no prose, no explanation, no markdown code fences, nothing outside the "--- file: <path> ---" blocks.

If you cannot find a way to satisfy the intent, or the code already satisfies it, output exactly: NO_CHANGE`

// SuggestFile is improve's non-strict (default) mode: asks completer to
// improve name (a function/class defined somewhere in targetFile) per
// intent, given targetFile's *entire* content (not just the symbol's own
// snippet) - so the model can add includes/helpers anywhere in that file,
// and/or propose brand-new files. Returns the raw parsed file blocks
// (path -> proposed full content), reusing internal/heal.ParseFileBlocks
// (the exact same "--- file: path ---" convention and defensive parsing
// heal/improvise already rely on) rather than a second implementation of
// it - nil (with no error) if the model reports no change.
//
// The caller is responsible for enforcing "never an existing file other
// than targetFile" - this function only builds the prompt and parses the
// response; it doesn't have access to the rest of the project's disk
// state to check that itself.
func SuggestFile(ctx context.Context, completer Completer, kind, name, targetFile, fileContent, intent string) (map[string]string, error) {
	if strings.TrimSpace(intent) == "" {
		return nil, fmt.Errorf("--intent is required, e.g. --intent=\"improve time complexity by using a more efficient data structure\"")
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Improve the %s %q. Intent: %s\n\n", kind, name, intent)
	fmt.Fprintf(&b, "--- file: %s ---\n%s\n", targetFile, fileContent)

	raw, err := completer.Complete(ctx, fileSystemPrompt, b.String())
	if err != nil {
		return nil, fmt.Errorf("LLM request failed: %w", err)
	}

	raw = strings.TrimSpace(raw)
	if raw == "NO_CHANGE" {
		return nil, nil
	}

	blocks := heal.ParseFileBlocks(raw)
	if len(blocks) == 0 {
		return nil, fmt.Errorf("model response didn't contain any recognizable \"--- file: <path> ---\" blocks:\n%s", raw)
	}
	return blocks, nil
}

// replacementMarker is this package's own delimiter separating the
// model's response from the replacement code itself.
const replacementMarker = "--- replacement ---"

// parseReplacement extracts the code following the "--- replacement ---"
// marker, tolerating the same real-world model quirks internal/heal's own
// ParseFileBlocks already documents and defends against (a wrapping
// markdown code fence; a stray trailing separator line echoing the
// marker convention), plus one observed live: despite the system prompt
// saying "never more than one such block," a real claude-haiku-4-5
// response tried to communicate an additional #include requirement by
// emitting a second, malformed "--- replacement ---"-shaped block after
// the real one - truncating at the first repeat of the marker discards
// that trailing garbage rather than splicing it into the file too.
func parseReplacement(raw string) (string, error) {
	_, content, ok := strings.Cut(raw, replacementMarker)
	if !ok {
		return "", fmt.Errorf("model response didn't contain a \"%s\" block:\n%s", replacementMarker, raw)
	}
	if again, _, found := strings.Cut(content, replacementMarker); found {
		content = again
	}
	content = strings.TrimPrefix(content, "\n")
	content = strings.TrimRight(content, "\n")
	content = stripFence(content)
	return content, nil
}

// stripFence removes a single leading and/or trailing markdown code-fence
// line, if present - the same defensive tolerance internal/heal's own
// stripFence applies (kept as a small local copy rather than exported
// cross-package plumbing for a five-line helper).
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
