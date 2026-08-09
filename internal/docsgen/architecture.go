package docsgen

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"cmaker/internal/explain"
)

// maxArchitectureChars bounds how much source gets sent for the
// architecture overview - a whole-project dump doesn't scale, the same
// reasoning and the same budget internal/suggest.BuildPrompt already
// applies (not reused directly: that function's own preamble text is
// suggestion-specific ["Suggest concrete improvements..."], which would
// conflict with NarrateArchitecture's system prompt asking for prose
// instead).
const maxArchitectureChars = 40000

// BuildArchitecturePrompt gathers root's cmaker.yaml and as many source
// files as fit within maxArchitectureChars (largest first, the same
// "biggest files are the most likely to matter" heuristic
// internal/suggest uses) into the prompt string for NarrateArchitecture.
func BuildArchitecturePrompt(root string) (string, error) {
	files, err := explain.WalkSourceFiles(root)
	if err != nil {
		return "", err
	}

	type fileInfo struct {
		rel  string
		size int64
	}
	infos := make([]fileInfo, 0, len(files))
	for _, f := range files {
		fi, err := os.Stat(filepath.Join(root, f))
		if err != nil {
			continue
		}
		infos = append(infos, fileInfo{rel: f, size: fi.Size()})
	}
	sort.Slice(infos, func(i, j int) bool { return infos[i].size > infos[j].size })

	var b strings.Builder
	b.WriteString("Write an architecture overview for this C/C++ project.\n\n")

	if cfgData, err := os.ReadFile(filepath.Join(root, "cmaker.yaml")); err == nil {
		fmt.Fprintf(&b, "--- cmaker.yaml ---\n%s\n\n", string(cfgData))
	}

	budget := maxArchitectureChars
	included := 0
	for _, fi := range infos {
		if budget <= 0 {
			break
		}
		data, err := os.ReadFile(filepath.Join(root, fi.rel))
		if err != nil {
			continue
		}
		content := string(data)
		if len(content) > budget {
			continue
		}
		fmt.Fprintf(&b, "--- file: %s ---\n%s\n\n", fi.rel, content)
		budget -= len(content)
		included++
	}
	if included == 0 {
		return "", fmt.Errorf("no source files found under %s to write an architecture overview for", root)
	}
	return b.String(), nil
}
