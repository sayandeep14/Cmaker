// Package suggest implements `cmaker suggest` - whole-project improvement
// suggestions, the one command in this family that isn't scoped to a
// single symbol (see internal/improve for that). Returns structured
// suggestions (title + detail), never raw prose - cmd/suggest.go renders
// them, either to the terminal or as a markdown checklist via --export,
// deterministically, rather than trusting the model to format its own
// output correctly.
package suggest

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"cmaker/internal/explain"
)

// Completer is the minimal interface suggest needs from an LLM backend -
// declared here (mirroring every other AI-backed package in this
// codebase, not imported from them) so this package stays independent.
type Completer interface {
	Complete(ctx context.Context, system, user string) (string, error)
}

// Suggestion is one LLM-proposed project improvement.
type Suggestion struct {
	Title  string `json:"title"`  // short imperative summary, becomes one markdown checklist item
	Detail string `json:"detail"` // one or two sentences of specifics - what and where
}

const systemPrompt = `You are a C/C++ code review assistant. You will be given a project's cmaker.yaml and some or all of its source files. Identify concrete, actionable improvements - things like: missing error handling, inefficient algorithms/data structures, potential bugs, missing const-correctness, resource management issues, code duplication, missing tests, outdated patterns.

Respond with ONLY a JSON array (no markdown code fence, no prose before or after) of objects shaped exactly like:
[{"title": "short imperative summary", "detail": "one or two sentences of specifics - what and where"}]

Focus on genuinely useful, specific suggestions grounded in the actual code shown - not generic advice a linter would already catch, and not vague platitudes. If there's truly nothing worth suggesting, respond with an empty array: []`

// maxProjectChars bounds how much source gets sent in one request - a
// whole-project dump doesn't scale, so this caps total source content the
// same way internal/heal already truncates failing logs. Files are
// included largest-first (a rough proxy for "most likely to contain
// something worth suggesting") until the budget runs out; a file that
// doesn't fully fit in what's left is skipped entirely rather than
// truncated mid-file, which would hand the model a syntactically broken
// fragment.
const maxProjectChars = 40000

// Suggest asks completer for improvement suggestions on the project
// rooted at root.
func Suggest(ctx context.Context, completer Completer, root string) ([]Suggestion, error) {
	prompt, err := BuildPrompt(root)
	if err != nil {
		return nil, err
	}
	raw, err := completer.Complete(ctx, systemPrompt, prompt)
	if err != nil {
		return nil, fmt.Errorf("LLM request failed: %w", err)
	}
	return ParseSuggestions(raw)
}

// BuildPrompt gathers root's cmaker.yaml and as many source files as fit
// within maxProjectChars (largest first) into the user-content string
// ready for Suggest.
func BuildPrompt(root string) (string, error) {
	files, err := explain.WalkSourceFiles(root)
	if err != nil {
		return "", err
	}

	type fileInfo struct {
		rel  string
		size int64
	}
	infos := make([]fileInfo, 0, len(files))
	for _, f := range files {
		fi, err := os.Stat(filepath.Join(root, f))
		if err != nil {
			continue
		}
		infos = append(infos, fileInfo{rel: f, size: fi.Size()})
	}
	sort.Slice(infos, func(i, j int) bool { return infos[i].size > infos[j].size })

	var b strings.Builder
	b.WriteString("Suggest concrete improvements for this C/C++ project.\n\n")

	if cfgData, err := os.ReadFile(filepath.Join(root, "cmaker.yaml")); err == nil {
		fmt.Fprintf(&b, "--- cmaker.yaml ---\n%s\n\n", string(cfgData))
	}

	budget := maxProjectChars
	included := 0
	for _, fi := range infos {
		if budget <= 0 {
			break
		}
		data, err := os.ReadFile(filepath.Join(root, fi.rel))
		if err != nil {
			continue
		}
		content := string(data)
		if len(content) > budget {
			continue
		}
		fmt.Fprintf(&b, "--- file: %s ---\n%s\n\n", fi.rel, content)
		budget -= len(content)
		included++
	}
	if included == 0 {
		return "", fmt.Errorf("no source files found under %s to suggest improvements for", root)
	}
	return b.String(), nil
}

// ParseSuggestions decodes Suggest's raw response - json.Decoder (not
// json.Unmarshal), for the same reason every other structured-output
// parser in this codebase uses it: a live model response has been
// observed wrapping JSON in a code fence and/or appending trailing prose
// after it (see internal/improvise, internal/describe, internal/explain's
// own ParseAnnotations) - Decoder parses just the first JSON value and
// tolerates anything after.
func ParseSuggestions(raw string) ([]Suggestion, error) {
	raw = stripLeadingCodeFence(strings.TrimSpace(raw))
	var suggestions []Suggestion
	dec := json.NewDecoder(strings.NewReader(raw))
	if err := dec.Decode(&suggestions); err != nil {
		return nil, fmt.Errorf("model response wasn't a valid JSON suggestion array: %w\nresponse was:\n%s", err, raw)
	}
	return suggestions, nil
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

// RenderChecklist renders suggestions as a GitHub-flavored markdown
// checklist - designed to be worked through incrementally by hand today,
// and to feed the future `cmaker codegen --intent-from` (which will read
// this same format back, mark completed items `- [x]`, and break down
// oversized ones into sub-checkboxes in place).
func RenderChecklist(suggestions []Suggestion) string {
	var b strings.Builder
	b.WriteString("# cmaker suggest\n\n")
	for _, s := range suggestions {
		fmt.Fprintf(&b, "- [ ] %s\n", s.Title)
		if s.Detail != "" {
			fmt.Fprintf(&b, "\n  %s\n\n", s.Detail)
		}
	}
	return b.String()
}
