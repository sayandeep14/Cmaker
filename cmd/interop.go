package cmd

import (
	"fmt"
	"os"
	"path/filepath"
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
