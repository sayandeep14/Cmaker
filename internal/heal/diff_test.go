package heal

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestUnifiedDiffIdenticalContent(t *testing.T) {
	if got := unifiedDiff("x.cpp", "same\n", "same\n"); got != "" {
		t.Errorf("unifiedDiff() for identical content = %q, want empty", got)
	}
}

func TestUnifiedDiffSimpleChange(t *testing.T) {
	old := "line1\nline2\nline3\n"
	new_ := "line1\nCHANGED\nline3\n"
	got := unifiedDiff("f.cpp", old, new_)

	for _, want := range []string{
		"--- a/f.cpp",
		"+++ b/f.cpp",
		"-line2",
		"+CHANGED",
		" line1", // context
		" line3", // context
	} {
		if !strings.Contains(got, want) {
			t.Errorf("unifiedDiff() missing %q:\n%s", want, got)
		}
	}
}

func TestUnifiedDiffHunkHeaderIsConsistentWithBody(t *testing.T) {
	// Regression test for the real bug this whole redesign was driven by:
	// an LLM-authored diff had a hunk header line count that didn't match
	// its own body, which `git apply` rejected as corrupt. Since we now
	// compute the diff ourselves, the header must always be internally
	// consistent - verify this holds for a handful of shapes, and (where
	// git is available) that the result is actually a valid, applyable
	// patch, not just "looks right."
	cases := []struct {
		name string
		old  string
		new  string
	}{
		{"single line change with context", "a\nb\nc\nd\ne\n", "a\nb\nX\nd\ne\n"},
		{"insertion only", "a\nb\n", "a\nINSERTED\nb\n"},
		{"deletion only", "a\nb\nc\n", "a\nc\n"},
		{"change at start of file", "first\nb\nc\n", "CHANGED\nb\nc\n"},
		{"change at end of file, no trailing newline", "a\nb\nlast", "a\nb\nCHANGED"},
		{"multi-line insertion", "a\nb\n", "a\nx\ny\nz\nb\n"},
	}

	gitPath, lookErr := exec.LookPath("git")

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			diff := unifiedDiff("f.cpp", tc.old, tc.new)
			if diff == "" {
				t.Fatal("expected a non-empty diff for a real content change")
			}

			if lookErr == nil {
				verifyPatchApplies(t, gitPath, tc.old, diff, tc.new)
			}
		})
	}
}

// verifyPatchApplies actually runs `git apply` against a real file and
// checks the result matches the expected new content exactly - the
// strongest possible check that unifiedDiff produces a genuinely valid,
// consumable patch, not just diff-shaped text.
func verifyPatchApplies(t *testing.T, gitPath, oldContent, diff, wantContent string) {
	t.Helper()
	dir := t.TempDir()
	if err := exec.Command(gitPath, "-C", dir, "init", "-q").Run(); err != nil {
		t.Skipf("git init failed, skipping live-apply check: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "f.cpp"), []byte(oldContent), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "patch.diff"), []byte(diff+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(gitPath, "apply", "patch.diff")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git apply rejected the computed diff: %v\n%s\n--- diff ---\n%s", err, out, diff)
	}

	got, err := os.ReadFile(filepath.Join(dir, "f.cpp"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != wantContent {
		t.Errorf("after applying the patch, file content = %q, want %q", got, wantContent)
	}
}
