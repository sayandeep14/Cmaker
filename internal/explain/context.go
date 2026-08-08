package explain

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"cmaker/internal/heal"
	"cmaker/internal/logs"
	"cmaker/internal/registry"
)

// maxLogChars mirrors internal/heal's own bound (see heal.go) - the tail of
// a failing log is what matters, and keeping the request scoped keeps it
// cheap.
const maxLogChars = 6000

// maxReferencedFiles mirrors internal/heal's own bound - explain lastError
// scopes to what the compiler actually pointed at, not the whole project.
const maxReferencedFiles = 5

// BuildClassPrompt turns one FindClass match into the user-content string
// for Ask.
func BuildClassPrompt(className string, m ClassMatch) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Explain the class/struct %q defined in %s:\n\n%s\n", className, m.File, m.Body)
	return b.String()
}

// BuildFunctionPrompt turns one FindFunction match into the user-content
// string for Ask.
func BuildFunctionPrompt(funcName string, m FuncMatch) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Explain the function %q defined in %s:\n\n%s\n", funcName, m.File, m.Body)
	return b.String()
}

// BuildFilePrompt reads path (resolved relative to root unless already
// absolute) and returns the user-content string for Ask, explaining what
// the whole file does.
func BuildFilePrompt(root, path string) (string, error) {
	readPath := path
	if !filepath.IsAbs(path) {
		readPath = filepath.Join(root, path)
	}
	data, err := os.ReadFile(readPath)
	if err != nil {
		return "", fmt.Errorf("failed to read %s: %w", path, err)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Explain what this file (%s) does:\n\n%s\n", path, string(data))
	return b.String(), nil
}

// BuildLastErrorPrompt reads the most recent failing build/run log (see
// internal/logs) and the file(s) the compiler's error output pointed at
// (see internal/heal.ExtractReferencedFiles - the exact same scoping
// internal/heal.Suggest itself uses), and returns the user-content string
// for Ask, asking the model to explain the failure rather than fix it.
func BuildLastErrorPrompt(root string) (string, error) {
	logPath, err := logs.LatestFailure(root, "")
	if err != nil {
		return "", err
	}

	logData, err := os.ReadFile(logPath)
	if err != nil {
		return "", fmt.Errorf("failed to read %s: %w", logPath, err)
	}
	logText := string(logData)
	if len(logText) > maxLogChars {
		logText = "...(truncated)...\n" + logText[len(logText)-maxLogChars:]
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Explain the root cause of this build/run failure (%s):\n\n%s\n\n", logPath, logText)

	referenced := heal.ExtractReferencedFiles(logText, maxReferencedFiles)
	for _, f := range referenced {
		readPath := f
		if !filepath.IsAbs(f) {
			readPath = filepath.Join(root, f)
		}
		data, err := os.ReadFile(readPath)
		if err != nil {
			continue
		}
		fmt.Fprintf(&b, "--- file: %s ---\n%s\n\n", f, string(data))
	}
	return b.String(), nil
}

// BuildDiffPrompt runs `git diff` (working-tree changes, not yet committed)
// in root and returns the user-content string for Ask - empty if there are
// no changes, which the caller (cmd/explain.go) treats specially rather
// than asking the model to explain nothing.
func BuildDiffPrompt(root string) (string, error) {
	out, err := exec.Command("git", "-C", root, "diff").Output()
	if err != nil {
		return "", fmt.Errorf("failed to run 'git diff': %w", err)
	}
	diff := strings.TrimSpace(string(out))
	if diff == "" {
		return "", nil
	}
	return fmt.Sprintf("Explain this git diff (working-tree changes) in plain English - what changed and why it likely matters:\n\n%s\n", diff), nil
}

// BuildConfigPrompt reads root's cmaker.yaml and generated CMakeLists.txt
// (if present) and returns the user-content string for Ask, explaining the
// project's build configuration in plain English. CMakeLists.txt is
// included as-is rather than regenerated, since explaining "what's actually
// there right now" is the point - if it's stale, that's the project's own
// state to explain, not explain's job to fix.
func BuildConfigPrompt(root string) (string, error) {
	yamlData, err := os.ReadFile(filepath.Join(root, "cmaker.yaml"))
	if err != nil {
		return "", fmt.Errorf("failed to read cmaker.yaml: %w", err)
	}

	var b strings.Builder
	b.WriteString("Explain this project's CMake configuration in plain English - what it builds, its dependencies, and any notable settings:\n\n")
	fmt.Fprintf(&b, "--- cmaker.yaml ---\n%s\n\n", string(yamlData))

	if cmakeData, err := os.ReadFile(filepath.Join(root, "CMakeLists.txt")); err == nil {
		fmt.Fprintf(&b, "--- generated CMakeLists.txt ---\n%s\n", string(cmakeData))
	}
	return b.String(), nil
}

// maxDependencyUsages bounds how many #include/usage lines get sent for
// BuildDependencyPrompt - enough to show real usage patterns without
// dumping the whole project.
const maxDependencyUsages = 20

// BuildDependencyPrompt looks up name in the registry (its one-line Notes
// field) and searches the project's source files for lines that actually
// reference it (#include lines mentioning its name, case-insensitively -
// a loose heuristic, not a real include-graph resolution, matching this
// codebase's established "heuristic scan, not a real parser" posture -
// see internal/codegen's own doc comments), combining both into the
// user-content string for Ask so the explanation covers both what the
// library is and how this specific project actually uses it.
func BuildDependencyPrompt(root, name string) (string, error) {
	entry, ok := registry.Find(name)
	if !ok {
		return "", fmt.Errorf("%q not found in the dependency registry (see 'cmaker search %s')", name, name)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Explain the dependency %q and how this project uses it.\n\n", name)
	if entry.Notes != "" {
		fmt.Fprintf(&b, "Registry description: %s\n\n", entry.Notes)
	}

	files, err := WalkSourceFiles(root)
	if err != nil {
		return "", err
	}

	lowerName := strings.ToLower(name)
	var usages []string
	for _, f := range files {
		data, err := os.ReadFile(filepath.Join(root, f))
		if err != nil {
			continue
		}
		for i, line := range strings.Split(string(data), "\n") {
			trimmed := strings.TrimSpace(line)
			if !strings.HasPrefix(trimmed, "#include") {
				continue
			}
			if strings.Contains(strings.ToLower(trimmed), lowerName) {
				usages = append(usages, fmt.Sprintf("%s:%d: %s", f, i+1, trimmed))
				if len(usages) >= maxDependencyUsages {
					break
				}
			}
		}
		if len(usages) >= maxDependencyUsages {
			break
		}
	}

	if len(usages) > 0 {
		b.WriteString("Usage found in this project:\n")
		b.WriteString(strings.Join(usages, "\n"))
		b.WriteString("\n")
	} else {
		b.WriteString("No direct #include usage of it was found in this project's source files.\n")
	}
	return b.String(), nil
}
