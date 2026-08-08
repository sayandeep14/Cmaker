package llm

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveAndResolveStoredAPIKey(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	if err := SaveAPIKey("sk-test-123"); err != nil {
		t.Fatalf("SaveAPIKey() error = %v", err)
	}

	got, err := resolveStoredAPIKey()
	if err != nil {
		t.Fatalf("resolveStoredAPIKey() error = %v", err)
	}
	if got != "sk-test-123" {
		t.Errorf("resolveStoredAPIKey() = %q, want %q", got, "sk-test-123")
	}
}

func TestSaveAPIKeyOverwritesPrevious(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	if err := SaveAPIKey("sk-old"); err != nil {
		t.Fatalf("SaveAPIKey() error = %v", err)
	}
	if err := SaveAPIKey("sk-new"); err != nil {
		t.Fatalf("SaveAPIKey() error = %v", err)
	}

	got, err := resolveStoredAPIKey()
	if err != nil {
		t.Fatalf("resolveStoredAPIKey() error = %v", err)
	}
	if got != "sk-new" {
		t.Errorf("resolveStoredAPIKey() = %q, want %q (overwritten)", got, "sk-new")
	}
}

func TestSaveAPIKeyFilePermissions(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if err := SaveAPIKey("sk-test"); err != nil {
		t.Fatalf("SaveAPIKey() error = %v", err)
	}

	info, err := os.Stat(filepath.Join(home, ".cmaker", "env"))
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("env file permissions = %v, want 0600", perm)
	}
}

func TestResolveStoredAPIKeyNoFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	got, err := resolveStoredAPIKey()
	if err != nil {
		t.Fatalf("resolveStoredAPIKey() error = %v, want nil for a missing file", err)
	}
	if got != "" {
		t.Errorf("resolveStoredAPIKey() = %q, want empty", got)
	}
}

func TestConfigDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	dir, err := ConfigDir()
	if err != nil {
		t.Fatalf("ConfigDir() error = %v", err)
	}
	want := filepath.Join(home, ".cmaker")
	if dir != want {
		t.Errorf("ConfigDir() = %q, want %q", dir, want)
	}
}

func TestNewClientFromEnvPrefersEnvVarOverStoredKey(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := SaveAPIKey("sk-stored"); err != nil {
		t.Fatalf("SaveAPIKey() error = %v", err)
	}
	t.Setenv("ANTHROPIC_API_KEY", "sk-env")

	client, err := NewClientFromEnv("")
	if err != nil {
		t.Fatalf("NewClientFromEnv() error = %v", err)
	}
	if client.APIKey != "sk-env" {
		t.Errorf("NewClientFromEnv() APIKey = %q, want the env var to win over the stored key", client.APIKey)
	}
}

func TestNewClientFromEnvFallsBackToStoredKey(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ANTHROPIC_API_KEY", "")
	if err := SaveAPIKey("sk-stored"); err != nil {
		t.Fatalf("SaveAPIKey() error = %v", err)
	}

	client, err := NewClientFromEnv("")
	if err != nil {
		t.Fatalf("NewClientFromEnv() error = %v", err)
	}
	if client.APIKey != "sk-stored" {
		t.Errorf("NewClientFromEnv() APIKey = %q, want the stored key", client.APIKey)
	}
}

func TestNewClientFromEnvNeitherSet(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ANTHROPIC_API_KEY", "")

	if _, err := NewClientFromEnv(""); err == nil {
		t.Error("NewClientFromEnv() expected an error when no key is set anywhere, got nil")
	}
}
