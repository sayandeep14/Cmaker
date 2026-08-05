package cmd

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"cmaker/internal/config"
)

// addCmd/removeCmd are small parent commands so the surface reads naturally
// as `cmaker add config <name> '<args>'` / `cmaker remove config <name>` -
// only `config` exists as a child today, but the "add/remove X" shape
// leaves room for other saveable project-scoped things later.
var addCmd = &cobra.Command{
	Use:   "add",
	Short: "Add a project-scoped item (currently: config)",
}

var removeCmd = &cobra.Command{
	Use:   "remove",
	Short: "Remove a project-scoped item (currently: config)",
}

var addConfigCmd = &cobra.Command{
	Use:   "config <name> '<cmaker args...>'",
	Short: "Save a named shortcut, run later as 'cmaker <name>'",
	Long:  "Save a named shortcut into cmaker.yaml's configs: map. For example,\n'cmaker add config scratch \"run --only=scratch.cpp\"' lets you later just run\n'cmaker scratch' to do the same thing - a lightweight task runner in the\nspirit of 'npm run <script>' or 'just'.",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		return addNamedConfig(args[0], args[1])
	},
}

var removeConfigCmd = &cobra.Command{
	Use:   "config <name>",
	Short: "Delete a saved named shortcut",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return removeNamedConfig(args[0])
	},
}

var configsCmd = &cobra.Command{
	Use:   "configs",
	Short: "List saved named-config shortcuts for this project",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := loadConfigOrExit()
		if len(cfg.Configs) == 0 {
			infof("No saved configs. Add one with: cmaker add config <name> '<cmaker args...>'")
			return nil
		}
		names := make([]string, 0, len(cfg.Configs))
		for n := range cfg.Configs {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			fmt.Printf("  %s -> cmaker %s\n", colorize(ansiCyan, n), cfg.Configs[n])
		}
		return nil
	},
}

func init() {
	addCmd.AddCommand(addConfigCmd)
	removeCmd.AddCommand(removeConfigCmd)
}

// isReservedCommandName reports whether name collides with a real cmaker
// subcommand (or alias) - checked at *save* time so `cmaker add config
// build '...'` fails immediately with a clear error, instead of the config
// silently never being reachable because `cmaker build` always resolves to
// the built-in command first.
func isReservedCommandName(name string) bool {
	for _, c := range rootCmd.Commands() {
		if c.Name() == name || slices.Contains(c.Aliases, name) {
			return true
		}
	}
	return false
}

func addNamedConfig(name, commandLine string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("config name must not be empty")
	}
	if isReservedCommandName(name) {
		return fmt.Errorf("%q is a built-in cmaker command and can't be used as a config name", name)
	}
	if strings.TrimSpace(commandLine) == "" {
		return fmt.Errorf("config command must not be empty")
	}

	cfg := loadConfigOrExit()
	if cfg.Configs == nil {
		cfg.Configs = map[string]string{}
	}
	cfg.Configs[name] = commandLine
	if err := config.Save("cmaker.yaml", cfg); err != nil {
		return err
	}
	okf("Saved config %q -> cmaker %s (run it with 'cmaker %s')", name, commandLine, name)
	return nil
}

func removeNamedConfig(name string) error {
	cfg := loadConfigOrExit()
	if _, ok := cfg.Configs[name]; !ok {
		return fmt.Errorf("no saved config named %q (see 'cmaker configs')", name)
	}
	delete(cfg.Configs, name)
	if err := config.Save("cmaker.yaml", cfg); err != nil {
		return err
	}
	okf("Removed config %q", name)
	return nil
}
