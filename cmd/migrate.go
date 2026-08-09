package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"cmaker/internal/agentic"
	"cmaker/internal/cmake"
	"cmaker/internal/config"
	"cmaker/internal/explain"
	"cmaker/internal/heal"
	"cmaker/internal/llm"
)

var migrateCmd = &cobra.Command{
	Use:   "migrate --dependency=<name> --to=<version>",
	Short: "Bump one dependency's pinned version and patch call-sites that changed (LLM-assisted)",
	Long: "Updates <name>'s pinned version (the 'tag:' field) in cmaker.yaml to --to, regenerates\n" +
		"CMakeLists.txt to match, then asks an LLM (Anthropic; requires ANTHROPIC_API_KEY) to find\n" +
		"and patch any call-sites that need to change for compatibility with the new version -\n" +
		"renamed/removed APIs, changed signatures, and similar. A much narrower, more tractable\n" +
		"slice of 'agentic' than 'cmaker codegen': the change set is scoped to one dependency\n" +
		"bump, not an open-ended request.\n" +
		"\n" +
		"Requires a clean git working tree. Rebuilds to verify after applying (retrying against\n" +
		"the build error up to --max-attempts, like 'cmaker codegen'). If no call-sites need\n" +
		"changing at all, the version bump alone is still verified and committed - that's a\n" +
		"valid, complete migration, not a no-op. If you decline a proposed call-site diff, or\n" +
		"every build-fix attempt still fails, EVERYTHING this run changed is reverted, including\n" +
		"the version bump itself - never left half-migrated.",
	Example: `  cmaker migrate --dependency=fmt --to=10.2.1
  cmaker migrate --dependency=nlohmann_json --to=v3.11.3 --deep`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		dependency, _ := cmd.Flags().GetString("dependency")
		to, _ := cmd.Flags().GetString("to")
		deep, _ := cmd.Flags().GetBool("deep")
		plan, _ := cmd.Flags().GetBool("plan")
		model, _ := cmd.Flags().GetString("model")
		maxAttempts, _ := cmd.Flags().GetInt("max-attempts")
		if strings.TrimSpace(dependency) == "" {
			return fmt.Errorf("--dependency is required, e.g. --dependency=fmt")
		}
		if strings.TrimSpace(to) == "" {
			return fmt.Errorf("--to is required, e.g. --to=10.2.1")
		}
		if maxAttempts < 1 {
			return fmt.Errorf("--max-attempts must be at least 1")
		}
		return runMigrate(dependency, to, deep, plan, model, maxAttempts)
	},
}

func init() {
	migrateCmd.Flags().String("dependency", "", "the dependency name, exactly as it appears in cmaker.yaml's dependencies: list (required)")
	migrateCmd.Flags().String("to", "", "the version/tag to migrate to (required)")
	migrateCmd.Flags().Bool("deep", false, "use a stronger model and ask clarifying questions before implementing")
	migrateCmd.Flags().Bool("plan", false, "ask for and confirm a short per-file plan before patching call-sites")
	migrateCmd.Flags().String("model", "", "override the Anthropic model used (default: "+llm.DefaultImproviseModel+", or "+llm.DefaultOpusModel+" with --deep)")
	migrateCmd.Flags().Int("max-attempts", 3, "give up (and revert everything) after this many failed build-fix attempts")
	migrateCmd.MarkFlagRequired("dependency")
	migrateCmd.MarkFlagRequired("to")
}

// runMigrate bumps name's pinned version to to in cmaker.yaml (and
// regenerates CMakeLists.txt to match) as a deterministic first step, not
// gated behind an LLM proposal or confirmation - the same posture as any
// other cmaker.yaml edit (e.g. 'cmaker add config'). Unlike
// runCodegenCore, "the model found no call-sites to change" is treated as
// a valid success (the version bump alone is a complete migration, not a
// no-op to discard) - only an explicit decline or a build that still
// fails after every retry reverts the version bump along with everything
// else. This distinction is exactly why migrate doesn't just call
// runCodegenCore with a seeded touched file: that function's "nothing
// proposed" and "user declined" paths are both plain no-ops, which is
// right for codegen/fix but wrong here.
func runMigrate(name, to string, deep, plan bool, model string, maxAttempts int) error {
	if !isInsideGitRepo(".") {
		return fmt.Errorf("not a git repository - migrate needs one so its changes can be safely checked and committed; run 'git init' first (or scaffold with 'cmaker new'/'init' without --nogit)")
	}
	clean, err := heal.WorkingTreeClean(".")
	if err != nil {
		return err
	}
	if !clean {
		return fmt.Errorf("'cmaker migrate' requires a clean git working tree (uncommitted changes found) - commit or stash first, then retry")
	}

	cfg := loadConfigOrExit()
	depIdx := -1
	for i, d := range cfg.Dependencies {
		if d.Name == name {
			depIdx = i
			break
		}
	}
	if depIdx == -1 {
		return fmt.Errorf("no dependency named %q in cmaker.yaml's dependencies: list", name)
	}
	oldVersion := cfg.Dependencies[depIdx].Tag
	displayOld := oldVersion
	if displayOld == "" {
		displayOld = "(unpinned)"
	}
	if oldVersion == to {
		infof("%s is already pinned to %s.", name, to)
		return nil
	}

	origYAML, err := os.ReadFile("cmaker.yaml")
	if err != nil {
		return fmt.Errorf("failed to read cmaker.yaml: %w", err)
	}
	origCMakeLists, cmakeReadErr := os.ReadFile("CMakeLists.txt")
	hadCMakeLists := cmakeReadErr == nil
	// cmaker.lock isn't touched by this function directly, but runBuild
	// (called below to verify) best-effort refreshes it with whatever CPM
	// actually resolved for the new version - caught live: without also
	// snapshotting/restoring it here, a reverted migration left
	// cmaker.lock still pointing at the failed version while cmaker.yaml
	// went back to the old one, an inconsistent lockfile state.
	origLock, lockReadErr := os.ReadFile("cmaker.lock")
	hadLock := lockReadErr == nil

	cfg.Dependencies[depIdx].Tag = to
	if err := config.Save("cmaker.yaml", cfg); err != nil {
		return fmt.Errorf("failed to save cmaker.yaml: %w", err)
	}
	if err := cmake.Generate(".", cfg); err != nil {
		os.WriteFile("cmaker.yaml", origYAML, 0644)
		return fmt.Errorf("failed to regenerate CMakeLists.txt: %w", err)
	}
	okf("Bumped %s: %s -> %s (cmaker.yaml and CMakeLists.txt updated).", name, displayOld, to)

	revertVersionBump := func() {
		os.WriteFile("cmaker.yaml", origYAML, 0644)
		if hadCMakeLists {
			os.WriteFile("CMakeLists.txt", origCMakeLists, 0644)
		} else {
			os.Remove("CMakeLists.txt")
		}
		if hadLock {
			os.WriteFile("cmaker.lock", origLock, 0644)
		} else {
			os.Remove("cmaker.lock")
		}
	}

	if model == "" {
		if deep {
			model = llm.DefaultOpusModel
		} else {
			model = llm.DefaultImproviseModel
		}
	}
	client, err := llm.NewClientFromEnv(model)
	if err != nil {
		revertVersionBump()
		return err
	}

	candidates, err := explain.WalkSourceFiles(".")
	if err != nil {
		revertVersionBump()
		return err
	}

	findIntent := fmt.Sprintf("Find files that use the %q library/dependency, since it was just upgraded from %s to %s.", name, displayOld, to)
	infof("Asking %s which files use %s...", client.Model, name)
	relevant, err := agentic.SuggestRelevantFiles(context.Background(), client, findIntent, candidates)
	if err != nil {
		revertVersionBump()
		return err
	}

	fixIntent := fmt.Sprintf("The dependency %q was just upgraded from %s to %s in this project. Update any call-sites in the given files that need to change for compatibility with the new version - renamed/removed APIs, changed function signatures, deprecated functions replaced, changed header paths, and similar. If nothing needs to change, make no changes.", name, displayOld, to)

	if deep {
		infof("Asking %s if it has clarifying questions...", client.Model)
		questions, err := agentic.AskClarifyingQuestions(context.Background(), client, fixIntent, candidates)
		if err != nil {
			revertVersionBump()
			return err
		}
		if len(questions) > 0 {
			fixIntent = fixIntent + "\n\n" + collectAnswers(questions)
		}
	}

	filesContent := make(map[string]string, len(relevant))
	for _, f := range relevant {
		if data, err := os.ReadFile(f); err == nil {
			filesContent[f] = string(data)
		}
	}

	if len(filesContent) > 0 {
		if plan {
			infof("Asking %s for a plan...", client.Model)
			planText, err := agentic.AskPlan(context.Background(), client, fixIntent, relevant)
			if err != nil {
				revertVersionBump()
				return err
			}
			fmt.Println(renderMarkdown(planText))
			if !confirmYesNo("Proceed with this plan?") {
				infof("Not proceeding - reverting the version bump too.")
				revertVersionBump()
				return nil
			}
		}

		touched := map[string]*touchedRecord{}
		currentIntent := fixIntent
		noCallSiteChangesNeeded := false

		for attempt := 1; attempt <= maxAttempts; attempt++ {
			if attempt == 1 {
				infof("Asking %s whether any call-sites need updating...", client.Model)
			} else {
				healStatus("Asking %s to fix the build failure (attempt %d/%d)...", client.Model, attempt, maxAttempts)
			}
			proposed, err := agentic.ProposeChanges(context.Background(), client, currentIntent, filesContent)
			if err != nil {
				revertVersionBump()
				revertTouched(touched)
				return err
			}

			if len(proposed) == 0 {
				if attempt == 1 {
					infof("No call-sites need updating for this upgrade.")
					noCallSiteChangesNeeded = true
					break
				}
				warnf("The model didn't propose a fix on attempt %d.", attempt)
			} else {
				changes := buildCodegenFileChanges(proposed, filesContent)
				var combined strings.Builder
				var toApply []fileChange
				for _, c := range changes {
					d := heal.UnifiedDiff(c.path, c.original, c.newContent)
					if d == "" {
						continue
					}
					if combined.Len() > 0 {
						combined.WriteString("\n")
					}
					combined.WriteString(d)
					toApply = append(toApply, c)
				}
				if combined.Len() > 0 {
					printDiff(combined.String())
					if attempt == 1 {
						if !confirmYesNo("Apply these call-site changes?") {
							infof("Not applied - reverting the version bump too.")
							revertVersionBump()
							revertTouched(touched)
							return nil
						}
					}
					for _, c := range toApply {
						if _, ok := touched[c.path]; !ok {
							touched[c.path] = &touchedRecord{original: c.original, isNew: c.isNew}
						}
						if err := writeCodegenFile(c); err != nil {
							revertVersionBump()
							revertTouched(touched)
							return err
						}
						filesContent[c.path] = c.newContent
					}
					okf("Applied.")
				}
			}

			healStatus("Rebuilding to verify...")
			if buildErr := runBuild(false, "", 0, ""); buildErr == nil {
				okf("Build succeeded.")
				return commitMigration(model, name, to)
			}

			if attempt >= maxAttempts {
				warnf("Still failing after %d attempt(s) - reverting everything, including the version bump.", maxAttempts)
				revertVersionBump()
				revertTouched(touched)
				return fmt.Errorf("migrating %s to %s couldn't produce a working build after %d attempt(s); everything was reverted", name, to, maxAttempts)
			}
			currentIntent = fmt.Sprintf("%s\n\nA previous attempt resulted in this build failure - fix it while keeping %s at version %s:\n%s", fixIntent, name, to, latestBuildLogSnippet())
		}

		if !noCallSiteChangesNeeded {
			// The loop above already returned (success, decline, or
			// attempts-exhausted) for every other case.
			return nil
		}
	}

	// Either no files use this dependency, or the model found nothing to
	// change - the version bump alone is the whole migration; verify it
	// still builds before committing it.
	healStatus("Rebuilding to verify the version bump alone...")
	if buildErr := runBuild(false, "", 0, ""); buildErr != nil {
		warnf("The version bump alone doesn't build - reverting.")
		revertVersionBump()
		return fmt.Errorf("migrating %s to %s failed to build with no call-site changes proposed: %w", name, to, buildErr)
	}
	okf("Build succeeded.")
	return commitMigration(model, name, to)
}

// commitMigration stages and commits everything currently in the working
// tree (the version bump, plus any applied call-site changes) as this
// migration's single checkpoint commit.
func commitMigration(model, name, to string) error {
	committed, err := autoCommitStaged(model)
	if err != nil {
		return err
	}
	if !committed {
		infof("Nothing to commit.")
	}
	okf("Migrated %s to %s.", name, to)
	return nil
}
