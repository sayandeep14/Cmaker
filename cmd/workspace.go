package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"cmaker/internal/cmake"
	"cmaker/internal/config"
)

// loadWorkspaceMember loads and validates a workspace member's own
// cmaker.yaml - a member is just an ordinary cmaker project, unaware it's
// part of a workspace, so its own cmaker.yaml stays the source of truth for
// its own settings.
func loadWorkspaceMember(memberDir string) (config.Config, error) {
	cfg, err := config.Load(filepath.Join(memberDir, "cmaker.yaml"))
	if err != nil {
		return config.Config{}, fmt.Errorf("workspace member %q: %w", memberDir, err)
	}
	if cfg.Workspace != nil {
		return config.Config{}, fmt.Errorf("workspace member %q: nested workspaces aren't supported", memberDir)
	}
	return cfg, nil
}

// regenerateWorkspace regenerates every member's CMakeLists.txt from its own
// cmaker.yaml, then the workspace root's own CMakeLists.txt (an
// add_subdirectory list) - the multi-project equivalent of syncConfig's
// regenerate-on-every-build behavior for a single project.
func regenerateWorkspace(cfg config.Config) (map[string]config.Config, error) {
	members := make(map[string]config.Config, len(cfg.Workspace.Members))
	for _, m := range cfg.Workspace.Members {
		mcfg, err := loadWorkspaceMember(m)
		if err != nil {
			return nil, err
		}
		if err := cmake.Generate(m, mcfg); err != nil {
			return nil, fmt.Errorf("workspace member %q: failed to write CMakeLists.txt: %w", m, err)
		}
		members[m] = mcfg
	}
	if err := cmake.Generate(".", cfg); err != nil {
		return nil, fmt.Errorf("failed to write workspace root CMakeLists.txt: %w", err)
	}
	return members, nil
}

// runWorkspaceBuild configures and builds the whole workspace as a single
// CMake tree in one './build' directory. With member set, only that
// member's target is built (via 'cmake --build --target <name>'); the
// configure step still covers the whole tree either way, since CMake has no
// per-subdirectory configure.
//
// A workspace's compiler/ccache settings come only from the root
// cmaker.yaml (cfg here), not from individual members - CMake locks in one
// compiler per configure, so members can't each override it independently
// in workspace mode.
func runWorkspaceBuild(cfg config.Config, release bool, jobs int, member string) error {
	members, err := regenerateWorkspace(cfg)
	if err != nil {
		errorf("%v", err)
		os.Exit(1)
	}

	var targetName string
	if member != "" {
		mcfg, ok := members[member]
		if !ok {
			return fmt.Errorf("%q is not a workspace member (see cmaker.yaml's workspace.members: %s)", member, strings.Join(cfg.Workspace.Members, ", "))
		}
		targetName = mcfg.Executable
	}

	buildType := "Debug"
	if release {
		buildType = "Release"
		infof("Optimization: Release Mode ON (-O3)")
	}

	configArgs := append([]string{"-S", ".", "-B", "build", "-DCMAKE_BUILD_TYPE=" + buildType}, cmake.StandardConfigureFlags(cfg)...)
	configArgs = append(configArgs, cmake.CompilerArgs(cfg.Compiler, cfg.Language)...)
	configCmd := exec.Command("cmake", configArgs...)
	configCmd.Stdout, configCmd.Stderr = os.Stdout, os.Stderr
	if err := runWithSpinner("Configuring workspace", configCmd); err != nil {
		return fmt.Errorf("workspace configuration failed: %w", err)
	}

	buildArgs := buildCommandArgs(buildType, jobs)
	if targetName != "" {
		buildArgs = append(buildArgs, "--target", targetName)
	}
	buildExec := exec.Command("cmake", buildArgs...)
	buildExec.Stdout, buildExec.Stderr = os.Stdout, os.Stderr
	if err := runWithSpinner("Building workspace", buildExec); err != nil {
		return fmt.Errorf("workspace compilation failed: %w", err)
	}

	if member != "" {
		okf("%s build successful (member: %s)", buildType, member)
	} else {
		okf("%s workspace build successful (%d member(s))", buildType, len(members))
	}
	return nil
}

// workspaceRunnableBinaryName mirrors runnableBinaryName, but resolves the
// examples/demo.cpp check relative to the member's own directory rather than
// cwd - in workspace mode cwd is the workspace root, not the member.
func workspaceRunnableBinaryName(member string, mcfg config.Config, targetType string) (string, error) {
	if targetType == "executable" {
		return mcfg.Executable, nil
	}
	if _, err := os.Stat(filepath.Join(member, "examples", "demo.cpp")); err != nil {
		return "", fmt.Errorf("workspace member %q is a %s project - there's no executable to run; add %s/examples/demo.cpp to also enable running a demo executable", member, targetType, member)
	}
	return mcfg.Executable + "_demo", nil
}

// runWorkspaceRun builds (if needed, via runWorkspaceBuild) and runs one
// workspace member's executable. Unlike single-project 'cmaker run', member
// selection is mandatory - a workspace has no single default binary to run.
func runWorkspaceRun(cfg config.Config, member string, runnerOverride string, args []string) error {
	if member == "" {
		return fmt.Errorf("workspace mode requires --member=<name> to say which member to run (one of: %s)", strings.Join(cfg.Workspace.Members, ", "))
	}
	if err := runWorkspaceBuild(cfg, false, 0, member); err != nil {
		return err
	}

	mcfg, err := loadWorkspaceMember(member)
	if err != nil {
		return err
	}
	targetType := config.TargetTypeOrDefault(mcfg.TargetType)

	runner := mcfg.Runner
	if runnerOverride != "" {
		runner = runnerOverride
	}
	if runner != "" {
		if targetType != "executable" {
			return fmt.Errorf("'runner' isn't supported for %s projects yet", targetType)
		}
		return runViaRunner(runner, filepath.Join(member, mainSourcePath(mcfg)), args)
	}

	exeName, err := workspaceRunnableBinaryName(member, mcfg, targetType)
	if err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		exeName += ".exe"
	}
	exePath := filepath.Join("build", member, exeName)

	runPath := exePath
	if runtime.GOOS != "windows" {
		runPath = "./" + exePath
	}

	child := exec.Command(runPath, args...)
	child.Stdout, child.Stderr, child.Stdin = os.Stdout, os.Stderr, os.Stdin
	infof("Running %s:\n", exePath)
	if err := child.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			os.Exit(exitErr.ExitCode())
		}
		return err
	}
	return nil
}

// runWorkspaceTest builds the whole workspace and runs ctest across it (or,
// with member set, scoped to just that member's build subdirectory) -
// requires at least one member to have opted into 'testing: { enabled:
// true }', mirroring runTest's same requirement for a single project.
func runWorkspaceTest(cfg config.Config, release bool, member string) error {
	if member != "" && !slices.Contains(cfg.Workspace.Members, member) {
		return fmt.Errorf("%q is not a workspace member (see cmaker.yaml's workspace.members: %s)", member, strings.Join(cfg.Workspace.Members, ", "))
	}

	anyTesting := false
	for _, m := range cfg.Workspace.Members {
		mcfg, err := loadWorkspaceMember(m)
		if err != nil {
			return err
		}
		if mcfg.Testing != nil && mcfg.Testing.Enabled {
			anyTesting = true
		}
	}
	if !anyTesting {
		return fmt.Errorf("no tests configured in any workspace member - set 'testing: { enabled: true }' in a member's cmaker.yaml")
	}

	if err := runWorkspaceBuild(cfg, release, 0, ""); err != nil {
		return err
	}

	testDir := "build"
	if member != "" {
		testDir = filepath.Join("build", member)
	}

	infof("Running ctest...")
	ctestCmd := exec.Command("ctest", "--test-dir", testDir, "--output-on-failure")
	ctestCmd.Stdout, ctestCmd.Stderr = os.Stdout, os.Stderr
	if err := ctestCmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			os.Exit(exitErr.ExitCode())
		}
		return fmt.Errorf("failed to run ctest: %w", err)
	}
	okf("All tests passed!")
	return nil
}
