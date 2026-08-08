// Package explain implements `cmaker explain <target>` (§29): ask an LLM to
// explain something about the current project in plain English - a class, a
// function, the most recent build/run failure, a whole file, the working-tree
// diff, the project's cmake configuration, or a registered dependency.
//
// Unlike every other AI-backed package in this codebase (internal/codegen,
// internal/heal, internal/describe, internal/improvise), explain never
// writes anything and never asks the model for structured output it has to
// validate - it's a pure read, and the model's freeform prose response is
// exactly what gets printed to the user. That's what makes this the safest
// LLM feature in cmaker: there's no diff to apply, no JSON to parse, no
// scaffold decision riding on the response.
package explain

import (
	"context"
	"strings"
)

// Completer is the minimal interface explain needs from an LLM backend - a
// single-turn system+user prompt in, text out. Declared here rather than
// imported (mirroring internal/heal.Completer/internal/describe.Completer),
// so this package stays independent; internal/llm's Anthropic client
// satisfies it by duck typing. Kept separate so tests can supply a fake with
// no network access.
type Completer interface {
	Complete(ctx context.Context, system, user string) (string, error)
}

// systemPrompt is deliberately generic - the different Build*Prompt
// functions in context.go are what actually specialize the request per
// target, by shaping the user-content string each one hands to Ask.
const systemPrompt = `You are a C/C++ project assistant helping a developer understand their own codebase. You will be given some context - a class, a function, a build/run failure log, a file, a git diff, a project's CMake configuration, or a dependency - and asked to explain it in plain English.

Be clear, concise, and specific to the actual content given - not a generic textbook explanation. Prefer plain prose over bullet lists unless the content itself is naturally a list of distinct things. Do not propose code changes or fixes unless directly asked to; the goal is understanding, not action.`

// Ask sends userContent to completer under explain's system prompt and
// returns the trimmed response - the one shared step every target (class=,
// function=, lastError, file=, diff, config, dependency=) funnels through
// after building its own context via the Build*Prompt functions.
func Ask(ctx context.Context, completer Completer, userContent string) (string, error) {
	resp, err := completer.Complete(ctx, systemPrompt, userContent)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(resp), nil
}
