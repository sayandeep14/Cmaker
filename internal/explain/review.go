package explain

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// reviewSystemPrompt is deliberately distinct from explain's own
// systemPrompt (see explain.go) - explain never proposes changes or
// judges quality, only describes; review's whole point is to critique.
const reviewSystemPrompt = `You are an experienced C/C++ code reviewer looking at a git diff of uncommitted changes before they're committed. Point out genuine concerns: likely bugs, missing error handling, resource/memory management issues, missing const-correctness, edge cases the diff doesn't handle, style inconsistencies with the surrounding code, and anything that looks unfinished (TODOs, debug prints left in, etc).

Be specific and grounded in the actual diff shown - not generic advice. If the diff genuinely looks fine, say so briefly rather than inventing nitpicks. Do not rewrite the code yourself - describe what should change and why; the developer applies it themselves.`

// AskReview sends userContent (see BuildReviewPrompt) to completer under
// review's own critique-oriented system prompt and returns the trimmed
// response - same "prose is the artifact, nothing to parse or apply"
// shape as Ask, just a different system prompt and a different intent
// (critique, not explanation).
func AskReview(ctx context.Context, completer Completer, userContent string) (string, error) {
	resp, err := completer.Complete(ctx, reviewSystemPrompt, userContent)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(resp), nil
}

// BuildReviewPrompt runs `git diff HEAD` in root - every uncommitted
// change, staged or not, matching what a subsequent `cmaker commit`
// would actually commit (unlike BuildDiffPrompt's plain `git diff`,
// which only shows unstaged changes - reviewing "what's about to be
// committed" needs the staged half too) - and returns the user-content
// string for AskReview, empty if there's nothing uncommitted.
func BuildReviewPrompt(root string) (string, error) {
	out, err := exec.Command("git", "-C", root, "diff", "HEAD").Output()
	if err != nil {
		return "", fmt.Errorf("failed to run 'git diff HEAD': %w", err)
	}
	diff := strings.TrimSpace(string(out))
	if diff == "" {
		return "", nil
	}
	return fmt.Sprintf("Review this git diff (uncommitted changes, staged or not) before it's committed:\n\n%s\n", diff), nil
}
