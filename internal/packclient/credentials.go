package packclient

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"cmaker/internal/llm"
)

// credentialsFilePath is ~/.cmaker/credentials - the same ConfigDir()
// (0700) internal/llm's own env file (the Anthropic API key) already
// uses, but a deliberately separate file (0600), never mixed with it -
// PACKS_PLAN.md §6's hard requirement for "a second, clearly-separated
// credential."
func credentialsFilePath() (string, error) {
	dir, err := llm.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "credentials"), nil
}

// SaveCredentials persists token/login to ~/.cmaker/credentials
// (overwriting any previous login), in the same flat KEY=value format
// internal/llm's env file uses.
func SaveCredentials(token, login string) error {
	dir, err := llm.ConfigDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create %s: %w", dir, err)
	}
	path, err := credentialsFilePath()
	if err != nil {
		return err
	}
	content := fmt.Sprintf("CMAKER_PACKS_TOKEN=%s\nCMAKER_PACKS_USER=%s\n", token, login)
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		return fmt.Errorf("failed to write %s: %w", path, err)
	}
	return nil
}

// LoadCredentials reads a previously saved login. Returns ("", "", nil) -
// not an error - when nothing has been saved yet (the normal case before
// the first 'cmaker login').
func LoadCredentials() (token, login string, err error) {
	path, err := credentialsFilePath()
	if err != nil {
		return "", "", err
	}
	data, readErr := os.ReadFile(path)
	if os.IsNotExist(readErr) {
		return "", "", nil
	}
	if readErr != nil {
		return "", "", fmt.Errorf("failed to read %s: %w", path, readErr)
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		if v, ok := strings.CutPrefix(line, "CMAKER_PACKS_TOKEN="); ok {
			token = strings.TrimSpace(v)
		}
		if v, ok := strings.CutPrefix(line, "CMAKER_PACKS_USER="); ok {
			login = strings.TrimSpace(v)
		}
	}
	return token, login, nil
}

// ClearCredentials removes ~/.cmaker/credentials - a no-op, not an error,
// if it doesn't exist (cmaker logout when already logged out).
func ClearCredentials() error {
	path, err := credentialsFilePath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove %s: %w", path, err)
	}
	return nil
}
