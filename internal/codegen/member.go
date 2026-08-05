package codegen

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Member describes one data member of a class, as extracted by an LLM (see
// Completer/ExtractMembers below).
type Member struct {
	Name              string `json:"name"`
	Type              string `json:"type"`
	IsConst           bool   `json:"is_const"`
	ReturnByReference bool   `json:"return_by_reference"`
}

// Completer is the minimal interface cmaker needs from an LLM backend: a
// single-turn system+user prompt in, text out. Satisfied by internal/llm's
// Anthropic client. Declared here (not in internal/llm) so codegen doesn't
// need to import a concrete provider, and so tests can supply a fake with
// no network access.
type Completer interface {
	Complete(ctx context.Context, system, user string) (string, error)
}

const extractSystemPrompt = `You are a C++ code analysis assistant. Given the body of a C++ class or struct, identify its non-public data members that should get generated getter/setter accessor methods.

Respond with ONLY a JSON array (no prose, no markdown code fences) where each element is an object with exactly these fields:
  "name": the member's identifier, exactly as declared
  "type": the member's declared type, without the member name or any trailing default initializer (e.g. "int", "std::string", "std::vector<int>", "Foo*")
  "is_const": true if the member itself is declared const (such members should only get a getter, never a setter)
  "return_by_reference": true if the getter should return "const Type&" instead of copying by value (appropriate for non-trivial types like std::string, containers, or other class types); false for primitives, enums, and pointers

Rules:
- Only include members that are NOT under a "public:" section (i.e. members that are private or protected, or come before any access specifier in a class where the default is private).
- Skip static members.
- Skip members that are themselves function pointers or member function declarations.
- If there are no eligible members, respond with an empty JSON array: []`

// ExtractMembers asks completer to identify classBody's accessor-eligible
// data members.
func ExtractMembers(ctx context.Context, completer Completer, className, classBody string) ([]Member, error) {
	user := fmt.Sprintf("Class name: %s\n\nClass body:\n%s", className, classBody)
	raw, err := completer.Complete(ctx, extractSystemPrompt, user)
	if err != nil {
		return nil, fmt.Errorf("LLM request failed: %w", err)
	}

	jsonText := stripCodeFences(raw)
	var members []Member
	if err := json.Unmarshal([]byte(jsonText), &members); err != nil {
		return nil, fmt.Errorf("could not parse LLM response as JSON: %w\n--- raw response ---\n%s", err, raw)
	}
	return members, nil
}

// stripCodeFences trims a leading/trailing ```/```json markdown fence,
// since models frequently wrap JSON output in one even when explicitly
// told not to.
func stripCodeFences(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	lines := strings.Split(s, "\n")
	if len(lines) < 2 {
		return s
	}
	lines = lines[1:] // drop opening ``` or ```json line
	if len(lines) > 0 && strings.HasPrefix(strings.TrimSpace(lines[len(lines)-1]), "```") {
		lines = lines[:len(lines)-1]
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}
