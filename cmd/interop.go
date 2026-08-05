package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// writeInteropDemoMain writes the project's main source file to demonstrate
// whichever of --with-rust/--with-zig were requested. Either, both, or
// (via the plain template/lang path) neither can be in play; when both are
// set, one combined main() calls into both rather than the two scaffolders
// clobbering each other's file.
func writeInteropDemoMain(root string, language string, withRust, withZig bool) error {
	isC := language == "c"

	var content string
	switch {
	case withRust && withZig && isC:
		content = bothDemoMainC
	case withRust && withZig:
		content = bothDemoMainCpp
	case withRust && isC:
		content = rustDemoMainC
	case withRust:
		content = rustDemoMainCpp
	case withZig && isC:
		content = zigDemoMainC
	case withZig:
		content = zigDemoMainCpp
	default:
		return fmt.Errorf("writeInteropDemoMain called with neither --with-rust nor --with-zig")
	}

	filename := "main.cpp"
	if isC {
		filename = "main.c"
	}
	return os.WriteFile(filepath.Join(root, "src", filename), []byte(content), 0644)
}

const rustDemoMainCpp = `#include <iostream>
#include "rustlib.h"

int main() {
    std::cout << "Hello from cmaker (C++ calling Rust)! 2 + 3 (via Rust) = "
              << rust_add(2, 3) << "\n";
    return 0;
}
`

const rustDemoMainC = `#include <stdio.h>
#include "rustlib.h"

int main(void) {
    printf("Hello from cmaker (C calling Rust)! 2 + 3 (via Rust) = %d\n", rust_add(2, 3));
    return 0;
}
`

const zigDemoMainCpp = `#include <iostream>
#include "ziglib.h"

int main() {
    std::cout << "Hello from cmaker (C++ calling Zig)! 4 + 5 (via Zig) = "
              << zig_add(4, 5) << "\n";
    return 0;
}
`

const zigDemoMainC = `#include <stdio.h>
#include "ziglib.h"

int main(void) {
    printf("Hello from cmaker (C calling Zig)! 4 + 5 (via Zig) = %d\n", zig_add(4, 5));
    return 0;
}
`

const bothDemoMainCpp = `#include <iostream>
#include "rustlib.h"
#include "ziglib.h"

int main() {
    std::cout << "Hello from cmaker (C++ calling Rust + Zig)! "
              << "rust_add(2,3)=" << rust_add(2, 3) << ", "
              << "zig_add(4,5)=" << zig_add(4, 5) << "\n";
    return 0;
}
`

const bothDemoMainC = `#include <stdio.h>
#include "rustlib.h"
#include "ziglib.h"

int main(void) {
    printf("Hello from cmaker (C calling Rust + Zig)! rust_add(2,3)=%d, zig_add(4,5)=%d\n",
           rust_add(2, 3), zig_add(4, 5));
    return 0;
}
`

// injectInteropUsageHint is the non-default-template counterpart to
// writeInteropDemoMain (§18): a domain template's src/main.cpp is real,
// working service/demo code (an HTTP server, a linear-algebra example, ...)
// that --with-rust/--with-zig must never overwrite the way the generic
// 'default' template's placeholder main() gets overwritten. Instead, this
// only ever appends - a real #include for each linked library (so the
// template compiles with the crate genuinely available, not just present in
// cmaker.yaml) plus a short comment showing how to call into it, inserted
// right after the file's existing #include block rather than disturbing any
// of the template's own code.
func injectInteropUsageHint(root string, withRust, withZig bool) error {
	path := filepath.Join(root, "src", "main.cpp")
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read %s to add the Rust/Zig usage hint: %w", path, err)
	}

	var block strings.Builder
	if withRust {
		block.WriteString("#include \"rustlib.h\"\n")
	}
	if withZig {
		block.WriteString("#include \"ziglib.h\"\n")
	}
	block.WriteString("\n// --- cmaker: linked native crate(s) available, see below ---\n")
	if withRust {
		block.WriteString("// Rust (rust/src/lib.rs) is linked into this target - call it like:\n")
		block.WriteString("//   int sum = rust_add(2, 3);\n")
	}
	if withZig {
		block.WriteString("// Zig (zig/src/lib.zig) is linked into this target - call it like:\n")
		block.WriteString("//   int sum = zig_add(4, 5);\n")
	}

	lines := strings.Split(string(data), "\n")
	insertAt := 0
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "#include") {
			insertAt = i + 1
		}
	}

	var out []string
	out = append(out, lines[:insertAt]...)
	out = append(out, strings.Split(strings.TrimRight(block.String(), "\n"), "\n")...)
	out = append(out, lines[insertAt:]...)

	return os.WriteFile(path, []byte(strings.Join(out, "\n")), 0644)
}
