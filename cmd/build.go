package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/schollz/progressbar/v3"
	"github.com/spf13/cobra"

	"cmaker/internal/cmake"
)

var buildCmd = &cobra.Command{
	Use:   "build",
	Short: "Configure and build the project",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		release, _ := cmd.Flags().GetBool("release")
		compiler, _ := cmd.Flags().GetString("compiler")
		only, _ := cmd.Flags().GetString("only")
		if only != "" {
			return runBuildOnly(only, compiler)
		}
		return runBuild(release, compiler)
	},
}

func init() {
	buildCmd.Flags().Bool("release", false, "build with CMAKE_BUILD_TYPE=Release (-O3)")
	buildCmd.Flags().String("compiler", "", "override the compiler for this build only (e.g. clang++-17), takes precedence over cmaker.yaml's 'compiler'")
	buildCmd.Flags().String("only", "", "compile a single source file ad hoc (scratch experiments), without wiring it into the main executable")
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

func runBuild(release bool, compilerOverride string) error {
	cfg := syncConfig()

	compiler := cfg.Compiler
	if compilerOverride != "" {
		compiler = compilerOverride
	}
	if err := cmake.ValidateCompilerSupportsStandard(compiler, cfg.Language, cfg.CppVersion, cfg.CVersion); err != nil {
		return err
	}

	buildType := "Debug"
	if release {
		buildType = "Release"
		infof("Optimization: Release Mode ON (-O3)")
	}

	configArgs := []string{"-S", ".", "-B", "build", "-DCMAKE_BUILD_TYPE=" + buildType, cmake.PolicyVersionMinFlag}
	configArgs = append(configArgs, cmake.CompilerArgs(compiler, cfg.Language)...)
	configCmd := exec.Command("cmake", configArgs...)
	if err := runWithSpinner("Configuring", configCmd); err != nil {
		return fmt.Errorf("configuration failed: %w", err)
	}

	buildExec := exec.Command("cmake", "--build", "build", "--config", buildType)
	if err := runWithSpinner("Building", buildExec); err != nil {
		return fmt.Errorf("compilation failed: %w", err)
	}

	okf("%s build successful!", buildType)
	return nil
}

// runWithSpinner runs cmd to completion while an indeterminate spinner ticks,
// so the UI reflects real elapsed work instead of jumping through fake fixed
// percentages. Command output streams straight to stdout/stderr as it runs.
func runWithSpinner(desc string, cmd *exec.Cmd) error {
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

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
