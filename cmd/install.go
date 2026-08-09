package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"cmaker/internal/audit"
	"cmaker/internal/cmake"
	"cmaker/internal/config"
	"cmaker/internal/packclient"
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
		remote, _ := cmd.Flags().GetBool("remote")
		return runSearch(args[0], remote)
	},
}

func init() {
	installCmd.Flags().String("git", "", "install a git-hosted library not in the built-in registry, by URL (requires --tag)")
	installCmd.Flags().String("tag", "", "git tag/branch to fetch (required with --git)")
	installCmd.Flags().StringSlice("link", nil, "CMake target(s) to link, e.g. --link=fmt::fmt (required with --git; comma-separate for multiple)")
	installCmd.Flags().StringSlice("options", nil, "extra CPMAddPackage OPTIONS lines (only with --git)")
	searchCmd.Flags().Bool("remote", false, "also search the remote packs registry (requires 'cmaker login')")
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

	var newDeps []config.Dependency
	if gitURL != "" {
		if tag == "" {
			return fmt.Errorf("--tag is required when using --git")
		}
		if len(link) == 0 {
			return fmt.Errorf("--link is required when using --git (which CMake target(s) should be linked?)")
		}
		newDeps = []config.Dependency{{Name: name, Repo: gitURL, Tag: tag, Link: link, Options: options, DownloadOnly: downloadOnly}}
	} else {
		entry, ok := registry.Find(name)
		if !ok {
			// Not a built-in registry entry - try the remote pack
			// registry before giving up (PACKS_PLAN.md's install
			// extension). A pack isn't a CMake dependency at all (no
			// cmaker.yaml/CMakeLists.txt wiring - see runInstallPack's
			// own doc), so a successful pack install returns here
			// directly rather than falling through to the
			// newDeps/cmaker.yaml/cmake-configure logic below.
			installed, err := tryInstallPack(name)
			if err != nil {
				return err
			}
			if installed {
				return nil
			}

			msg := fmt.Sprintf("%q isn't in cmaker's built-in registry or the packs registry (see 'cmaker search <term>' / 'cmaker search <term> --remote')", name)
			bareName, _ := splitPackNameVersion(name)
			var suggestions []string
			suggestions = append(suggestions, registry.CloseMatches(name)...)
			suggestions = append(suggestions, remotePackSuggestions(bareName)...)
			if len(suggestions) > 0 {
				msg += fmt.Sprintf(" - did you mean: %s?", strings.Join(suggestions, ", "))
			}
			msg += "\nFor a library not in the registry, use --git=<url> --tag=<tag> --link=<target>."
			return fmt.Errorf("%s", msg)
		}

		chain, err := resolveInstallChain(entry, cfg)
		if err != nil {
			return err
		}
		for _, e := range chain {
			dep, err := installEntry(e)
			if err != nil {
				return err
			}
			newDeps = append(newDeps, dep)
		}
	}

	cfg.Dependencies = append(cfg.Dependencies, newDeps...)
	if err := config.Save("cmaker.yaml", cfg); err != nil {
		return fmt.Errorf("failed to update cmaker.yaml: %w", err)
	}
	if err := cmake.Generate(".", cfg); err != nil {
		return fmt.Errorf("failed to write CMakeLists.txt: %w", err)
	}

	for _, dep := range newDeps {
		infof("%s", installFetchMessage(dep))
	}
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

	if len(newDeps) == 1 {
		okf("Installed %s - linked as %s", newDeps[0].Name, strings.Join(newDeps[0].Link, ", "))
	} else {
		prereqs := make([]string, len(newDeps)-1)
		for i, d := range newDeps[:len(newDeps)-1] {
			prereqs[i] = d.Name
		}
		last := newDeps[len(newDeps)-1]
		okf("Installed %s (+ prerequisite(s): %s) - linked as %s", last.Name, strings.Join(prereqs, ", "), strings.Join(last.Link, ", "))
	}
	return nil
}

// resolveInstallChain walks entry.Requires (transitively, cycle-checked)
// and returns the full ordered list of registry entries that need
// installing - every prerequisite before the entry that needs it, e.g.
// [asio, Crow] for `cmaker install crow`. Anything already present in
// cfg.Dependencies is silently skipped rather than erroring, since a
// shared prerequisite (asio, glfw) may already be there from an earlier
// unrelated install.
// registryFind is registry.Find by default - a package var so tests can
// swap in a fake lookup (e.g. to exercise resolveInstallChain's cycle
// detection with synthetic entries the real built-in registry, correctly,
// has none of).
var registryFind = registry.Find

func resolveInstallChain(entry registry.Entry, cfg config.Config) ([]registry.Entry, error) {
	alreadyInstalled := make(map[string]bool, len(cfg.Dependencies))
	for _, d := range cfg.Dependencies {
		alreadyInstalled[strings.ToLower(d.Name)] = true
	}

	var chain []registry.Entry
	inChain := map[string]bool{}
	var visit func(e registry.Entry, visiting map[string]bool) error
	visit = func(e registry.Entry, visiting map[string]bool) error {
		key := strings.ToLower(e.Name)
		if alreadyInstalled[key] || inChain[key] {
			return nil
		}
		if visiting[key] {
			return fmt.Errorf("circular 'requires' relationship in the registry involving %q", e.Name)
		}
		visiting[key] = true
		for _, reqName := range e.Requires {
			reqEntry, ok := registryFind(reqName)
			if !ok {
				return fmt.Errorf("%q requires %q, which isn't in the registry (internal registry data error - please report this)", e.Name, reqName)
			}
			if err := visit(reqEntry, visiting); err != nil {
				return err
			}
		}
		inChain[key] = true
		chain = append(chain, e)
		return nil
	}
	if err := visit(entry, map[string]bool{}); err != nil {
		return nil, err
	}
	return chain, nil
}

// installEntry performs whatever side effect entry.Kind requires (a real
// package-manager install for system_package; resolving the current
// platform's URL for prebuilt_archive; nothing yet for cpm, which only
// fetches at the next configure) and returns the resulting config.Dependency.
func installEntry(entry registry.Entry) (config.Dependency, error) {
	switch entry.Kind {
	case registry.KindSystemPackage, registry.KindPkgConfig:
		// Runs and must succeed *before* cmaker.yaml is touched -
		// find_package(...)/pkg_check_modules(...) at the next configure
		// will fail outright if the package manager install didn't
		// actually happen.
		if err := installSystemPackage(entry.Name, entry.PackageManagers); err != nil {
			return config.Dependency{}, err
		}
		return entry.ToDependency(), nil
	case registry.KindPrebuiltArchive:
		url, err := entry.ResolveArchiveURL(runtime.GOOS, runtime.GOARCH)
		if err != nil {
			return config.Dependency{}, err
		}
		return entry.ToDependencyForArchive(url), nil
	default: // registry.KindCPM
		return entry.ToDependency(), nil
	}
}

// installFetchMessage returns the "Fetching..." status line, adapted to
// dep.Kind - a cpm dependency has a real repo/tag to name, but
// system_package/prebuilt_archive dependencies don't (the former is
// already installed by this point; the latter's real fetch is about to
// happen inside the cmake configure that follows, not before it).
func installFetchMessage(dep config.Dependency) string {
	switch dep.KindOrDefault() {
	case config.DependencyKindSystemPackage:
		return fmt.Sprintf("Wiring in %s via find_package(%s)...", dep.Name, dep.FindPackage)
	case config.DependencyKindPkgConfig:
		return fmt.Sprintf("Wiring in %s via pkg-config (%s)...", dep.Name, dep.PkgConfigModule)
	case config.DependencyKindPrebuiltArchive:
		return fmt.Sprintf("Downloading %s from %s...", dep.Name, dep.ArchiveURL)
	default:
		return fmt.Sprintf("Fetching %s (%s@%s)...", dep.Name, dep.Repo, dep.Tag)
	}
}

// installSystemPackage shells out to whichever of entry.PackageManagers is
// actually available on this machine (checked in a fixed preference order),
// streaming the real install output to the terminal exactly like every
// other cmaker command that shells out to a subprocess. brew (macOS), apt
// (Debian/Ubuntu), and pacman (Windows, via MSYS2 - see registry.Entry's own
// PackageManagers doc for why it's pacman rather than choco/winget: ABI
// compatibility with the MinGW-w64/UCRT compiler the Windows installer sets
// up) are supported; vcpkg is still a documented, not-yet-implemented
// follow-up (§27).
func installSystemPackage(name string, packageManagers map[string]string) error {
	type manager struct {
		id          string
		lookup      string   // binary to check via exec.LookPath
		command     []string // argv prefix, package name appended
		windowsOnly bool     // never attempted outside GOOS=="windows", even if lookup happens to resolve (e.g. a real Arch Linux pacman)
	}
	managers := []manager{
		{id: "brew", lookup: "brew", command: []string{"brew", "install"}},
		{id: "apt", lookup: "apt-get", command: []string{"sudo", "apt-get", "install", "-y"}},
		{id: "pacman", lookup: "pacman", command: []string{"pacman", "-S", "--noconfirm"}, windowsOnly: true},
	}

	for _, m := range managers {
		pkg, ok := packageManagers[m.id]
		if !ok {
			continue
		}
		if m.windowsOnly && runtime.GOOS != "windows" {
			continue
		}
		if _, err := exec.LookPath(m.lookup); err != nil {
			continue
		}
		infof("Installing %s via %s (%s)...", name, m.id, pkg)
		installCmd := exec.Command(m.command[0], append(m.command[1:], pkg)...)
		installCmd.Stdout = os.Stdout
		installCmd.Stderr = os.Stderr
		if err := installCmd.Run(); err != nil {
			return fmt.Errorf("%s install %s failed: %w", m.id, pkg, err)
		}
		return nil
	}

	tried := make([]string, 0, len(managers))
	for _, m := range managers {
		if _, ok := packageManagers[m.id]; ok {
			tried = append(tried, m.id)
		}
	}
	if len(tried) == 0 {
		return fmt.Errorf("%q has no known package manager entry for %s", name, runtime.GOOS)
	}
	return fmt.Errorf("%q needs one of these package managers on PATH: %s (none found)", name, strings.Join(tried, ", "))
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
		var line string
		switch dep.KindOrDefault() {
		case config.DependencyKindSystemPackage:
			line = fmt.Sprintf("%s (system package, find_package(%s)) -> %s", dep.Name, dep.FindPackage, strings.Join(dep.Link, ", "))
		case config.DependencyKindPkgConfig:
			line = fmt.Sprintf("%s (system package, pkg-config %s) -> %s", dep.Name, dep.PkgConfigModule, strings.Join(dep.Link, ", "))
		case config.DependencyKindPrebuiltArchive:
			line = fmt.Sprintf("%s (prebuilt archive: %s) -> %s", dep.Name, dep.ArchiveURL, strings.Join(dep.Link, ", "))
		default:
			line = fmt.Sprintf("%s (%s@%s) -> %s", dep.Name, dep.Repo, dep.Tag, strings.Join(dep.Link, ", "))
		}
		if licenses {
			// Only a cpm dependency has a GitHub repo to look a license up
			// for - system_package/prebuilt_archive dependencies aren't
			// fetched from a repo cmaker knows about at all.
			if dep.KindOrDefault() == config.DependencyKindCPM {
				license, err := audit.GitHubLicense(context.Background(), dep.Repo)
				switch {
				case err != nil:
					license = "unknown"
				case license == "":
					license = "undetected"
				}
				line += " - license: " + license
			} else {
				line += " - license: n/a"
			}
		}
		fmt.Println(line)
	}
	return nil
}

// runSearch searches the built-in registry by name/notes and prints
// matches - "how do I even find a JSON library" made discoverable.
func runSearch(term string, remote bool) error {
	matches := registry.Search(term)
	if len(matches) == 0 {
		infof("No registry matches for %q. See 'cmaker install --git=...' for anything not in the built-in registry.", term)
	} else {
		for _, e := range matches {
			source := ""
			if e.Source != registry.SourceBuiltIn {
				source = fmt.Sprintf(" [%s]", e.Source)
			}
			fmt.Printf("%s%s - %s (%s)\n", e.Name, colorize(ansiYellow, source), e.Notes, e.Repo)
		}
	}

	if remote {
		if err := runSearchRemote(term); err != nil {
			warnf("remote search failed: %v", err)
		}
	}
	return nil
}

// runSearchRemote queries the packs registry (see 'cmaker search --remote')
// - a separate flag-gated step, not folded into the default search,
// specifically to keep "the built-in curated registry" and "anyone's
// uploaded pack" from being confused with each other (see PACKS_PLAN.md's
// own note on this).
func runSearchRemote(term string) error {
	token, _, err := packclient.LoadCredentials()
	if err != nil {
		return err
	}
	if token == "" {
		return fmt.Errorf("not logged in - run 'cmaker login' first")
	}

	client := packclient.NewClient("", token)
	results, err := client.Search(context.Background(), term)
	if err != nil {
		return err
	}
	if len(results) == 0 {
		infof("No packs registry matches for %q.", term)
		return nil
	}
	fmt.Println(colorize(ansiBold, "Packs registry (see 'cmaker install <name>'):"))
	for _, r := range results {
		fmt.Printf("%s - %s\n", r.Name, r.Description)
	}
	return nil
}
