// Package improvise implements §28's `--improvise` flag and its paired
// scaffold-modification step: once `--describe` has selected a template
// (internal/describe, unchanged - still menu-selection only), improvise
// lets an LLM propose actual changes to the scaffolded files themselves, to
// better match the plain-English description - and, when `--improvise` is
// set, first asks the LLM whether it has enough information at all, posing
// structured clarifying questions to the user if not.
//
// Both capabilities are a deliberate, explicitly-flagged departure from the
// "LLM only ever selects from a known-good menu; cmaker's own code
// executes it" principle internal/codegen, internal/heal, and
// internal/describe all independently converged on and held to (see
// ROADMAP.md §28) - modifying scaffold content is asking the LLM to author
// real code. The mitigation is reusing internal/heal's own hard-won
// diff-computation machinery (ParseFileBlocks/DiffFromModelResponse) - the
// same "ask for full corrected file content, let cmaker compute and verify
// the diff itself" shape §24 already spent real effort de-risking, not a
// second, independently-maintained mechanism.
package improvise

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"cmaker/internal/heal"
)

// Completer mirrors every other AI-backed package's own interface
// (internal/codegen, internal/heal, internal/describe) - a single-turn
// system+user prompt in, text out. Declared locally, not imported, so this
// package stays independent; internal/llm's Anthropic client satisfies it
// by duck typing.
type Completer interface {
	Complete(ctx context.Context, system, user string) (string, error)
}

// QuestionType is how a clarifying Question should be rendered/answered -
// mirrors this terminal's own AskUserQuestion shape (single-select,
// multi-select, free-text), the closest existing precedent for "an LLM (or
// agent) asks a human a small set of structured questions."
type QuestionType string

const (
	QuestionSingleSelect QuestionType = "single_select"
	QuestionMultiSelect  QuestionType = "multi_select"
	QuestionFreeText     QuestionType = "free_text"
)

// Question is one clarifying question the model wants answered before it's
// confident enough to scaffold. Options is only meaningful for the two
// select types.
type Question struct {
	Prompt  string       `json:"prompt"`
	Type    QuestionType `json:"type"`
	Options []string     `json:"options,omitempty"`
}

// maxQuestions bounds how many clarifying questions a single Clarify call
// can return - mirrors AskUserQuestion's own 1-4 question cap, keeping the
// interactive prompt this feeds into (see cmd/) from turning into an
// interrogation.
const maxQuestions = 4

type clarifyResponse struct {
	Proceed   bool       `json:"proceed"`
	Questions []Question `json:"questions,omitempty"`
}

const clarifySystemPrompt = `You are helping scaffold a C/C++ project from a plain-English description. Decide whether the description below has enough information to scaffold a project confidently, or whether you should ask clarifying questions first.

Respond with ONLY a JSON object (no prose, no markdown code fences) with exactly these fields:
{
  "proceed": bool,
  "questions": [
    {"prompt": "<question text>", "type": "single_select" | "multi_select" | "free_text", "options": ["<option>", ...]}
  ]
}

Rules:
- If "proceed" is true, "questions" must be empty - you have enough information.
- If "proceed" is false, ask at most 4 short, genuinely decision-relevant questions (not busywork) - each with "options" populated for single_select/multi_select, omitted for free_text.
- Only ask questions whose answer would actually change what gets scaffolded (which framework, which language feature, sync vs. async, etc.) - don't ask about things with an obvious sensible default.
- Most reasonably-specific descriptions already have enough information - default to "proceed": true unless something genuinely load-bearing is ambiguous.`

// Clarify asks completer whether description has enough information to
// scaffold a project confidently. proceed=true means yes - questions is
// always empty in that case. Otherwise, questions holds up to maxQuestions
// structured questions for the caller to render interactively (see cmd/ for
// the actual CLI prompt) before retrying with an enriched description.
func Clarify(ctx context.Context, completer Completer, description string) (proceed bool, questions []Question, err error) {
	raw, err := completer.Complete(ctx, clarifySystemPrompt, description)
	if err != nil {
		return false, nil, fmt.Errorf("LLM request failed: %w", err)
	}

	var resp clarifyResponse
	if err := decodeFirstJSONValue(raw, &resp); err != nil {
		return false, nil, fmt.Errorf("could not parse LLM clarification response as JSON: %w\n--- raw response ---\n%s", err, raw)
	}
	if resp.Proceed || len(resp.Questions) == 0 {
		return true, nil, nil
	}

	questions = resp.Questions
	if len(questions) > maxQuestions {
		questions = questions[:maxQuestions]
	}
	for i, q := range questions {
		// An unrecognized type falls back to free_text - the one shape
		// that's always valid to render and answer, regardless of what
		// the model actually meant.
		if q.Type != QuestionSingleSelect && q.Type != QuestionMultiSelect && q.Type != QuestionFreeText {
			questions[i].Type = QuestionFreeText
		}
	}
	return false, questions, nil
}

// scaffoldSourceExts bounds ModifyScaffold to files a template actually
// authors (source/header files under src//include/) - never
// CMakeLists.txt (regenerated from cmaker.yaml on every build regardless
// of what's written here, so asking the model to edit it would be
// pointless) or config files like cmaker.yaml/.clang-format.
var scaffoldSourceExts = map[string]bool{
	".c": true, ".h": true,
	".cpp": true, ".hpp": true, ".cxx": true, ".cc": true,
}

// CollectScaffoldFiles walks root's src/ and include/ directories (if
// present) and returns every source/header file's path relative to root,
// sorted - the file set ModifyScaffold reads and may propose changes to.
func CollectScaffoldFiles(root string) ([]string, error) {
	var files []string
	for _, sub := range []string{"src", "include"} {
		dir := filepath.Join(root, sub)
		err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil // missing src/ or include/ (a template needn't have both) - not an error
			}
			if info.IsDir() {
				return nil
			}
			if !scaffoldSourceExts[strings.ToLower(filepath.Ext(path))] {
				return nil
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return nil
			}
			files = append(files, rel)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return files, nil
}

const modifySystemPrompt = `You are a C/C++ project scaffolding assistant. A project was just scaffolded from a template based on a plain-English description. You may modify the scaffolded files to better match that description - for example, adding real logic the description implies, adjusting structure, or wiring in a specific pattern mentioned.

For each file you want to change, output its ENTIRE corrected file content (not a diff, not a snippet - the complete file from its first line to its last), in exactly this format, one block per changed file:

--- file: <path> ---
<complete corrected file content>

Only include a block for a file you are actually changing - omit files that already fit well. Output nothing else: no prose, no explanation, no markdown code fences, nothing outside the "--- file: <path> ---" blocks.

If no changes are needed, output exactly: NO_CHANGES`

// ModifyScaffold asks completer to propose changes to the already-
// scaffolded project at root (every file named in files, relative to root)
// to better match description, reusing internal/heal's own
// diff-computation (DiffFromModelResponse/ParseFileBlocks) rather than
// inventing a second "LLM proposes full file content, cmaker verifies it"
// mechanism (see this package's own doc). Returns an empty diff and a nil
// changed map if the model finds nothing worth changing - the caller
// should treat that as "nothing to review or apply," not an error.
func ModifyScaffold(ctx context.Context, completer Completer, root, description, planReasoning string, files []string) (diff string, changed map[string]string, err error) {
	var b strings.Builder
	fmt.Fprintf(&b, "Project description: %s\n", description)
	if planReasoning != "" {
		fmt.Fprintf(&b, "Template selection reasoning: %s\n", planReasoning)
	}
	b.WriteString("\n")

	original := make(map[string]string, len(files))
	var order []string
	for _, rel := range files {
		data, readErr := os.ReadFile(filepath.Join(root, rel))
		if readErr != nil {
			continue // a file that doesn't actually exist - skip it, don't fail the whole request
		}
		content := string(data)
		fmt.Fprintf(&b, "--- file: %s ---\n%s\n\n", rel, content)
		original[rel] = content
		order = append(order, rel)
	}
	if len(order) == 0 {
		return "", nil, fmt.Errorf("no scaffolded source files found under %s to modify", root)
	}

	raw, err := completer.Complete(ctx, modifySystemPrompt, b.String())
	if err != nil {
		return "", nil, fmt.Errorf("LLM request failed: %w", err)
	}

	trimmed := strings.TrimSpace(raw)
	if trimmed == "NO_CHANGES" {
		return "", nil, nil
	}

	diff, err = heal.DiffFromModelResponse(raw, original, order)
	if err != nil {
		return "", nil, err
	}
	if diff == "" {
		return "", nil, nil
	}

	proposed := heal.ParseFileBlocks(raw)
	changed = make(map[string]string)
	for _, rel := range order {
		newContent, ok := proposed[rel]
		if !ok || newContent == original[rel] {
			continue
		}
		changed[rel] = newContent
	}
	return diff, changed, nil
}

// stripCodeFences trims a leading/trailing ```/```json markdown fence - the
// same real, observed-live model quirk internal/describe/internal/codegen/
// internal/heal all already defend against.
func stripLeadingCodeFence(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	_, rest, found := strings.Cut(s, "\n")
	if !found {
		return s
	}
	return strings.TrimSpace(rest)
}

// decodeFirstJSONValue strips a leading markdown code fence (if any, the
// same real observed-live quirk every other AI-backed package in this
// codebase already defends against) and decodes only the *first* JSON
// value from what's left into v, via json.Decoder rather than
// json.Unmarshal.
//
// This distinction is load-bearing, not stylistic: json.Unmarshal requires
// the entire input to be exactly one JSON value with nothing else, so it
// fails outright the moment a model appends explanatory prose after a
// closing ``` fence (observed live: claude-haiku-4-5 responded with a
// perfectly valid ```json ... ``` block immediately followed by several
// sentences of unrequested explanation) - the old stripCodeFences only
// handled a response that *was* the fenced block end-to-end, not one with
// real content trailing it. json.Decoder.Decode reads and parses just the
// first complete JSON value from the stream and simply stops, tolerating
// whatever comes after - exactly what's needed here, and a strictly better
// default for this whole class of "structured decision plus the model's
// own uninvited commentary" response.
func decodeFirstJSONValue(raw string, v any) error {
	s := stripLeadingCodeFence(raw)
	return json.NewDecoder(strings.NewReader(s)).Decode(v)
}
