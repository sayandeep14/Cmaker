package cmake

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
)

var compilerNameRe = regexp.MustCompile(`^(clang\+\+|clang|g\+\+|gcc)(-[0-9]+(\.[0-9]+)?)?$`)

// DiscoverCompilers enumerates every C/C++ compiler found on PATH plus a
// handful of common non-PATH install locations, so users with multiple
// toolchains installed (several Homebrew/apt clang or gcc versions, a
// cross-compiler) can see and pick between them - both `cmaker doctor`
// (reporting every one found, not just a single ready/missing clang++/g++)
// and the TUI's New Project flow (an interactive picker when more than one
// is detected) share this one implementation, kept here rather than in
// cmd/ specifically so both can call it without a package-layering cycle
// (cmd imports internal/tui; internal/tui can't import back into cmd).
func DiscoverCompilers() []string {
	dirs := filepath.SplitList(os.Getenv("PATH"))
	dirs = append(dirs, "/opt/homebrew/opt/llvm/bin", "/usr/local/opt/llvm/bin")

	seen := map[string]bool{}
	var found []string
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !compilerNameRe.MatchString(e.Name()) {
				continue
			}
			full := filepath.Join(dir, e.Name())
			if seen[full] {
				continue
			}
			if info, err := os.Stat(full); err != nil || info.Mode()&0111 == 0 {
				continue
			}
			seen[full] = true
			found = append(found, full)
		}
	}
	sort.Strings(found)
	return found
}
