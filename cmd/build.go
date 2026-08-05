package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"time"

	"github.com/schollz/progressbar/v3"
	"github.com/spf13/cobra"

	"cmaker/internal/cmake"
	"cmaker/internal/logs"
	"cmaker/internal/registry"
)

var buildCmd = &cobra.Command{
	Use:   "build",
	Short: "Configure and build the project",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		release, _ := cmd.Flags().GetBool("release")
		compiler, _ := cmd.Flags().GetString("compiler")
		only, _ := cmd.Flags().GetString("only")
		jobs, _ := cmd.Flags().GetInt("jobs")
		if only != "" {
			return runBuildOnly(only, compiler)
		}
		return runBuild(release, compiler, jobs)
	},
}

func init() {
	buildCmd.Flags().Bool("release", false, "build with CMAKE_BUILD_TYPE=Release (-O3)")
	buildCmd.Flags().String("compiler", "", "override the compiler for this build only (e.g. clang++-17), takes precedence over cmaker.yaml's 'compiler'")
	buildCmd.Flags().String("only", "", "compile a single source file ad hoc (scratch experiments), without wiring it into the main executable")
	buildCmd.Flags().IntP("jobs", "j", 0, "parallel build jobs to pass to 'cmake --build -j' (default: number of CPUs)")
}

// runBuildOnly compiles a single source file ad hoc via compileOnly,
// bypassing the main project's CMake build entirely.
func runBuildOnly(file string, compilerOverride string) error {
	cfg := loadConfigOrExit()
	if compilerOverride != "" {
		cfg.Compiler = compilerOverride
	}
	binPath, err := compileOnly(cfg, file)
	if err != nil {
		return err
	}
	okf("Built %s", binPath)
	return nil
}

func runBuild(release bool, compilerOverride string, jobs int) (err error) {
	cfg := syncConfig()

	// Captures the whole build's combined output (configure + build steps)
	// into one rotating .cmaker/logs/ entry (§24) - the foundation
	// `cmaker heal` reads from. A capture-start failure is logged via -v
	// and otherwise ignored: logging is a diagnostic aid, never a reason
	// to fail an otherwise-working build.
	logSession, logErr := logs.Start(".", "build", cfg.LogsKeep)
	if logErr != nil {
		debugf("log capture: %v", logErr)
	}
	defer func() { logSession.Finish(err) }()

	compiler := cfg.Compiler
	if compilerOverride != "" {
		compiler = compilerOverride
	}
	if err = cmake.ValidateCompilerSupportsStandard(compiler, cfg.Language, cfg.CppVersion, cfg.CVersion); err != nil {
		return err
	}

	buildType := "Debug"
	if release {
		buildType = "Release"
		infof("Optimization: Release Mode ON (-O3)")
	}

	configArgs := append([]string{"-S", ".", "-B", "build", "-DCMAKE_BUILD_TYPE=" + buildType}, cmake.StandardConfigureFlags(cfg)...)
	configArgs = append(configArgs, cmake.CompilerArgs(compiler, cfg.Language)...)
	configCmd := exec.Command("cmake", configArgs...)
	configCmd.Stdout = logSession.Tee(os.Stdout)
	configCmd.Stderr = logSession.Tee(os.Stderr)
	if err = runWithSpinner("Configuring", configCmd); err != nil {
		err = fmt.Errorf("configuration failed: %w", err)
		return err
	}

	// Best-effort: refresh cmaker.lock with whatever CPM actually fetched
	// this configure (§17). A lock-update failure (e.g. a dependency's
	// source dir isn't populated for some reason) shouldn't fail the whole
	// build - the lock just stays stale until the next successful update.
	if len(cfg.Dependencies) > 0 {
		if lockErr := registry.UpdateLockfile(".", "build", cfg); lockErr != nil {
			debugf("cmaker.lock update: %v", lockErr)
		}
	}

	buildExec := exec.Command("cmake", buildCommandArgs(buildType, jobs)...)
	buildExec.Stdout = logSession.Tee(os.Stdout)
	buildExec.Stderr = logSession.Tee(os.Stderr)
	if err = runWithSpinner("Building", buildExec); err != nil {
		err = fmt.Errorf("compilation failed: %w", err)
		return err
	}

	okf("%s build successful!", buildType)
	return nil
}

// buildCommandArgs returns the `cmake --build ...` args, defaulting jobs
// (when <= 0, e.g. unset) to the number of logical CPUs instead of cmake's
// own single-threaded-unless-told-otherwise default - a real, previously
// open build-speed gap (ROADMAP.md §20).
func buildCommandArgs(buildType string, jobs int) []string {
	if jobs <= 0 {
		jobs = runtime.NumCPU()
	}
	return []string{"--build", "build", "--config", buildType, "-j", strconv.Itoa(jobs)}
}

// runWithSpinner runs cmd to completion while an indeterminate spinner ticks,
// so the UI reflects real elapsed work instead of jumping through fake fixed
// percentages. Command output streams straight to stdout/stderr as it runs -
// unless the caller has already set cmd.Stdout/Stderr (e.g. to a log-capture
// tee, see runBuild), in which case that's left alone.
func runWithSpinner(desc string, cmd *exec.Cmd) error {
	if cmd.Stdout == nil {
		cmd.Stdout = os.Stdout
	}
	if cmd.Stderr == nil {
		cmd.Stderr = os.Stderr
	}

	var bar *progressbar.ProgressBar
	if !flagQuiet {
		bar = progressbar.NewOptions(-1,
			progressbar.OptionSetDescription("-- "+desc),
			progressbar.OptionSpinnerType(14),
			progressbar.OptionSetWriter(os.Stderr),
		)
	}

	done := make(chan struct{})
	go func() {
		if bar == nil {
			<-done
			return
		}
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				bar.Add(1)
			}
		}
	}()

	err := cmd.Run()
	close(done)
	if bar != nil {
		bar.Finish()
		fmt.Println()
	}
	return err
}
