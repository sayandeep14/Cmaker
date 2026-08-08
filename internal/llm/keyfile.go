package llm

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ConfigDir returns cmaker's per-user config directory (~/.cmaker) - not
// to be confused with a project's own local .cmaker/ (build/run log
// captures, heal's cached diagnosis - see internal/logs, internal/heal).
// This one lives in the user's home directory and holds cross-project
// settings; today that's just a saved Anthropic API key (see SaveAPIKey),
// written by the installer (scripts/install.sh) for anyone who'd rather
// not put a secret in a shell dotfile.
func ConfigDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to resolve home directory: %w", err)
	}
	return filepath.Join(home, ".cmaker"), nil
}

// envFilePath is where SaveAPIKey writes and resolveStoredAPIKey reads.
func envFilePath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "env"), nil
}

// SaveAPIKey persists key to cmaker's dedicated env file (~/.cmaker/env,
// 0600 - the directory itself is created 0700 if it doesn't exist yet),
// overwriting any previously saved key. Used by the installer's
// interactive API-key prompt; nothing else in cmaker writes this file.
func SaveAPIKey(key string) error {
	dir, err := ConfigDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create %s: %w", dir, err)
	}
	path, err := envFilePath()
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte("ANTHROPIC_API_KEY="+key+"\n"), 0600); err != nil {
		return fmt.Errorf("failed to write %s: %w", path, err)
	}
	return nil
}

// resolveStoredAPIKey reads a previously-saved key from ~/.cmaker/env, if
// present. Returns "" with no error when the file doesn't exist at all -
// the normal case for anyone who hasn't run the installer's key-prompt
// step (or deliberately skipped it) and is relying on the
// ANTHROPIC_API_KEY environment variable instead (see NewClientFromEnv,
// which only falls back to this when the env var is unset - the env var
// always wins, so it still works as a same-session override).
func resolveStoredAPIKey() (string, error) {
	path, err := envFilePath()
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("failed to read %s: %w", path, err)
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "ANTHROPIC_API_KEY="); ok {
			return strings.TrimSpace(v), nil
		}
	}
	return "", nil
}
