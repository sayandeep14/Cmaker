// Package cmd wires up cmaker's cobra command tree and the CLI-facing
// wrappers (exit-on-error config loading, colored output, ad hoc compiles)
// around the pure internal/config, internal/cmake, internal/templates, and
// internal/tui packages.
package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

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
		if len(args) > 0 {
			return runNamedConfig(args[0], args[1:])
		}
		if term.IsTerminal(int(os.Stdout.Fd())) {
			return tui.RunTUI()
		}
		return cmd.Help()
	},
}

// runNamedConfig dispatches `cmaker <name>` for a name that didn't match any
// built-in subcommand, by looking it up in cmaker.yaml's configs: map (see
// `cmaker add config`) and re-exec'ing the resolved command line as a child
// process - the same subprocess-reuse pattern the TUI uses to run headless
// subcommands. Extra positional args are forwarded; the saved command line
// itself is split on whitespace only (no shell-quoting support).
func runNamedConfig(name string, extraArgs []string) error {
	cfg := loadConfigOrExit()
	commandLine, ok := cfg.Configs[name]
	if !ok {
		return fmt.Errorf("unknown command %q for \"cmaker\" (no saved config with that name either - see 'cmaker configs')", name)
	}

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
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		errorf("%v", err)
		os.Exit(1)
	}
}
