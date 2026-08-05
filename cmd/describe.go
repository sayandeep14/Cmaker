package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"cmaker/internal/describe"
	"cmaker/internal/llm"
)

// runDescribeAndScaffold turns a natural-language description into a Plan
// (internal/describe.Describe), prints it for review, scaffolds the
// project from it (reusing scaffoldProject exactly as a --template/--lang/
// --with-rust/--with-zig/--target-type invocation would), and installs any
// packages the plan called for.
//
// Unlike `cmaker heal`, this doesn't gate behind a separate --apply step:
// scaffolding into a brand-new (typically empty) directory is inherently
// low-risk and trivially reversible (delete it and try again), unlike
// patching a user's existing source - so printing the plan and then acting
// on it in one command matches how every other `cmaker new` invocation
// already behaves, rather than inventing a new confirmation pattern found
// nowhere else in the CLI.
func runDescribeAndScaffold(root, name, description, compiler, runner string) error {
	client, err := llm.NewClientFromEnv("")
	if err != nil {
		return err
	}

	infof("Asking %s to plan a project for: %q", client.Model, description)
	plan, err := describe.Describe(context.Background(), client, description)
	if err != nil {
		return err
	}

	infof("Plan: template=%s language=%s with_rust=%v with_zig=%v target_type=%s", plan.Template, plan.Language, plan.WithRust, plan.WithZig, plan.TargetType)
	if len(plan.Packages) > 0 {
		infof("Packages: %s", strings.Join(plan.Packages, ", "))
	}
	if plan.Reasoning != "" {
		infof("Reasoning: %s", plan.Reasoning)
	}

	if err := scaffoldProject(root, name, plan.Template, plan.Language, compiler, plan.WithRust, plan.WithZig, runner, plan.TargetType); err != nil {
		return err
	}

	if len(plan.Packages) == 0 {
		return nil
	}

	// runInstall (like every other project-scoped command) operates on the
	// current directory - `cmaker new` scaffolds into a fresh subdirectory,
	// so this process needs to actually be inside it before installing.
	// Fine to do unconditionally: this command's process exits right after,
	// so there's no "caller's cwd" to preserve. `cmaker init` already
	// scaffolds into "." and needs no chdir.
	if root != "." && root != "" {
		if err := os.Chdir(root); err != nil {
			return fmt.Errorf("failed to enter %s to install the planned packages: %w", root, err)
		}
	}
	for _, pkg := range plan.Packages {
		infof("Installing planned package %q...", pkg)
		if err := runInstall(pkg, "", "", nil, nil, false); err != nil {
			warnf("failed to install %q: %v (continuing)", pkg, err)
		}
	}
	return nil
}
