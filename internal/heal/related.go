package heal

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// maxRelatedFiles bounds how many additional files SuggestRelatedFiles can
// pick - the whole point of the escalation ladder's context-expansion step
// is "a few more targeted files," not "the whole project" (that's
// internal/suggest's job).
const maxRelatedFiles = 5

// relatedFilesSystemPrompt asks the model to pick, from a known list of
// project files, which ones (if any) would help it diagnose a failure it
// couldn't resolve from the file(s) already shown - the "which files"
// half of 'cmaker heal's escalation ladder (see cmd/heal.go), grounded to
// an actual project file list rather than letting the model invent a
// plausible-sounding path that doesn't exist.
const relatedFilesSystemPrompt = `You are a C/C++ build-failure triage assistant. You were already given a failing build/run log and some source file(s), but couldn't determine a fix from them alone. You will now be given a list of every other source file in this project.

Pick which of these files (if any) would likely help you diagnose and fix the failure - things like a header declaring a type/function involved in the error, a base class, or a file that defines something the error references.

Respond with ONLY a JSON array of file paths, chosen EXACTLY as given in the list (no paths you weren't given, no invented paths, no markdown code fence, no prose). Pick only the files genuinely likely to help - if none would help, respond with an empty array: []`

// SuggestRelatedFiles asks completer which of candidates (project source
// files not already read) would help diagnose the failure described by
// logText, given the files already reviewed (alreadyRead, path ->
// content). Returns a subset of candidates - never a path outside it, even
// if the model proposes one, since that's not something Suggest could
// actually read - capped at maxRelatedFiles.
func SuggestRelatedFiles(ctx context.Context, completer Completer, logText string, alreadyRead map[string]string, candidates []string) ([]string, error) {
	if len(candidates) == 0 {
		return nil, nil
	}

	var b strings.Builder
	b.WriteString("Build/run log:\n")
	b.WriteString(logText)
	b.WriteString("\n\n")
	for path, content := range alreadyRead {
		fmt.Fprintf(&b, "--- file already reviewed: %s ---\n%s\n\n", path, content)
	}
	b.WriteString("Other files in this project:\n")
	for _, c := range candidates {
		b.WriteString("- " + c + "\n")
	}

	raw, err := completer.Complete(ctx, relatedFilesSystemPrompt, b.String())
	if err != nil {
		return nil, fmt.Errorf("LLM request failed: %w", err)
	}

	picked, err := parseFilePathArray(raw)
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
			if len(valid) >= maxRelatedFiles {
				break
			}
		}
	}
	return valid, nil
}

// parseFilePathArray decodes a JSON string array response - json.Decoder
// (not json.Unmarshal), for the same reason every other structured-output
// parser in this codebase uses it: a live model response has been
// observed wrapping JSON in a code fence and/or appending trailing prose
// after it - Decoder parses just the first JSON value and tolerates
// anything after.
func parseFilePathArray(raw string) ([]string, error) {
	raw = stripLeadingCodeFence(strings.TrimSpace(raw))
	var paths []string
	dec := json.NewDecoder(strings.NewReader(raw))
	if err := dec.Decode(&paths); err != nil {
		return nil, fmt.Errorf("model response wasn't a valid JSON file-path array: %w\nresponse was:\n%s", err, raw)
	}
	return paths, nil
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
