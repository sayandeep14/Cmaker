package docsgen

import (
	"context"
	"errors"
	"os"
	"path/filepath"
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

func TestNarrateFile(t *testing.T) {
	fc := &fakeCompleter{response: "This file implements the main entry point."}
	got, err := NarrateFile(context.Background(), fc, "src/main.cpp", "int main(){}")
	if err != nil {
		t.Fatalf("NarrateFile() error = %v", err)
	}
	if got != "This file implements the main entry point." {
		t.Errorf("NarrateFile() = %q, unexpected", got)
	}
	if !strings.Contains(fc.gotUser, "src/main.cpp") {
		t.Error("expected the file path in the prompt")
	}
}

func TestNarrateFileStripsFence(t *testing.T) {
	fc := &fakeCompleter{response: "```markdown\nSome doc text.\n```"}
	got, err := NarrateFile(context.Background(), fc, "a.cpp", "x")
	if err != nil {
		t.Fatalf("NarrateFile() error = %v", err)
	}
	if strings.Contains(got, "```") {
		t.Errorf("NarrateFile() = %q, want the code fence stripped", got)
	}
}

func TestNarrateFilePropagatesCompleterError(t *testing.T) {
	fc := &fakeCompleter{err: errors.New("network error")}
	if _, err := NarrateFile(context.Background(), fc, "a.cpp", "x"); err == nil {
		t.Error("NarrateFile() expected an error when the completer fails, got nil")
	}
}

func TestNarrateArchitecture(t *testing.T) {
	fc := &fakeCompleter{response: "# Architecture\n\nThis project is a CLI tool."}
	got, err := NarrateArchitecture(context.Background(), fc, "some prompt")
	if err != nil {
		t.Fatalf("NarrateArchitecture() error = %v", err)
	}
	if !strings.Contains(got, "CLI tool") {
		t.Errorf("NarrateArchitecture() = %q, unexpected", got)
	}
}

func TestGenerateDoxygenCommentAlreadyDocumented(t *testing.T) {
	fc := &fakeCompleter{response: "ALREADY_DOCUMENTED"}
	got, err := GenerateDoxygenComment(context.Background(), fc, "function", "add", "int add(int a, int b){ return a+b; }")
	if err != nil {
		t.Fatalf("GenerateDoxygenComment() error = %v", err)
	}
	if got != "" {
		t.Errorf("GenerateDoxygenComment() = %q, want empty for ALREADY_DOCUMENTED", got)
	}
}

func TestGenerateDoxygenCommentReturnsBlock(t *testing.T) {
	response := "/**\n * @brief Adds two integers.\n * @param a first operand\n * @param b second operand\n * @return the sum\n */"
	fc := &fakeCompleter{response: response}
	got, err := GenerateDoxygenComment(context.Background(), fc, "function", "add", "int add(int a, int b){ return a+b; }")
	if err != nil {
		t.Fatalf("GenerateDoxygenComment() error = %v", err)
	}
	if !strings.HasPrefix(got, "/**") || !strings.HasSuffix(got, "*/") {
		t.Errorf("GenerateDoxygenComment() = %q, want a /** ... */ block", got)
	}
}

func TestGenerateDoxygenCommentRejectsNonCommentResponse(t *testing.T) {
	fc := &fakeCompleter{response: "Sure, here's a comment for you:"}
	if _, err := GenerateDoxygenComment(context.Background(), fc, "function", "add", "int add(){}"); err == nil {
		t.Error("GenerateDoxygenComment() with a non-comment response: expected an error, got nil")
	}
}

func TestGenerateDoxygenCommentPropagatesCompleterError(t *testing.T) {
	fc := &fakeCompleter{err: errors.New("network error")}
	if _, err := GenerateDoxygenComment(context.Background(), fc, "function", "add", "int add(){}"); err == nil {
		t.Error("GenerateDoxygenComment() expected an error when the completer fails, got nil")
	}
}

func TestBuildArchitecturePromptIncludesConfigAndSource(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "cmaker.yaml"), []byte("project_name: demo\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.cpp"), []byte("int main(){ return 0; }\n"), 0644); err != nil {
		t.Fatal(err)
	}

	got, err := BuildArchitecturePrompt(root)
	if err != nil {
		t.Fatalf("BuildArchitecturePrompt() error = %v", err)
	}
	if !strings.Contains(got, "project_name: demo") {
		t.Error("expected cmaker.yaml content in the prompt")
	}
	if !strings.Contains(got, "int main(){ return 0; }") {
		t.Error("expected source file content in the prompt")
	}
}

func TestBuildArchitecturePromptNoSourceFiles(t *testing.T) {
	root := t.TempDir()
	if _, err := BuildArchitecturePrompt(root); err == nil {
		t.Error("BuildArchitecturePrompt() with no source files: expected an error, got nil")
	}
}

func TestBuildArchitecturePromptSkipsFilesTooBigForBudget(t *testing.T) {
	root := t.TempDir()
	huge := strings.Repeat("x", maxArchitectureChars+1000)
	if err := os.WriteFile(filepath.Join(root, "huge.cpp"), []byte(huge), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "small.cpp"), []byte("int main(){}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	got, err := BuildArchitecturePrompt(root)
	if err != nil {
		t.Fatalf("BuildArchitecturePrompt() error = %v", err)
	}
	if strings.Contains(got, huge) {
		t.Error("expected the oversized file to be skipped entirely, not truncated in")
	}
	if !strings.Contains(got, "int main(){}") {
		t.Error("expected the small file to still be included")
	}
}
