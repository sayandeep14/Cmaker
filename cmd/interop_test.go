package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInjectInteropUsageHintPreservesExistingCode(t *testing.T) {
	dir := t.TempDir()
	original := `#include <httplib.h>
#include <iostream>

int main() {
    httplib::Server svr;
    svr.listen("127.0.0.1", 8080);
    return 0;
}
`
	writeFileAt(t, filepath.Join(dir, "src", "main.cpp"), original)

	if err := injectInteropUsageHint(dir, true, true); err != nil {
		t.Fatalf("injectInteropUsageHint() error = %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "src", "main.cpp"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)

	for _, want := range []string{
		`#include "rustlib.h"`,
		`#include "ziglib.h"`,
		"rust_add(2, 3)",
		"zig_add(4, 5)",
		`httplib::Server svr;`,
		`svr.listen("127.0.0.1", 8080);`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}

	// The template's own real code must survive completely untouched, not
	// just "still present somewhere" - this is the whole point of §18's
	// split versus the 'default' template's overwrite-the-whole-file path.
	if !strings.Contains(got, "httplib::Server svr;\n    svr.listen(\"127.0.0.1\", 8080);\n    return 0;\n}") {
		t.Errorf("template's own main() body was altered:\n%s", got)
	}
}

func TestInjectInteropUsageHintRustOnly(t *testing.T) {
	dir := t.TempDir()
	writeFileAt(t, filepath.Join(dir, "src", "main.cpp"), "#include <iostream>\n\nint main() { return 0; }\n")

	if err := injectInteropUsageHint(dir, true, false); err != nil {
		t.Fatalf("injectInteropUsageHint() error = %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "src", "main.cpp"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)

	if !strings.Contains(got, `#include "rustlib.h"`) {
		t.Errorf("expected rustlib.h include:\n%s", got)
	}
	if strings.Contains(got, "ziglib.h") {
		t.Errorf("--with-rust only should not mention ziglib.h:\n%s", got)
	}
}
