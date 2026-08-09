package explain

import (
	"context"
	"strings"
	"testing"
)

func TestAskReview(t *testing.T) {
	fc := &fakeCompleter{response: "The added function doesn't check for a null pointer before dereferencing it."}
	got, err := AskReview(context.Background(), fc, "some diff content")
	if err != nil {
		t.Fatalf("AskReview() error = %v", err)
	}
	if got != "The added function doesn't check for a null pointer before dereferencing it." {
		t.Errorf("AskReview() = %q, unexpected", got)
	}
	if fc.gotSystem != reviewSystemPrompt {
		t.Error("AskReview() should use review's own system prompt, not explain's generic one")
	}
}

func TestBuildReviewPromptNoChanges(t *testing.T) {
	root := t.TempDir()
	runGit(t, root, "init")
	runGit(t, root, "-c", "user.email=t@example.com", "-c", "user.name=Test", "commit", "--allow-empty", "-m", "init")

	got, err := BuildReviewPrompt(root)
	if err != nil {
		t.Fatalf("BuildReviewPrompt() error = %v", err)
	}
	if got != "" {
		t.Errorf("BuildReviewPrompt() = %q, want empty for a clean working tree", got)
	}
}

func TestBuildReviewPromptIncludesStagedChanges(t *testing.T) {
	root := t.TempDir()
	runGit(t, root, "init")
	writeFile(t, root, "main.cpp", "int main() { return 0; }\n")
	runGit(t, root, "add", "main.cpp")
	runGit(t, root, "-c", "user.email=t@example.com", "-c", "user.name=Test", "commit", "-m", "init")

	writeFile(t, root, "main.cpp", "int main() { return 1; }\n")
	runGit(t, root, "add", "main.cpp") // staged, not committed - BuildDiffPrompt's plain `git diff` would miss this

	got, err := BuildReviewPrompt(root)
	if err != nil {
		t.Fatalf("BuildReviewPrompt() error = %v", err)
	}
	if !strings.Contains(got, "return 1;") {
		t.Errorf("BuildReviewPrompt() = %q, missing the staged diff content", got)
	}
}

func TestBuildReviewPromptIncludesUnstagedChanges(t *testing.T) {
	root := t.TempDir()
	runGit(t, root, "init")
	writeFile(t, root, "main.cpp", "int main() { return 0; }\n")
	runGit(t, root, "add", "main.cpp")
	runGit(t, root, "-c", "user.email=t@example.com", "-c", "user.name=Test", "commit", "-m", "init")

	writeFile(t, root, "main.cpp", "int main() { return 2; }\n")

	got, err := BuildReviewPrompt(root)
	if err != nil {
		t.Fatalf("BuildReviewPrompt() error = %v", err)
	}
	if !strings.Contains(got, "return 2;") {
		t.Errorf("BuildReviewPrompt() = %q, missing the unstaged diff content", got)
	}
}
