package cmd

import (
	"fmt"
	"os"
	"path/filepath"
)

// addZigLib scaffolds a minimal Zig source file (zig/src/lib.zig) exposing
// a plain C ABI function (`export fn`), plus a matching hand-written C
// header (include/ziglib.h). Built via `zig build-lib` directly against the
// single source file rather than a full `zig build`/build.zig project
// layout - mirroring addRustCrate's direct-cargo-build approach in rust.go:
// fewer moving parts for a single-file demo library.
func addZigLib(root string) error {
	zigDir := filepath.Join(root, "zig", "src")
	if err := os.MkdirAll(zigDir, 0755); err != nil {
		return fmt.Errorf("failed to create zig/src/: %w", err)
	}

	if err := os.WriteFile(filepath.Join(zigDir, "lib.zig"), []byte(zigLibZig), 0644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, "include", "ziglib.h"), []byte(zigHeader), 0644)
}

const zigLibZig = `export fn zig_add(a: i32, b: i32) i32 {
    return a + b;
}
`

const zigHeader = `#ifndef ZIGLIB_H
#define ZIGLIB_H

#ifdef __cplusplus
extern "C" {
#endif

int zig_add(int a, int b);

#ifdef __cplusplus
}
#endif

#endif
`
