package packclient

import (
	"testing"
)

func TestSaveLoadClearCredentials(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	token, login, err := LoadCredentials()
	if err != nil {
		t.Fatalf("LoadCredentials() before any save: error = %v", err)
	}
	if token != "" || login != "" {
		t.Errorf("LoadCredentials() before any save = (%q, %q), want empty", token, login)
	}

	if err := SaveCredentials("cmpk_live_test", "octocat"); err != nil {
		t.Fatalf("SaveCredentials() error = %v", err)
	}

	token, login, err = LoadCredentials()
	if err != nil {
		t.Fatalf("LoadCredentials() error = %v", err)
	}
	if token != "cmpk_live_test" || login != "octocat" {
		t.Errorf("LoadCredentials() = (%q, %q), want (cmpk_live_test, octocat)", token, login)
	}

	if err := ClearCredentials(); err != nil {
		t.Fatalf("ClearCredentials() error = %v", err)
	}
	token, login, err = LoadCredentials()
	if err != nil {
		t.Fatalf("LoadCredentials() after clear: error = %v", err)
	}
	if token != "" || login != "" {
		t.Errorf("LoadCredentials() after clear = (%q, %q), want empty", token, login)
	}
}

func TestClearCredentialsWhenNoneSaved(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := ClearCredentials(); err != nil {
		t.Errorf("ClearCredentials() with nothing saved: error = %v, want nil", err)
	}
}

func TestSaveCredentialsOverwritesPrevious(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	if err := SaveCredentials("cmpk_live_old", "old-user"); err != nil {
		t.Fatal(err)
	}
	if err := SaveCredentials("cmpk_live_new", "new-user"); err != nil {
		t.Fatal(err)
	}
	token, login, err := LoadCredentials()
	if err != nil {
		t.Fatalf("LoadCredentials() error = %v", err)
	}
	if token != "cmpk_live_new" || login != "new-user" {
		t.Errorf("LoadCredentials() = (%q, %q), want the newer credentials", token, login)
	}
}
