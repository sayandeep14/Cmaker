// Package cmd wires up cmaker's cobra command tree and the CLI-facing
// wrappers (exit-on-error config loading, colored output, ad hoc compiles)
// around the pure internal/config, internal/cmake, internal/templates, and
// internal/tui packages.
package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"cmaker/internal/config"
	"cmaker/internal/tui"
)

// Global flags shared by every subcommand.
var (
	flagVerbose bool
	flagQuiet   bool
	flagNoColor bool
)

var rootCmd = &cobra.Command{
	Use:           "cmaker",
	Short:         "cmaker scaffolds, configures, and builds CMake-based C++ projects",
	Long:          "cmaker is a small CLI that scaffolds CMake-based C++ projects from templates,\nkeeps CMakeLists.txt in sync with a cmaker.yaml config, and wraps the\nconfigure/build/run loop behind a single command.",
	SilenceUsage:  true,
	SilenceErrors: true,
	Args:          cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Reaching here with args[0] set at all would mean
		// maybeDispatchFallback (see Execute) somehow didn't already
		// intercept an unrecognized first argument - it always should,
		// since it's checked first and covers exactly this case, so this
		// is just a defensive fallback, not the primary path.
		if len(args) > 0 {
			return runNamedConfig(args[0], args[1:])
		}
		if term.IsTerminal(int(os.Stdout.Fd())) {
			return tui.RunTUI()
		}
		return cmd.Help()
	},
}

// runNamedConfig dispatches `cmaker <name>` for a name that isn't a
// built-in subcommand. Two fallbacks are tried in order, so something the
// user explicitly configured always wins over the generic git forward:
//  1. cmaker.yaml's configs: map (see `cmaker add config`) - re-exec'd as a
//     child process, the same subprocess-reuse pattern the TUI uses to run
//     headless subcommands.
//  2. If name is a recognized git subcommand (knownGitVerbs - a fixed
//     allowlist, not "anything"), forward straight to `git <name> ...`.
//
// Uses config.TryLoadConfigs (best-effort, never exits) rather than
// loadConfigOrExit - unlike every other cmaker command, this fallback path
// needs to work even outside a cmaker project entirely (e.g. `cmaker pull`
// in a plain git repo with no cmaker.yaml at all), so a missing/invalid
// cmaker.yaml just means "no saved configs to check," not a hard failure.
func runNamedConfig(name string, extraArgs []string) error {
	if commandLine, ok := config.TryLoadConfigs(".")[name]; ok {
		fields := strings.Fields(commandLine)
		fields = append(fields, extraArgs...)

		infof("Running saved config %q: cmaker %s", name, strings.Join(fields, " "))
		child := exec.Command(os.Args[0], fields...)
		child.Stdout, child.Stderr, child.Stdin = os.Stdout, os.Stderr, os.Stdin
		if err := child.Run(); err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				os.Exit(exitErr.ExitCode())
			}
			return err
		}
		return nil
	}

	if knownGitVerbs[name] {
		return runGitPassthrough(append([]string{name}, extraArgs...))
	}

	return fmt.Errorf("unknown command %q for \"cmaker\" (no saved config with that name either - see 'cmaker configs')", name)
}

// maybeDispatchFallback intercepts `cmaker <name> ...` before cobra ever
// parses os.Args, when <name> isn't one of cobra's own registered
// top-level command names - the same reason maybeDispatchGitOverride
// bypasses cobra for --git: cobra parses flags against whichever command
// it resolves to using *that command's own* defined flags, and neither
// rootCmd nor any built-in subcommand knows about e.g. git log's
// `--oneline`. Caught live: `cmaker log --oneline` failed with "unknown
// flag: --oneline" before this existed, since cobra rejected it while
// still trying to resolve rootCmd's own flags, never even reaching
// runNamedConfig's git-forwarding logic. Routing around cobra entirely
// for an unrecognized command name fixes it for every git verb's flags at
// once, not just --oneline specifically.
func maybeDispatchFallback(args []string) (handled bool, err error) {
	if len(args) == 0 || isRegisteredCommandName(args[0]) {
		return false, nil
	}
	// A flag (--help, --version, -v, ...) or cobra's own implicit "help"
	// pseudo-command must always fall through to cobra's normal dispatch -
	// "help" in particular isn't in rootCmd.Commands() yet at this point
	// (cobra registers it lazily inside Execute() itself), so without this
	// check it would look exactly like an unrecognized command name and
	// get swallowed here instead of ever reaching cobra's real help
	// system. Caught live: `cmaker --help` and `cmaker help <cmd>` were
	// both completely broken before this check existed.
	if args[0] == "help" || strings.HasPrefix(args[0], "-") {
		return false, nil
	}
	return true, runNamedConfig(args[0], args[1:])
}

// isRegisteredCommandName reports whether name is a built-in cmaker
// subcommand (or one of its aliases) - if so, maybeDispatchFallback backs
// off and lets cobra's normal dispatch (and flag parsing) handle it as
// always.
func isRegisteredCommandName(name string) bool {
	for _, c := range rootCmd.Commands() {
		if c.Name() == name || slices.Contains(c.Aliases, name) {
			return true
		}
	}
	return false
}

// knownGitVerbs is the fixed set of git subcommands bare `cmaker <verb>`
// transparently forwards to `git <verb>` when <verb> isn't a built-in
// cmaker command or a saved named config - deliberately an allowlist, not
// "anything unrecognized," so a genuine typo in a cmaker command/config
// name still gets the clear "unknown command" error above instead of a
// confusing forward to git. init/add/clean are deliberately excluded -
// those three already have well-established cmaker meanings (scaffold,
// named-config management, wipe build/); use `cmaker git init`/`cmaker
// init --git` etc. for git's version of them instead (see gitCmd and
// maybeDispatchGitOverride in git.go). push is also excluded - it's a
// real registered command (see push.go) so it can offer --auto.
var knownGitVerbs = map[string]bool{
	"pull": true, "fetch": true, "clone": true,
	"status": true, "branch": true, "checkout": true, "switch": true,
	"merge": true, "rebase": true, "reset": true, "revert": true,
	"tag": true, "remote": true, "stash": true, "show": true,
	"blame": true, "cherry-pick": true, "rm": true, "mv": true,
	"config": true, "describe": true, "submodule": true, "log": true,
	"diff": true, "restore": true, "worktree": true, "reflog": true,
	"bisect": true,
}

var tuiCmd = &cobra.Command{
	Use:   "tui",
	Short: "Launch the interactive dashboard explicitly",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return tui.RunTUI()
	},
}

func init() {
	rootCmd.PersistentFlags().BoolVarP(&flagVerbose, "verbose", "v", false, "print extra diagnostic output")
	rootCmd.PersistentFlags().BoolVarP(&flagQuiet, "quiet", "q", false, "suppress non-error output")
	rootCmd.PersistentFlags().BoolVar(&flagNoColor, "no-color", false, "disable colored output")

	rootCmd.AddCommand(newCmd)
	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(createCmd)
	rootCmd.AddCommand(templatesCmd)
	rootCmd.AddCommand(buildCmd)
	rootCmd.AddCommand(runCmd)
	rootCmd.AddCommand(testCmd)
	rootCmd.AddCommand(cleanCmd)
	rootCmd.AddCommand(doctorCmd)
	rootCmd.AddCommand(watchCmd)
	rootCmd.AddCommand(tuiCmd)
	rootCmd.AddCommand(addCmd)
	rootCmd.AddCommand(removeCmd)
	rootCmd.AddCommand(configsCmd)
	rootCmd.AddCommand(generateCmd)
	rootCmd.AddCommand(installCmd)
	rootCmd.AddCommand(uninstallCmd)
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(searchCmd)
	rootCmd.AddCommand(fmtCmd)
	rootCmd.AddCommand(lintCmd)
	rootCmd.AddCommand(auditCmd)
	rootCmd.AddCommand(logsCmd)
	rootCmd.AddCommand(healCmd)
	rootCmd.AddCommand(explainCmd)
	rootCmd.AddCommand(readCmd)
	rootCmd.AddCommand(improveCmd)
	rootCmd.AddCommand(suggestCmd)
	rootCmd.AddCommand(commitCmd)
	rootCmd.AddCommand(gitCmd)
	rootCmd.AddCommand(pushCmd)
	rootCmd.AddCommand(dummygitCmd)
	rootCmd.AddCommand(newBranchCmd)
	rootCmd.AddCommand(editCmd)
	rootCmd.AddCommand(codegenCmd)
	rootCmd.AddCommand(reviewCmd)
	rootCmd.AddCommand(fixCmd)
	rootCmd.AddCommand(migrateCmd)
	rootCmd.AddCommand(documentCmd)
	rootCmd.AddCommand(benchCmd)
	rootCmd.AddCommand(coverageCmd)
	rootCmd.AddCommand(docsCmd)
}

// SetVersion sets the version string cobra reports for the auto-generated
// `--version` flag (and `cmaker version`). Called from main.go with a value
// baked in at build time via -ldflags.
func SetVersion(v string) {
	rootCmd.Version = v
}

// Execute runs the root command, printing a colored error and exiting
// non-zero on failure. This is cmaker's single exported entry point,
// called from main.go.
//
// maybeDispatchGitOverride and maybeDispatchFallback are both checked
// before cobra ever parses os.Args - see their own docs for why
// `cmaker init/add/clean --git ...` and `cmaker <git-verb> <git-flags>`
// can't go through cobra's normal flag parsing.
func Execute() {
	if handled, err := maybeDispatchGitOverride(os.Args[1:]); handled {
		if err != nil {
			errorf("%v", err)
			os.Exit(1)
		}
		return
	}

	if handled, err := maybeDispatchFallback(os.Args[1:]); handled {
		if err != nil {
			errorf("%v", err)
			os.Exit(1)
		}
		return
	}

	if err := rootCmd.Execute(); err != nil {
		errorf("%v", err)
		os.Exit(1)
	}
}
