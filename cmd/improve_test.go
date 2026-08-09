package cmd

import "testing"

// TestIsSafeNewFilePath guards the one new piece of trust surface non-
// strict 'cmaker improve' introduces: the model can now name a brand-new
// file to create. An absolute path or one that escapes the project root
// (e.g. "../../etc/passwd") must never be accepted, regardless of what
// the system prompt asked for.
func TestIsSafeNewFilePath(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"src/helpers.hpp", true},
		{"helpers.hpp", true},
		{"a/b/c.hpp", true},
		{"/etc/passwd", false},
		{"..", false},
		{"../outside.hpp", false},
		{"../../etc/passwd", false},
	}
	for _, tt := range tests {
		if got := isSafeNewFilePath(tt.path); got != tt.want {
			t.Errorf("isSafeNewFilePath(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}
