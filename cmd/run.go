package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"cmaker/internal/config"
	"cmaker/internal/logs"
)

var runCmd = &cobra.Command{
	Use:   "run [-- args...]",
	Short: "Build (if needed) and run the project executable",
	Args:  cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		only, _ := cmd.Flags().GetString("only")
		compiler, _ := cmd.Flags().GetString("compiler")
		runner, _ := cmd.Flags().GetString("runner")
		member, _ := cmd.Flags().GetString("member")
		if only != "" {
			return runOnlyFile(only, compiler, runner, args)
		}
		return runProject(runner, member, args)
	},
}

func init() {
	runCmd.Flags().String("only", "", "compile and run a source file (or glob, e.g. 'tests/*.cpp') ad hoc, without wiring it into the main executable")
	runCmd.Flags().String("compiler", "", "compiler to use for --only, overriding cmaker.yaml's 'compiler'")
	runCmd.Flags().String("runner", "", "custom program to invoke as '<runner> <file>' instead of compiling then running (e.g. crun), overriding cmaker.yaml's 'runner' - applies to --only and to a whole project's 'cmaker run'")
	runCmd.Flags().String("member", "", "workspace root only: which member to run (required in workspace mode, see cmaker.yaml's 'workspace.members')")
}

// runOnlyFile runs every source file matching file (a plain path or a glob
// like 'tests/*.cpp') ad hoc. If a runner is configured (via --runner or
// cmaker.yaml's 'runner'), it takes priority: the whole compile-then-run
// flow is skipped in favor of directly invoking `<runner> <file> [args...]`
// per matched file, since tools like crun compile and run in a single step
// and don't produce a separate binary path cmaker could exec itself.
// Otherwise, falls back to compiling each match via compileOnly and running
// the resulting scratch binaries, forwarding args after the `--` separator
// to each.
//
// A single match preserves the original exact behavior (the child's own
// exit code is propagated via os.Exit, unchanged since before glob support
// existed). Multiple matches run all of them, aggregate pass/fail (rather
// than stopping at the first failure - the point of a glob like
// 'tests/*.cpp' is seeing every result, the same way a real test runner
// would), and exit 1 overall if any failed - a specific child's exact exit
// code doesn't generalize across more than one child, so this only ever
// reports a real failure occurred, not which one.
func runOnlyFile(file string, compilerOverride string, runnerOverride string, args []string) error {
	cfg := loadConfigOrExit()
	if compilerOverride != "" {
		cfg.Compiler = compilerOverride
	}
	runner := cfg.Runner
	if runnerOverride != "" {
		runner = runnerOverride
	}

	if runner != "" {
		files, err := resolveOnlyFiles(file)
		if err != nil {
			return err
		}
		if len(files) == 1 {
			return runViaRunner(runner, files[0], args)
		}
		return runManyViaRunner(runner, files, args)
	}

	bins, err := compileOnly(cfg, file)
	if err != nil {
		return err
	}
	if len(bins) == 1 {
		return runOneBinaryExact(bins[0], args)
	}
	return runManyBinaries(bins, args)
}

// runOneBinaryExact runs binPath, streaming stdout/stderr/stdin straight
// through and propagating its exact exit code via os.Exit - the original
// single-file '--only' run behavior, unchanged, extracted so the new
// multi-match path (runManyBinaries) can sit alongside it without altering
// this one.
func runOneBinaryExact(binPath string, args []string) error {
	child := exec.Command(binPath, args...)
	child.Stdout, child.Stderr, child.Stdin = os.Stdout, os.Stderr, os.Stdin
	infof("Running %s:\n", binPath)
	if err := child.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			os.Exit(exitErr.ExitCode())
		}
		return err
	}
	return nil
}

// runManyBinaries runs every binary in bins in sequence, streaming each
// one's output and reporting a pass/fail summary at the end - exits 1 if
// any failed, matching '--only=<glob>' being a lightweight ad hoc test
// runner, not just a batch compiler.
func runManyBinaries(bins []string, args []string) error {
	var failed []string
	for _, bin := range bins {
		infof("--- Running %s ---", bin)
		child := exec.Command(bin, args...)
		child.Stdout, child.Stderr, child.Stdin = os.Stdout, os.Stderr, os.Stdin
		if err := child.Run(); err != nil {
			failed = append(failed, bin)
		}
	}
	return reportOnlyGlobResult(len(bins), failed)
}

// runManyViaRunner is runManyBinaries' equivalent for a configured runner
// (--runner/cmaker.yaml's 'runner') - each matched file is passed to
// `<runner> <file> [args...]` in turn instead of being compiled first.
func runManyViaRunner(runner string, files []string, args []string) error {
	var failed []string
	for _, f := range files {
		infof("--- %s %s ---", runner, f)
		parts := append([]string{runner, shellQuote(f)}, quoteAll(args)...)
		child := loginShellCommand(strings.Join(parts, " "))
		child.Stdout, child.Stderr, child.Stdin = os.Stdout, os.Stderr, os.Stdin
		if err := child.Run(); err != nil {
			failed = append(failed, f)
		}
	}
	return reportOnlyGlobResult(len(files), failed)
}

// reportOnlyGlobResult prints a pass/fail summary for a multi-match
// '--only=<glob>' run and exits 1 if anything failed - shared by
// runManyBinaries/runManyViaRunner so both report identically.
func reportOnlyGlobResult(total int, failed []string) error {
	if len(failed) == 0 {
		okf("%d/%d passed", total, total)
		return nil
	}
	errorf("%d/%d passed - failed: %s", total-len(failed), total, strings.Join(failed, ", "))
	os.Exit(1)
	return nil
}

// runViaRunner invokes a custom compile-and-run tool (e.g. crun) on file,
// forwarding args after it and streaming stdout/stderr/stdin through to the
// terminal exactly like the default compile-then-run path.
//
// This goes through the user's login shell in interactive mode (see
// loginShellCommand) rather than exec'ing runner directly, since tools like
// crun are often shell functions/aliases defined in ~/.zshrc or ~/.bashrc (or,
// on Windows, a PowerShell $PROFILE function) - a plain os/exec.Command can
// only launch real executables on PATH, so it can't see those. Running
// through the shell also means real executables on PATH continue to work
// exactly as before.
func runViaRunner(runner, file string, args []string) error {
	if _, err := os.Stat(file); err != nil {
		return fmt.Errorf("--only file %q not found: %w", file, err)
	}

	parts := append([]string{runner, shellQuote(file)}, quoteAll(args)...)
	cmdLine := strings.Join(parts, " ")

	child := loginShellCommand(cmdLine)
	child.Stdout, child.Stderr, child.Stdin = os.Stdout, os.Stderr, os.Stdin
	infof("Running %s %s via custom runner (%s)...\n", runner, file, child.Path)
	if err := child.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			os.Exit(exitErr.ExitCode())
		}
		return err
	}
	return nil
}

// loginShellCommand builds the *exec.Cmd that runs cmdLine through the
// user's own interactive login shell, so shell functions/aliases defined in
// its rc/profile (crun-style runners chief among them - see runViaRunner's
// own doc) are visible, not just real PATH executables. On Windows there's
// no $SHELL/rc-file convention - the equivalent is PowerShell loading the
// user's $PROFILE, which happens by default unless -NoProfile is passed (so
// it deliberately isn't, here, to preserve that parity).
func loginShellCommand(cmdLine string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		return exec.Command("powershell", "-NoLogo", "-Command", cmdLine)
	}
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	return exec.Command(shell, "-i", "-c", cmdLine)
}

// shellQuote wraps s in single quotes so it survives being interpolated into
// a shell -c command line unchanged. The escaping for an embedded single
// quote differs by shell: POSIX shells have no in-quote escape sequence at
// all, so the standard trick is to close the quoted string, insert a
// backslash-escaped literal quote outside it, then reopen the quoted string
// - while PowerShell's single-quoted strings escape an embedded quote simply
// by repeating it.
func shellQuote(s string) string {
	if runtime.GOOS == "windows" {
		return "'" + strings.ReplaceAll(s, "'", "''") + "'"
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func quoteAll(args []string) []string {
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = shellQuote(a)
	}
	return quoted
}

func runProject(runnerOverride string, member string, args []string) error {
	peek := loadConfigOrExit()
	if peek.Workspace != nil {
		return runWorkspaceRun(peek, member, runnerOverride, args)
	}
	if member != "" {
		return fmt.Errorf("--member is only valid for a workspace root cmaker.yaml (see 'workspace:' in cmaker.yaml)")
	}

	cfg := syncConfig()
	targetType := config.TargetTypeOrDefault(cfg.TargetType)

	runner := cfg.Runner
	if runnerOverride != "" {
		runner = runnerOverride
	}
	if runner != "" {
		if targetType != "executable" {
			return fmt.Errorf("'runner' isn't supported for %s projects yet", targetType)
		}
		return runViaRunner(runner, mainSourcePath(cfg), args)
	}

	exeName, err := runnableBinaryName(cfg, targetType)
	if err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		exeName += ".exe"
	}
	exePath := filepath.Join("build", exeName)

	if isBuildRequired(exePath) {
		infof("Changes detected. Rebuilding...")
		if err := runBuild(false, "", 0, ""); err != nil {
			return err
		}
	}

	runPath := exePath
	if runtime.GOOS != "windows" {
		runPath = "./" + exePath
	}

	// Captures the executable's own run output (§24) as a "run" log,
	// separate from the "build" log runBuild independently captures above -
	// a runtime crash is exactly the kind of failure 'cmaker heal' should
	// be able to read from too.
	logSession, logErr := logs.Start(".", "run", cfg.LogsKeep)
	if logErr != nil {
		debugf("log capture: %v", logErr)
	}

	child := exec.Command(runPath, args...)
	child.Stdout = logSession.Tee(os.Stdout)
	child.Stderr = logSession.Tee(os.Stderr)
	child.Stdin = os.Stdin
	infof("Running %s:\n", exePath)
	runErr := child.Run()
	logSession.Finish(runErr)
	if runErr != nil {
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			os.Exit(exitErr.ExitCode())
		}
		return runErr
	}
	return nil
}

// runnableBinaryName returns the name of the binary 'cmaker run' should
// build and execute. A plain executable project runs cfg.Executable itself.
// A library project has no executable of its own - the closest thing is
// its examples/demo.cpp demo consumer (see internal/cmake.Generate and
// cmd/new.go's writeLibrarySources), built as "<executable>_demo"; if that
// file doesn't exist, there's nothing for 'cmaker run' to build and run.
func runnableBinaryName(cfg config.Config, targetType string) (string, error) {
	if targetType == "executable" {
		return cfg.Executable, nil
	}
	if _, err := os.Stat(filepath.Join("examples", "demo.cpp")); err != nil {
		return "", fmt.Errorf("this is a %s project - there's no executable to run (use 'cmaker build' instead); add examples/demo.cpp to also enable 'cmaker run' via a demo executable", targetType)
	}
	return cfg.Executable + "_demo", nil
}

// mainSourcePath returns the entry source file cmaker itself scaffolds for a
// project (see scaffoldProject in new.go) - used when a 'runner' is
// configured for the whole project, since a tool like crun runs a single
// source file directly rather than a built executable.
func mainSourcePath(cfg config.Config) string {
	if config.LanguageOrDefault(cfg.Language) == "c" {
		return filepath.Join("src", "main.c")
	}
	return filepath.Join("src", "main.cpp")
}

// isBuildRequired reports whether any tracked source file is newer than the
// existing binary. It skips build/ and .git/ so it doesn't walk generated
// or VCS metadata trees on every invocation.
func isBuildRequired(binaryPath string) bool {
	info, err := os.Stat(binaryPath)
	if err != nil {
		return true
	}

	modTime := info.ModTime()
	foundNewer := false
	filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() && (info.Name() == "build" || info.Name() == ".git") {
			return filepath.SkipDir
		}
		ext := filepath.Ext(path)
		if !info.IsDir() && (ext == ".cpp" || ext == ".cxx" || ext == ".c" || ext == ".h" || ext == ".hpp" || path == "cmaker.yaml") {
			if info.ModTime().After(modTime) {
				foundNewer = true
			}
		}
		return nil
	})
	return foundNewer
}
