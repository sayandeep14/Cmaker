package llm

import "testing"

func TestResolveModel(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"haiku", DefaultModel},
		{"HAIKU", DefaultModel},
		{"Sonnet", DefaultImproviseModel},
		{"opus", DefaultOpusModel},
		{"OPUS", DefaultOpusModel},
		{"claude-opus-5", "claude-opus-5"},
		{"", ""},
		{"gpt-4", "gpt-4"},
	}
	for _, tt := range tests {
		if got := ResolveModel(tt.in); got != tt.want {
			t.Errorf("ResolveModel(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestNewClientFromEnvResolvesModelAlias(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-env")

	client, err := NewClientFromEnv("sonnet")
	if err != nil {
		t.Fatalf("NewClientFromEnv() error = %v", err)
	}
	if client.Model != DefaultImproviseModel {
		t.Errorf("NewClientFromEnv(%q).Model = %q, want %q", "sonnet", client.Model, DefaultImproviseModel)
	}
}
