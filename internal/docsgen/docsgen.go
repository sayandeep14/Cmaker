// Package docsgen implements Phase C's documentation-building features:
// LLM-authored narrative docs (NarrateFile/NarrateArchitecture, driving
// `cmaker docs --narrative`) and single-symbol Doxygen comment generation
// (GenerateDoxygenComment, driving `cmaker document`). See drift.go for
// the third piece, stale-doc detection - a fully local, deterministic
// heuristic with no LLM involved at all.
//
// Narrative docs are the one place in this codebase where an LLM's raw
// prose response IS the final artifact written to disk, rather than
// structured content Go renders/verifies - the same considered exception
// internal/explain's pure-read features already established (nothing
// here is applied to *source code*; it's documentation describing code,
// where a wrong sentence is corrected by regenerating, not silently
// dangerous the way a bad code diff would be). GenerateDoxygenComment
// stays closer to this codebase's usual "diff/--apply gate" shape instead
// (see cmd/document.go), since it does get spliced into a real source
// file.
package docsgen

import (
	"context"
	"fmt"
	"strings"
)

// Completer is the minimal interface docsgen needs from an LLM backend -
// declared here (mirroring every other AI-backed package in this
// codebase, not imported from them) so this package stays independent.
type Completer interface {
	Complete(ctx context.Context, system, user string) (string, error)
}

const fileNarrativeSystemPrompt = `You are a C/C++ documentation writer. You will be given the path and complete content of one source file. Write a short narrative explanation of it: what it does, why it exists, and how it fits into the rest of the project (as best you can tell from the file alone) - the kind of orientation a new contributor reading this file for the first time would want.

Write it as markdown prose (a short intro paragraph plus, if useful, a few bullet points for key pieces) - not a line-by-line walkthrough, not restating obvious code. Respond with ONLY the markdown content - no code fence around the whole thing, no preamble like "Here's the explanation".`

// NarrateFile asks completer to write a narrative markdown explanation of
// path (content already read by the caller) - one LLM call per file, so
// callers documenting a whole project should budget how many files they
// actually narrate (see cmd/docs.go's --max-files).
func NarrateFile(ctx context.Context, completer Completer, path, content string) (string, error) {
	user := fmt.Sprintf("File: %s\n\n%s", path, content)
	raw, err := completer.Complete(ctx, fileNarrativeSystemPrompt, user)
	if err != nil {
		return "", fmt.Errorf("LLM request failed: %w", err)
	}
	return strings.TrimSpace(stripFence(raw)), nil
}

const architectureSystemPrompt = `You are a C/C++ documentation writer. You will be given a project's cmaker.yaml and a sample of its source files. Write a short architecture overview: what the project does, how it's structured (major components/modules and how they relate), and any notable design decisions visible from the code shown.

Write it as markdown prose with headers where useful - grounded in what's actually shown, not generic boilerplate. Respond with ONLY the markdown content - no code fence around the whole thing, no preamble.`

// NarrateArchitecture asks completer for a whole-project architecture
// overview, given prompt (see BuildArchitecturePrompt).
func NarrateArchitecture(ctx context.Context, completer Completer, prompt string) (string, error) {
	raw, err := completer.Complete(ctx, architectureSystemPrompt, prompt)
	if err != nil {
		return "", fmt.Errorf("LLM request failed: %w", err)
	}
	return strings.TrimSpace(stripFence(raw)), nil
}

const doxygenCommentSystemPrompt = `You are a C/C++ documentation assistant. You will be given a single function or class/struct definition. Write a Doxygen-style comment block for it: @brief, @param for each parameter (if any), @return if it returns non-void, @throws if it clearly can throw. Base it strictly on the actual signature/behavior shown - never invent behavior the code doesn't have.

Output ONLY the comment block itself, in this exact form (starting with /** on its own line, ending with */ on its own line, each line in between starting with " * "):

/**
 * @brief ...
 * @param name ...
 * @return ...
 */

Nothing else - no prose, no markdown fences, no the code itself. If the code already has an adequate Doxygen comment immediately above it (shown as part of the given text), output exactly: ALREADY_DOCUMENTED`

// GenerateDoxygenComment asks completer to write a Doxygen comment block
// for code (a single function/class/struct definition, e.g. from
// internal/explain.FindFunction/FindClass) - "" (with no error) if the
// model reports it's already adequately documented. The caller is
// responsible for splicing the returned block above code at the correct
// indentation (see cmd/document.go) - this function only asks for and
// returns the comment text itself.
func GenerateDoxygenComment(ctx context.Context, completer Completer, kind, name, code string) (string, error) {
	user := fmt.Sprintf("%s %q:\n\n%s", kind, name, code)
	raw, err := completer.Complete(ctx, doxygenCommentSystemPrompt, user)
	if err != nil {
		return "", fmt.Errorf("LLM request failed: %w", err)
	}
	raw = strings.TrimSpace(stripFence(raw))
	if raw == "ALREADY_DOCUMENTED" {
		return "", nil
	}
	if !strings.HasPrefix(raw, "/**") {
		return "", fmt.Errorf("model response wasn't a Doxygen comment block:\n%s", raw)
	}
	return raw, nil
}

// stripFence removes a single leading and/or trailing markdown code-fence
// line, if present - the same defensive tolerance every other package in
// this codebase applies, kept as its own small local copy rather than
// shared cross-package plumbing for a five-line helper.
func stripFence(s string) string {
	s = strings.TrimSpace(s)
	lines := strings.Split(s, "\n")
	if len(lines) > 0 && strings.HasPrefix(strings.TrimSpace(lines[0]), "```") {
		lines = lines[1:]
	}
	if len(lines) > 0 && strings.HasPrefix(strings.TrimSpace(lines[len(lines)-1]), "```") {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
}
