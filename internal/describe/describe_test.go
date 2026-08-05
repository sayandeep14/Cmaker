package describe

import (
	"context"
	"strings"
	"testing"
)

type fakeCompleter struct {
	response string
	err      error

	gotSystem, gotUser string
}

func (f *fakeCompleter) Complete(ctx context.Context, system, user string) (string, error) {
	f.gotSystem, f.gotUser = system, user
	return f.response, f.err
}

func TestDescribeValidPlan(t *testing.T) {
	fc := &fakeCompleter{response: `{
		"template": "backend",
		"language": "cpp",
		"with_rust": true,
		"with_zig": false,
		"target_type": "executable",
		"packages": ["nlohmann-json", "not-a-real-package"],
		"reasoning": "A REST API needs an HTTP server and JSON support."
	}`}

	plan, err := Describe(context.Background(), fc, "a REST API with JSON support")
	if err != nil {
		t.Fatalf("Describe() error = %v", err)
	}
	if plan.Template != "backend" {
		t.Errorf("Plan.Template = %q, want backend", plan.Template)
	}
	if !plan.WithRust || plan.WithZig {
		t.Errorf("Plan.WithRust/WithZig = %v/%v, want true/false", plan.WithRust, plan.WithZig)
	}
	if plan.TargetType != "executable" {
		t.Errorf("Plan.TargetType = %q, want executable", plan.TargetType)
	}
	// The hallucinated package must be silently dropped, the real one kept.
	if len(plan.Packages) != 1 || plan.Packages[0] != "nlohmann-json" {
		t.Errorf("Plan.Packages = %v, want [nlohmann-json] (hallucinated package dropped)", plan.Packages)
	}
	if plan.Reasoning == "" {
		t.Error("expected non-empty Reasoning")
	}
	if !strings.Contains(fc.gotUser, "a REST API with JSON support") {
		t.Errorf("expected the description in the prompt, got: %q", fc.gotUser)
	}
	if !strings.Contains(fc.gotSystem, "backend:") {
		t.Errorf("expected the real template list in the system prompt, got: %q", fc.gotSystem)
	}
}

func TestDescribeUnknownTemplateRejected(t *testing.T) {
	fc := &fakeCompleter{response: `{"template": "rest-api-generator", "language": "cpp", "target_type": "executable"}`}
	if _, err := Describe(context.Background(), fc, "anything"); err == nil {
		t.Error("expected an error for a hallucinated, nonexistent template")
	}
}

func TestDescribeUnknownLanguageRejected(t *testing.T) {
	fc := &fakeCompleter{response: `{"template": "default", "language": "rust", "target_type": "executable"}`}
	if _, err := Describe(context.Background(), fc, "anything"); err == nil {
		t.Error("expected an error for an invalid language")
	}
}

func TestDescribeUnknownTargetTypeRejected(t *testing.T) {
	fc := &fakeCompleter{response: `{"template": "default", "language": "cpp", "target_type": "header_only"}`}
	if _, err := Describe(context.Background(), fc, "anything"); err == nil {
		t.Error("expected an error for an invalid target_type")
	}
}

func TestDescribeNormalizesIncompatibleFieldsForNonDefaultTemplate(t *testing.T) {
	// Real, observed model behavior from a live end-to-end run: the model
	// picked template="headeronly" together with target_type=
	// "static_library" despite the prompt explicitly saying non-default
	// templates should keep target_type at "executable" - scaffoldProject
	// rejects that combination outright, which used to fail the whole plan
	// over an auxiliary field, not the actual template decision.
	fc := &fakeCompleter{response: `{
		"template": "headeronly",
		"language": "c",
		"with_rust": true,
		"with_zig": true,
		"target_type": "static_library",
		"reasoning": "test"
	}`}

	plan, err := Describe(context.Background(), fc, "a header-only matrix library")
	if err != nil {
		t.Fatalf("Describe() error = %v", err)
	}
	if plan.Template != "headeronly" {
		t.Errorf("Plan.Template = %q, want headeronly (the actual decision should still be honored)", plan.Template)
	}
	if plan.Language != "cpp" {
		t.Errorf("Plan.Language = %q, want cpp (language != cpp only composes with the default template)", plan.Language)
	}
	if plan.TargetType != "executable" {
		t.Errorf("Plan.TargetType = %q, want executable (a non-default template can't scaffold a library target)", plan.TargetType)
	}
	// --with-rust/--with-zig compose with ANY template (§18) - these must
	// survive normalization untouched, not get zeroed out just because the
	// template isn't "default".
	if !plan.WithRust || !plan.WithZig {
		t.Errorf("Plan.WithRust/WithZig = %v/%v, want true/true preserved (they compose with any template per §18)", plan.WithRust, plan.WithZig)
	}
}

func TestDescribeNormalizesLibraryTargetTypeWithRustZig(t *testing.T) {
	// A library target_type combined with --with-rust/--with-zig is
	// invalid even with the "default" template (scaffoldProject's own
	// restriction, unrelated to §18) - target_type should be the one that
	// gives way here, not with_rust/with_zig.
	fc := &fakeCompleter{response: `{
		"template": "default",
		"language": "cpp",
		"with_rust": true,
		"with_zig": false,
		"target_type": "static_library",
		"reasoning": "test"
	}`}
	plan, err := Describe(context.Background(), fc, "a library with a Rust component")
	if err != nil {
		t.Fatalf("Describe() error = %v", err)
	}
	if plan.TargetType != "executable" {
		t.Errorf("Plan.TargetType = %q, want executable (static_library + with_rust is invalid regardless of template)", plan.TargetType)
	}
	if !plan.WithRust {
		t.Error("Plan.WithRust = false, want true preserved")
	}
}

func TestDescribeDefaultsApplied(t *testing.T) {
	// Omitted language/target_type should resolve to their defaults
	// (cpp/executable) via config.LanguageOrDefault/TargetTypeOrDefault,
	// same as an unset field in cmaker.yaml itself.
	fc := &fakeCompleter{response: `{"template": "default"}`}
	plan, err := Describe(context.Background(), fc, "a simple tool")
	if err != nil {
		t.Fatalf("Describe() error = %v", err)
	}
	if plan.Language != "cpp" {
		t.Errorf("Plan.Language = %q, want cpp (default)", plan.Language)
	}
	if plan.TargetType != "executable" {
		t.Errorf("Plan.TargetType = %q, want executable (default)", plan.TargetType)
	}
}

func TestDescribeStripsCodeFences(t *testing.T) {
	fc := &fakeCompleter{response: "```json\n{\"template\": \"default\", \"language\": \"cpp\", \"target_type\": \"executable\"}\n```"}
	plan, err := Describe(context.Background(), fc, "anything")
	if err != nil {
		t.Fatalf("Describe() error = %v", err)
	}
	if plan.Template != "default" {
		t.Errorf("Plan.Template = %q, want default", plan.Template)
	}
}

func TestDescribeMalformedJSON(t *testing.T) {
	fc := &fakeCompleter{response: "I'd suggest the backend template."}
	if _, err := Describe(context.Background(), fc, "anything"); err == nil {
		t.Error("expected an error for a non-JSON LLM response")
	}
}

func TestDescribeCompleterError(t *testing.T) {
	fc := &fakeCompleter{err: context.DeadlineExceeded}
	if _, err := Describe(context.Background(), fc, "anything"); err == nil {
		t.Error("expected an error when the LLM request itself fails")
	}
}
