package token

import (
	"strings"
	"testing"
)

func TestGenerateHasPrefix(t *testing.T) {
	tok, err := Generate()
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if !strings.HasPrefix(tok, prefix) {
		t.Errorf("Generate() = %q, want prefix %q", tok, prefix)
	}
}

func TestGenerateIsUnique(t *testing.T) {
	a, err := Generate()
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	b, err := Generate()
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if a == b {
		t.Error("Generate() returned the same token twice")
	}
}

func TestHashIsDeterministic(t *testing.T) {
	first := Hash("abc")
	second := Hash("abc")
	if first != second {
		t.Errorf("Hash(\"abc\") = %q then %q, want the same value both times", first, second)
	}
}

func TestHashDiffersPerInput(t *testing.T) {
	if Hash("abc") == Hash("abd") {
		t.Error("Hash() collided for different inputs")
	}
}

func TestHashNeverEqualsRawToken(t *testing.T) {
	tok, err := Generate()
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if Hash(tok) == tok {
		t.Error("Hash() must never equal its own input")
	}
}
