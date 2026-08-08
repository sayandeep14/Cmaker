package heal

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// CacheDir is where `cmaker heal` persists its most recent suggestion,
// relative to a project root - lets `cmaker heal --apply` reuse a
// diagnosis the user already reviewed via a plain `cmaker heal` instead of
// re-querying the LLM (and re-prompting for confirmation) for the exact
// same failure.
const CacheDir = ".cmaker/heal"

const cacheFileName = "last-suggestion.json"

// cachedSuggestion is the on-disk shape of a persisted Suggestion, keyed to
// the exact failing log it was computed from.
type cachedSuggestion struct {
	LogPath         string   `json:"log_path"`
	Diff            string   `json:"diff"`
	ReferencedFiles []string `json:"referenced_files"`
}

// SaveSuggestion persists s, keyed to logPath, so a later `cmaker heal
// --apply` for the same failure can reuse it without a new LLM call. A
// write failure is deliberately non-fatal to the caller - the cache is a
// convenience, not a requirement for `cmaker heal` to work.
func SaveSuggestion(root, logPath string, s Suggestion) error {
	dir := filepath.Join(root, CacheDir)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := json.Marshal(cachedSuggestion{
		LogPath:         logPath,
		Diff:            s.Diff,
		ReferencedFiles: s.ReferencedFiles,
	})
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, cacheFileName), data, 0644)
}

// LoadSuggestionFor returns the cached suggestion for logPath, if one
// exists and was computed from that exact log. A cache entry left over
// from a different (older) failing log is treated as not found - it was
// diagnosed for a failure that isn't the current one anymore.
func LoadSuggestionFor(root, logPath string) (Suggestion, bool) {
	data, err := os.ReadFile(filepath.Join(root, CacheDir, cacheFileName))
	if err != nil {
		return Suggestion{}, false
	}
	var c cachedSuggestion
	if err := json.Unmarshal(data, &c); err != nil {
		return Suggestion{}, false
	}
	if c.LogPath != logPath {
		return Suggestion{}, false
	}
	return Suggestion{Diff: c.Diff, ReferencedFiles: c.ReferencedFiles}, true
}

// ClearSuggestion removes the cached suggestion, if any - called once a
// diff has actually been applied, so a stale "previous result" is never
// silently reused (and reapplied on top of itself) on a later `cmaker heal
// --apply` for the same log path. Best-effort: a missing cache file is not
// an error.
func ClearSuggestion(root string) {
	os.Remove(filepath.Join(root, CacheDir, cacheFileName))
}
