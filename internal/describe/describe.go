// Package describe implements §25's natural-language scaffolding: turning
// a plain-English project description into a Plan - a set of choices among
// cmaker's *existing* scaffolding primitives (which template, --with-rust/
// --with-zig, which §17 packages to install). The LLM never writes code;
// it only selects from a menu of real templates/packages cmaker already
// knows about, which keeps the blast radius of a bad decision small (a
// slightly wrong template/package, never hand-rolled application logic) -
// the same "LLM proposes a structured decision, cmaker's deterministic
// machinery executes it" principle internal/codegen and internal/heal
// already established, and live testing on those already showed models
// will hallucinate plausible-looking-but-wrong values if not validated
// against the real, known-good option list afterward.
package describe

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"cmaker/internal/config"
	"cmaker/internal/registry"
	"cmaker/internal/templates"
)

// Completer mirrors internal/codegen.Completer / internal/heal.Completer -
// a single-turn system+user prompt in, text out. Declared locally (not
// imported from either) so this package stays independent; internal/llm's
// Anthropic client satisfies it by duck typing, the same pattern used
// throughout cmaker's other LLM-backed features. Kept separate so tests can
// supply a fake with no network access.
type Completer interface {
	Complete(ctx context.Context, system, user string) (string, error)
}

// Plan is a set of choices among cmaker's existing scaffolding primitives -
// never generated code.
type Plan struct {
	Template   string
	Language   string
	WithRust   bool
	WithZig    bool
	TargetType string
	Packages   []string // registry package names to `cmaker install` after scaffolding
	Reasoning  string   // one or two sentences, for a human to review before it scaffolds
}

type planResponse struct {
	Template   string   `json:"template"`
	Language   string   `json:"language"`
	WithRust   bool     `json:"with_rust"`
	WithZig    bool     `json:"with_zig"`
	TargetType string   `json:"target_type"`
	Packages   []string `json:"packages"`
	Reasoning  string   `json:"reasoning"`
}

// Describe asks completer to turn description into a Plan, validating every
// field against cmaker's actual template list, package registry, and known
// language/target-type values.
func Describe(ctx context.Context, completer Completer, description string) (Plan, error) {
	tmpls, err := templates.List()
	if err != nil {
		return Plan{}, fmt.Errorf("failed to list templates: %w", err)
	}
	pkgs := registry.List()

	raw, err := completer.Complete(ctx, buildSystemPrompt(tmpls, pkgs), description)
	if err != nil {
		return Plan{}, fmt.Errorf("LLM request failed: %w", err)
	}

	jsonText := stripCodeFences(raw)
	var resp planResponse
	if err := json.Unmarshal([]byte(jsonText), &resp); err != nil {
		return Plan{}, fmt.Errorf("could not parse LLM response as JSON: %w\n--- raw response ---\n%s", err, raw)
	}

	validTemplate := false
	for _, t := range tmpls {
		if t.Name == resp.Template {
			validTemplate = true
			break
		}
	}
	if !validTemplate {
		return Plan{}, fmt.Errorf("AI suggested unknown template %q - try rephrasing --describe, or use --template directly", resp.Template)
	}

	lang := config.LanguageOrDefault(resp.Language)
	if !config.ValidLanguages[lang] {
		return Plan{}, fmt.Errorf("AI suggested unknown language %q", resp.Language)
	}
	targetType := config.TargetTypeOrDefault(resp.TargetType)
	if !config.ValidTargetTypes[targetType] {
		return Plan{}, fmt.Errorf("AI suggested unknown target_type %q", resp.TargetType)
	}

	// An unrecognized package suggestion is dropped rather than failing the
	// whole plan - a minor omission (one fewer dependency pre-installed),
	// unlike an invalid template/language which changes the core decision
	// and is worth failing loudly over instead of silently guessing.
	var validPackages []string
	for _, p := range resp.Packages {
		if _, ok := registry.Find(p); ok {
			validPackages = append(validPackages, p)
		}
	}

	withRust, withZig := resp.WithRust, resp.WithZig
	// Mirrors scaffoldProject's (cmd/new.go) actual composition rules
	// exactly, correcting fields the model got wrong rather than trusting
	// its self-reported values or failing the whole plan over a field that
	// isn't even the core decision: language != cpp only composes with the
	// "default" template; target_type != executable additionally requires
	// language == cpp and no Rust/Zig. Note --with-rust/--with-zig
	// themselves compose with *any* template (§18) and are deliberately
	// left untouched here - a live test initially over-corrected by
	// zeroing them out for every non-default template, which would have
	// silently broken the exact "--backend --with-rust" composition §18
	// exists to support.
	if resp.Template != "default" && lang != "cpp" {
		lang = "cpp"
	}
	if targetType != "executable" && (resp.Template != "default" || lang != "cpp" || withRust || withZig) {
		targetType = "executable"
	}

	return Plan{
		Template:   resp.Template,
		Language:   lang,
		WithRust:   withRust,
		WithZig:    withZig,
		TargetType: targetType,
		Packages:   validPackages,
		Reasoning:  strings.TrimSpace(resp.Reasoning),
	}, nil
}

func buildSystemPrompt(tmpls []templates.Meta, pkgs []registry.Entry) string {
	var b strings.Builder
	b.WriteString("You are a project-scaffolding assistant for cmaker, a CMake project generator. Given a natural-language description of a C/C++ project, select which of cmaker's EXISTING building blocks best fit it. You do not write any code - you only choose from the options below.\n\n")

	b.WriteString("Available templates:\n")
	for _, t := range tmpls {
		fmt.Fprintf(&b, "- %s: %s\n", t.Name, t.Description)
	}

	b.WriteString("\nAvailable packages (installable via 'cmaker install <name>'):\n")
	for _, p := range pkgs {
		fmt.Fprintf(&b, "- %s: %s\n", p.Name, p.Notes)
	}

	b.WriteString(`
Respond with ONLY a JSON object (no prose, no markdown code fences) with exactly these fields:
{
  "template": "<one of the template names above, exactly as written>",
  "language": "cpp" | "c" | "hybrid",
  "with_rust": bool,
  "with_zig": bool,
  "target_type": "executable" | "static_library" | "shared_library",
  "packages": ["<zero or more package names above that this project would clearly benefit from>"],
  "reasoning": "<one or two sentences explaining the choices, for a human to review before it scaffolds>"
}

Rules:
- language/with_rust/with_zig/target_type only really compose freely with the "default" template - if you pick a different template, set language to "cpp", with_rust and with_zig to false, and target_type to "executable" unless there is a strong reason otherwise.
- Only include packages that are clearly and directly relevant - do not pad the list.
- If nothing fits particularly well, default to the "default" template with an empty packages list rather than forcing a poor match.`)

	return b.String()
}

// stripCodeFences trims a leading/trailing ```/```json markdown fence,
// since models frequently wrap JSON output in one even when explicitly
// told not to - the same real, observed-live quirk internal/codegen and
// internal/heal both already had to defend against.
func stripCodeFences(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	lines := strings.Split(s, "\n")
	if len(lines) < 2 {
		return s
	}
	lines = lines[1:]
	if len(lines) > 0 && strings.HasPrefix(strings.TrimSpace(lines[len(lines)-1]), "```") {
		lines = lines[:len(lines)-1]
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}
