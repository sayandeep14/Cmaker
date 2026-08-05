package cmd

import (
	"fmt"
	"os"
	"path/filepath"
)

// addRustCrate scaffolds a minimal Rust crate (rust/Cargo.toml, rust/src/lib.rs)
// exposing a plain C ABI function, plus a matching hand-written C header
// (include/rustlib.h). The demo main() that calls into it is written
// separately by writeInteropDemoMain (interop.go), since a project can
// combine --with-rust and --with-zig and needs one main() calling into
// both, not two scaffolders each overwriting the other's file.
//
// Deliberately not using cargo init/cbindgen: the crate is small and fixed
// enough that hand-writing both sides keeps scaffolding working even
// offline (cargo/rustc are only needed at *build* time, not scaffold time),
// and avoids a cbindgen dependency for a two-function demo crate.
//
// Known limitation: only wired up for --template=default (see the
// composition restriction in scaffoldProject) - not composed with the
// dependency-fetching templates (sfml/raylib/catch2/headeronly).
func addRustCrate(root string) error {
	rustDir := filepath.Join(root, "rust", "src")
	if err := os.MkdirAll(rustDir, 0755); err != nil {
		return fmt.Errorf("failed to create rust/src/: %w", err)
	}

	if err := os.WriteFile(filepath.Join(root, "rust", "Cargo.toml"), []byte(rustCargoToml), 0644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(rustDir, "lib.rs"), []byte(rustLibRs), 0644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, "include", "rustlib.h"), []byte(rustHeader), 0644)
}

const rustCargoToml = `[package]
name = "rustlib"
version = "0.1.0"
edition = "2021"

[lib]
name = "rustlib"
crate-type = ["staticlib"]
`

const rustLibRs = `#[no_mangle]
pub extern "C" fn rust_add(a: i32, b: i32) -> i32 {
    a + b
}
`

const rustHeader = `#ifndef RUSTLIB_H
#define RUSTLIB_H

#ifdef __cplusplus
extern "C" {
#endif

int rust_add(int a, int b);

#ifdef __cplusplus
}
#endif

#endif
`
