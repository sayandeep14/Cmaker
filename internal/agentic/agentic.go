// Package agentic implements `cmaker codegen --intent="..."`/`--intent-from=`
// - Phase B's "agentic layer" core loop from the project backlog: a
// free-form, whole-project change request, unlike `cmaker improve` (always
// scoped to one named function/class) or `cmaker suggest` (read-only,
// never writes code). It decides which files are relevant itself
// (SuggestRelevantFiles), optionally asks clarifying questions first
// (AskClarifyingQuestions, --deep only), and proposes the actual change
// (ProposeChanges) - all three ask for structured output (a JSON file list,
// JSON questions, or full corrected file content) that the caller
// (cmd/codegen.go) renders/diffs/applies deterministically, never trusting
// raw LLM output as the final artifact, the same principle every other
// AI-backed package here already follows.
//
// Named agentic, not codegen, because internal/codegen already exists -
// the accessor generator behind `cmaker generate` (a different, unrelated
// feature that predates this package).
//
// The build-fix retry loop, checkpoint commits, and rollback-on-failure
// live in cmd/codegen.go, not here - this package only ever proposes;
// cmd/ owns git, the filesystem, and the rebuild.
package agentic

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"cmaker/internal/heal"
)

// Completer is the minimal interface this package needs from an LLM
// backend - declared here (mirroring every other AI-backed package in
// this codebase, not imported from them) so this package stays
// independent.
type Completer interface {
	Complete(ctx context.Context, system, user string) (string, error)
}

// maxRelevantFiles bounds how many files SuggestRelevantFiles can pick -
// a bit more generous than internal/heal's own maxRelatedFiles (5), since
// a free-form project-wide change is often more broadly scoped than
// diagnosing one build failure.
const maxRelevantFiles = 8

const relevantFilesSystemPrompt = `You are a C/C++ coding assistant. You will be given a change request (an "intent") and a list of every source file in the project.

Pick which of these files would need to be read and/or changed to satisfy the intent - things like the file(s) implementing the relevant behavior, headers declaring types/functions involved, and closely related files (e.g. a class's .h alongside its .cpp).

Respond with ONLY a JSON array of file paths, chosen EXACTLY as given in the list (no paths you weren't given, no invented paths, no markdown code fence, no prose). Pick only files genuinely relevant to the intent - if truly none are, respond with an empty array: []`

// SuggestRelevantFiles asks completer which of candidates (every source
// file in the project) are relevant to intent - grounded to that real
// file list, never an invented path, exactly like internal/heal's own
// SuggestRelatedFiles (a separate, independent copy of this pattern, not
// a shared import, matching this codebase's established
// per-package-Completer convention). Capped at maxRelevantFiles.
func SuggestRelevantFiles(ctx context.Context, completer Completer, intent string, candidates []string) ([]string, error) {
	if len(candidates) == 0 {
		return nil, nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Intent: %s\n\n", intent)
	b.WriteString("Files in this project:\n")
	for _, c := range candidates {
		b.WriteString("- " + c + "\n")
	}

	raw, err := completer.Complete(ctx, relevantFilesSystemPrompt, b.String())
	if err != nil {
		return nil, fmt.Errorf("LLM request failed: %w", err)
	}

	picked, err := parseStringArray(raw)
	if err != nil {
		return nil, err
	}

	candidateSet := make(map[string]bool, len(candidates))
	for _, c := range candidates {
		candidateSet[c] = true
	}
	valid := make([]string, 0, len(picked))
	for _, p := range picked {
		if candidateSet[p] {
			valid = append(valid, p)
			if len(valid) >= maxRelevantFiles {
				break
			}
		}
	}
	return valid, nil
}

const clarifyingQuestionsSystemPrompt = `You are a C/C++ coding assistant about to implement a change request (an "intent") for a project. You will be given the intent and a list of the project's source files.

If the intent is clear and specific enough to implement directly, respond with exactly: NO_QUESTIONS

If it's ambiguous or underspecified in a way that would genuinely change what you'd implement (not just stylistic nitpicks), respond with ONLY a JSON array of up to 4 short, specific clarifying questions - no markdown code fence, no prose outside the array.`

// AskClarifyingQuestions asks completer whether intent needs clarification
// before implementation - only called in --deep mode (see cmd/codegen.go).
// Returns nil (no error) if the model reports the intent is clear enough
// to implement directly.
func AskClarifyingQuestions(ctx context.Context, completer Completer, intent string, candidates []string) ([]string, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "Intent: %s\n\n", intent)
	b.WriteString("Files in this project:\n")
	for _, c := range candidates {
		b.WriteString("- " + c + "\n")
	}

	raw, err := completer.Complete(ctx, clarifyingQuestionsSystemPrompt, b.String())
	if err != nil {
		return nil, fmt.Errorf("LLM request failed: %w", err)
	}

	raw = strings.TrimSpace(raw)
	if raw == "NO_QUESTIONS" {
		return nil, nil
	}
	return parseStringArray(raw)
}

const breakdownSystemPrompt = `You are a C/C++ engineering-planning assistant. You will be given a single task description and the project's list of source files.

If this task is small and concrete enough to implement in one focused change, respond with exactly: NO_BREAKDOWN

If it's too large or broad for one shot (e.g. it bundles several distinct changes, or touches many unrelated areas), respond with ONLY a JSON array of 2-5 concrete sub-task strings that together accomplish it, each independently small enough to implement in one focused change - no markdown code fence, no prose outside the array.`

// AssessBreakdown asks completer whether task is small enough to
// implement directly, or should be split into sub-tasks first - used by
// `cmaker codegen --intent-from` (see cmd/codegen.go), which inserts any
// returned sub-tasks as checklist items in place and takes up the first
// one. Returns nil (no error) if the model reports the task doesn't need
// breaking down.
func AssessBreakdown(ctx context.Context, completer Completer, task string, candidates []string) ([]string, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "Task: %s\n\n", task)
	b.WriteString("Files in this project:\n")
	for _, c := range candidates {
		b.WriteString("- " + c + "\n")
	}

	raw, err := completer.Complete(ctx, breakdownSystemPrompt, b.String())
	if err != nil {
		return nil, fmt.Errorf("LLM request failed: %w", err)
	}

	raw = strings.TrimSpace(raw)
	if raw == "NO_BREAKDOWN" {
		return nil, nil
	}
	return parseStringArray(raw)
}

const changeSystemPrompt = `You are a C/C++ coding assistant implementing a change request (an "intent"). You will be given the intent and the complete content of every file relevant to it.

You may modify any of the given files, and/or create brand-new files (e.g. a new helper header/source pair) with a sensible path relative to the project root, if that's the cleanest way to satisfy the intent. Do NOT propose changes to any other *existing* file besides the ones given to you - you have not seen its content, so any such change would be unfounded and will be discarded.

For each file you are changing or creating, output its ENTIRE content (not a diff, not a snippet - the complete file from its first line to its last), in exactly this format, one block per file:

--- file: <path> ---
<complete file content>

Only include a block for a file you're actually changing or creating - omit files that don't need changes. Output nothing else: no prose, no explanation, no markdown code fences, nothing outside the "--- file: <path> ---" blocks.

If you cannot find a way to satisfy the intent, or the project already satisfies it, output exactly: NO_CHANGE`

// ProposeChanges asks completer to implement intent given files (path ->
// current content, already selected by the caller - e.g. via
// SuggestRelevantFiles). Returns the proposed path -> new/created content
// (heal.ParseFileBlocks - the same "--- file: path ---" parsing every
// other full-file-content-based package here already shares), nil (no
// error) if the model reports no change is needed or possible. The
// caller is responsible for diffing against files' original content,
// enforcing "never write to an existing file that wasn't in files", and
// applying.
func ProposeChanges(ctx context.Context, completer Completer, intent string, files map[string]string) (map[string]string, error) {
	if strings.TrimSpace(intent) == "" {
		return nil, fmt.Errorf("--intent is required, e.g. --intent=\"add a --verbose flag to the CLI\"")
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no files to propose changes against")
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Intent: %s\n\n", intent)
	for path, content := range files {
		fmt.Fprintf(&b, "--- file: %s ---\n%s\n\n", path, content)
	}

	raw, err := completer.Complete(ctx, changeSystemPrompt, b.String())
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

// parseStringArray decodes a JSON string array response - json.Decoder
// (not json.Unmarshal), for the same reason every other structured-output
// parser in this codebase uses it: a live model response has been
// observed wrapping JSON in a code fence and/or appending trailing prose
// after it - Decoder parses just the first JSON value and tolerates
// anything after. Also normalizes "smart"/curly quotes to straight ones
// first - observed live from claude-opus-5 specifically (its prose-
// formatting habits leaking into what was supposed to be strict JSON,
// e.g. responding with [“src/main.cpp”] instead of ["src/main.cpp"]),
// which plain json.Decoder rejects outright since U+201C/U+201D aren't
// valid JSON string delimiters.
func parseStringArray(raw string) ([]string, error) {
	raw = normalizeSmartQuotes(stripLeadingCodeFence(strings.TrimSpace(raw)))
	var items []string
	dec := json.NewDecoder(strings.NewReader(raw))
	if err := dec.Decode(&items); err != nil {
		return nil, fmt.Errorf("model response wasn't a valid JSON string array: %w\nresponse was:\n%s", err, raw)
	}
	return items, nil
}

// normalizeSmartQuotes replaces Unicode "smart" double/single quotes with
// their plain ASCII equivalents.
func normalizeSmartQuotes(s string) string {
	replacer := strings.NewReplacer(
		"“", `"`, "”", `"`,
		"‘", "'", "’", "'",
	)
	return replacer.Replace(s)
}

// stripLeadingCodeFence removes a single leading "```" (optionally
// "```json") line, if present.
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
