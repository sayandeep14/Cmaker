package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"cmaker/internal/audit"
	"cmaker/internal/cmake"
	"cmaker/internal/config"
	"cmaker/internal/registry"
)

var installCmd = &cobra.Command{
	Use:   "install <name>",
	Short: "Add a dependency (from the built-in registry, or --git for any other repo) and fetch it immediately",
	Long: "Looks up <name> in cmaker's built-in package registry (see 'cmaker search'), appends the\n" +
		"resolved dependency to cmaker.yaml, and immediately reconfigures so it's fetched right\n" +
		"away - not silently deferred to the next build. For anything not in the registry, --git\n" +
		"is the escape hatch: any git-hosted library with a real CMakeLists.txt.",
	Example: `  cmaker install fmt
  cmaker install nlohmann-json
  cmaker install mylib --git=https://github.com/me/mylib --tag=v1.0.0 --link=mylib::mylib`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		gitURL, _ := cmd.Flags().GetString("git")
		tag, _ := cmd.Flags().GetString("tag")
		link, _ := cmd.Flags().GetStringSlice("link")
		options, _ := cmd.Flags().GetStringSlice("options")
		downloadOnly, _ := cmd.Flags().GetBool("download-only")
		return runInstall(name, gitURL, tag, link, options, downloadOnly)
	},
}

var uninstallCmd = &cobra.Command{
	Use:   "uninstall <name>",
	Short: "Remove a dependency from cmaker.yaml",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runUninstall(args[0])
	},
}

var listCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"installed"},
	Short:   "List every dependency currently declared in cmaker.yaml",
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		licenses, _ := cmd.Flags().GetBool("licenses")
		return runList(licenses)
	},
}

var searchCmd = &cobra.Command{
	Use:   "search <term>",
	Short: "Search the built-in package registry by name or description",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runSearch(args[0])
	},
}

func init() {
	installCmd.Flags().String("git", "", "install a git-hosted library not in the built-in registry, by URL (requires --tag)")
	installCmd.Flags().String("tag", "", "git tag/branch to fetch (required with --git)")
	installCmd.Flags().StringSlice("link", nil, "CMake target(s) to link, e.g. --link=fmt::fmt (required with --git; comma-separate for multiple)")
	installCmd.Flags().StringSlice("options", nil, "extra CPMAddPackage OPTIONS lines (only with --git)")
	installCmd.Flags().Bool("download-only", false, "fetch source but don't add_subdirectory it (only with --git; see cmaker.yaml's dependencies[].download_only)")
	listCmd.Flags().Bool("licenses", false, "also look up each GitHub-hosted dependency's declared license (network call per dependency, see 'cmaker audit')")
}

// runInstall resolves name to a config.Dependency (from the registry, or
// from --git/--tag/--link/--options), appends it to cmaker.yaml, and
// reconfigures immediately so the fetch happens right away - like `npm
// install`/`cargo add`, not silently deferred to the next build.
func runInstall(name, gitURL, tag string, link, options []string, downloadOnly bool) error {
	cfg := loadConfigOrExit()

	for _, dep := range cfg.Dependencies {
		if strings.EqualFold(dep.Name, name) {
			return fmt.Errorf("%q is already in cmaker.yaml's dependencies (tag %s) - run 'cmaker uninstall %s' first to change it", name, dep.Tag, name)
		}
	}

	var dep config.Dependency
	if gitURL != "" {
		if tag == "" {
			return fmt.Errorf("--tag is required when using --git")
		}
		if len(link) == 0 {
			return fmt.Errorf("--link is required when using --git (which CMake target(s) should be linked?)")
		}
		dep = config.Dependency{Name: name, Repo: gitURL, Tag: tag, Link: link, Options: options, DownloadOnly: downloadOnly}
	} else {
		entry, ok := registry.Find(name)
		if !ok {
			msg := fmt.Sprintf("%q isn't in cmaker's built-in registry (see 'cmaker search <term>')", name)
			if close := registry.CloseMatches(name); len(close) > 0 {
				msg += fmt.Sprintf(" - did you mean: %s?", strings.Join(close, ", "))
			}
			msg += "\nFor a library not in the registry, use --git=<url> --tag=<tag> --link=<target>."
			return fmt.Errorf("%s", msg)
		}
		dep = entry.ToDependency()
	}

	cfg.Dependencies = append(cfg.Dependencies, dep)
	if err := config.Save("cmaker.yaml", cfg); err != nil {
		return fmt.Errorf("failed to update cmaker.yaml: %w", err)
	}
	if err := cmake.Generate(".", cfg); err != nil {
		return fmt.Errorf("failed to write CMakeLists.txt: %w", err)
	}

	infof("Fetching %s (%s@%s)...", dep.Name, dep.Repo, dep.Tag)
	configArgs := append([]string{"-S", ".", "-B", "build"}, cmake.StandardConfigureFlags(cfg)...)
	configArgs = append(configArgs, cmake.CompilerArgs(cfg.Compiler, cfg.Language)...)
	configCmd := exec.Command("cmake", configArgs...)
	configCmd.Stdout = os.Stdout
	configCmd.Stderr = os.Stderr
	if err := configCmd.Run(); err != nil {
		return fmt.Errorf("added %q to cmaker.yaml, but the fetch/configure failed: %w (cmaker.yaml was still updated - fix the issue and run 'cmaker build' to retry)", name, err)
	}

	if err := registry.UpdateLockfile(".", "build", cfg); err != nil {
		debugf("cmaker.lock update: %v", err)
	}

	okf("Installed %s - linked as %s", dep.Name, strings.Join(dep.Link, ", "))
	return nil
}

// runUninstall removes name from cmaker.yaml's dependencies and its
// cmaker.lock entry, and regenerates CMakeLists.txt. It doesn't touch
// build/ - a stale build dir just means the next 'cmaker build' relinks
// without the removed dependency, which cmake handles fine on its own.
func runUninstall(name string) error {
	cfg := loadConfigOrExit()

	idx := -1
	for i, dep := range cfg.Dependencies {
		if strings.EqualFold(dep.Name, name) {
			idx = i
			break
		}
	}
	if idx == -1 {
		return fmt.Errorf("%q is not in cmaker.yaml's dependencies (see 'cmaker list')", name)
	}

	removed := cfg.Dependencies[idx]
	cfg.Dependencies = append(cfg.Dependencies[:idx], cfg.Dependencies[idx+1:]...)

	if err := config.Save("cmaker.yaml", cfg); err != nil {
		return fmt.Errorf("failed to update cmaker.yaml: %w", err)
	}
	if err := cmake.Generate(".", cfg); err != nil {
		return fmt.Errorf("failed to write CMakeLists.txt: %w", err)
	}

	if lf, err := registry.LoadLockfile("."); err == nil {
		if _, ok := lf.Dependencies[removed.Name]; ok {
			delete(lf.Dependencies, removed.Name)
			if err := registry.SaveLockfile(".", lf); err != nil {
				debugf("cmaker.lock update: %v", err)
			}
		}
	}

	okf("Removed %s. Run 'cmaker build' to relink (or 'cmaker clean' first for a fully fresh build).", removed.Name)
	return nil
}

// runList prints every dependency currently declared in cmaker.yaml - the
// read side of install/uninstall, more discoverable than reading raw YAML.
func runList(licenses bool) error {
	cfg := loadConfigOrExit()
	if len(cfg.Dependencies) == 0 {
		infof("No dependencies installed. Try 'cmaker search <term>' or 'cmaker install <name>'.")
		return nil
	}
	for _, dep := range cfg.Dependencies {
		line := fmt.Sprintf("%s (%s@%s) -> %s", dep.Name, dep.Repo, dep.Tag, strings.Join(dep.Link, ", "))
		if licenses {
			license, err := audit.GitHubLicense(context.Background(), dep.Repo)
			switch {
			case err != nil:
				license = "unknown"
			case license == "":
				license = "undetected"
			}
			line += " - license: " + license
		}
		fmt.Println(line)
	}
	return nil
}

// runSearch searches the built-in registry by name/notes and prints
// matches - "how do I even find a JSON library" made discoverable.
func runSearch(term string) error {
	matches := registry.Search(term)
	if len(matches) == 0 {
		infof("No registry matches for %q. See 'cmaker install --git=...' for anything not in the built-in registry.", term)
		return nil
	}
	for _, e := range matches {
		source := ""
		if e.Source != registry.SourceBuiltIn {
			source = fmt.Sprintf(" [%s]", e.Source)
		}
		fmt.Printf("%s%s - %s (%s)\n", e.Name, colorize(ansiYellow, source), e.Notes, e.Repo)
	}
	return nil
}
