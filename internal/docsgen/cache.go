package docsgen

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
)

// QualityCacheDir is where `cmaker status --detailed` persists its most
// recent AssessQuality verdict, relative to a project root - lets a later
// --detailed run reuse it instead of re-querying the LLM (and re-spending
// the tokens) when nothing that would change the answer has happened
// since: same source content, same model.
const QualityCacheDir = ".cmaker/status"

const qualityCacheFileName = "doc-quality.json"

// cachedQuality is the on-disk shape of a persisted Quality, keyed to the
// exact prompt (see PromptHash) and model it was computed from.
type cachedQuality struct {
	PromptHash string `json:"prompt_hash"`
	Model      string `json:"model"`
	Score      int    `json:"score"`
	Summary    string `json:"summary"`
}

// HashPrompt returns a short, stable digest of prompt (e.g. from
// BuildQualityPrompt) - the cache key that lets LoadQuality tell "the
// exact same source content, sampled the same way" apart from anything
// that changed since the last check, without needing to store the
// (large) prompt itself.
func HashPrompt(prompt string) string {
	sum := sha256.Sum256([]byte(prompt))
	return hex.EncodeToString(sum[:])
}

// SaveQuality persists q, keyed to promptHash and model, so a later
// `cmaker status --detailed` with an unchanged prompt and the same model
// can reuse it without a new LLM call. A write failure is deliberately
// non-fatal to the caller - the cache is a convenience, not a
// requirement for the quality check to work.
func SaveQuality(root, promptHash, model string, q Quality) error {
	dir := filepath.Join(root, QualityCacheDir)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := json.Marshal(cachedQuality{
		PromptHash: promptHash,
		Model:      model,
		Score:      q.Score,
		Summary:    q.Summary,
	})
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, qualityCacheFileName), data, 0644)
}

// LoadQuality returns the cached verdict for promptHash+model, if one
// exists and matches both exactly - a cache entry from a different
// source snapshot (the prompt hash changed - any tracked source file was
// added/removed/edited) or a different model is treated as not found,
// never silently served as if it still applied.
func LoadQuality(root, promptHash, model string) (Quality, bool) {
	data, err := os.ReadFile(filepath.Join(root, QualityCacheDir, qualityCacheFileName))
	if err != nil {
		return Quality{}, false
	}
	var c cachedQuality
	if err := json.Unmarshal(data, &c); err != nil {
		return Quality{}, false
	}
	if c.PromptHash != promptHash || c.Model != model {
		return Quality{}, false
	}
	return Quality{Score: c.Score, Summary: c.Summary}, true
}
