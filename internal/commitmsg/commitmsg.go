// Package commitmsg implements `cmaker commit`'s LLM-generated commit
// message: given a staged diff, asks an LLM for a conventional-style git
// commit message. Unlike most other AI features in this codebase, the
// output isn't code or a diff that gets applied - it's the commit message
// text itself, used directly (after the user reviews it, or edits it via
// --preview) as `git commit -m <message>`'s argument. There's nothing to
// deterministically re-render here the way there is for code changes; the
// message text *is* the artifact.
package commitmsg

import (
	"context"
	"fmt"
	"strings"
)

// Completer is the minimal interface commitmsg needs from an LLM backend -
// declared here (mirroring every other AI-backed package in this
// codebase, not imported from them) so this package stays independent.
type Completer interface {
	Complete(ctx context.Context, system, user string) (string, error)
}

const systemPrompt = `You are a git commit message assistant. You will be given a diff of staged changes. Write a concise, well-formed commit message following conventional git style:
- A short summary line (50-72 characters), imperative mood ("Add X", "Fix Y" - not "Added"/"Fixes")
- If the change is non-trivial, a blank line followed by a brief body explaining what and why - not restating the diff line by line

Respond with ONLY the commit message itself - no prose before or after, no markdown code fences, no surrounding quotes.`

// maxDiffChars bounds how much of a (possibly very large) staged diff gets
// sent - matches internal/heal's own log-truncation philosophy.
const maxDiffChars = 8000

// Generate asks completer for a commit message describing diff (the
// output of `git diff --staged`).
func Generate(ctx context.Context, completer Completer, diff string) (string, error) {
	if strings.TrimSpace(diff) == "" {
		return "", fmt.Errorf("no staged changes to describe")
	}
	truncated := diff
	if len(truncated) > maxDiffChars {
		truncated = truncated[:maxDiffChars] + "\n...(truncated)..."
	}

	raw, err := completer.Complete(ctx, systemPrompt, "Diff:\n"+truncated)
	if err != nil {
		return "", fmt.Errorf("LLM request failed: %w", err)
	}

	msg := strings.TrimSpace(raw)
	msg = stripFence(msg)
	msg = stripSurroundingQuotes(strings.TrimSpace(msg))
	if msg == "" {
		return "", fmt.Errorf("model returned an empty commit message")
	}
	return msg, nil
}

const branchSlugSystemPrompt = `You are a git branch-naming assistant. You will be given a diff. Respond with ONLY a short branch-name slug describing the change - lowercase, words separated by hyphens, 2-5 words, no prefix (the caller adds its own), no prose, no quotes, no markdown.

Example good responses: "fix-off-by-one-sum", "add-user-auth", "refactor-config-loader"`

// GenerateBranchSlug asks completer for a short kebab-case branch-name
// slug describing diff - used by 'cmaker newbranch' when --name isn't
// given. The caller is responsible for any prefix (e.g. "f-"); this
// returns just the descriptive part, defensively sanitized to a safe git
// ref component even if the model doesn't follow the requested shape
// exactly.
func GenerateBranchSlug(ctx context.Context, completer Completer, diff string) (string, error) {
	if strings.TrimSpace(diff) == "" {
		return "", fmt.Errorf("no changes to describe")
	}
	truncated := diff
	if len(truncated) > maxDiffChars {
		truncated = truncated[:maxDiffChars] + "\n...(truncated)..."
	}

	raw, err := completer.Complete(ctx, branchSlugSystemPrompt, "Diff:\n"+truncated)
	if err != nil {
		return "", fmt.Errorf("LLM request failed: %w", err)
	}

	slug := sanitizeSlug(stripFence(strings.TrimSpace(raw)))
	if slug == "" {
		return "", fmt.Errorf("model returned an empty branch name")
	}
	return slug, nil
}

// sanitizeSlug defensively coerces s into a safe git-ref-component slug:
// lowercase, only [a-z0-9-], no leading/trailing/repeated hyphens - the
// model was told to already produce this shape, but a branch name is
// used directly in a shell-exec'd git command, so it's worth not trusting
// that blindly.
func sanitizeSlug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	lastHyphen := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)
			lastHyphen = false
		case !lastHyphen && b.Len() > 0:
			b.WriteByte('-')
			lastHyphen = true
		}
	}
	return strings.Trim(b.String(), "-")
}

// stripFence removes a single leading and/or trailing markdown code-fence
// line, if present - the model was told not to, but every other prompt in
// this codebase defends against it anyway since it's been observed live
// elsewhere.
func stripFence(s string) string {
	lines := strings.Split(s, "\n")
	if len(lines) > 0 && strings.HasPrefix(strings.TrimSpace(lines[0]), "```") {
		lines = lines[1:]
	}
	if len(lines) > 0 && strings.HasPrefix(strings.TrimSpace(lines[len(lines)-1]), "```") {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
}

// stripSurroundingQuotes removes one layer of matching leading/trailing
// double or single quotes, if the whole message is wrapped in them.
func stripSurroundingQuotes(s string) string {
	if len(s) >= 2 {
		first, last := s[0], s[len(s)-1]
		if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}
