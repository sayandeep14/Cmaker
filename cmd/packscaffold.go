package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"cmaker/internal/packclient"
)

// scaffoldPack writes a new publishable pack into root (creating it if
// it doesn't exist yet) - a manifest.yaml plus a starter header, not a
// buildable CMake project (no cmaker.yaml/CMakeLists.txt): a pack is
// source meant to be copied into other projects via a future 'cmaker
// install', not built standalone. See PACKS_PLAN.md §4 for the manifest
// format this writes.
func scaffoldPack(root, name string) error {
	headerRel := filepath.Join("include", name+".hpp")
	if err := os.MkdirAll(filepath.Join(root, "include"), 0755); err != nil {
		return fmt.Errorf("failed to create %s: %w", filepath.Join(root, "include"), err)
	}

	ns := packNamespace(name)
	headerContent := fmt.Sprintf(`#pragma once

// %s - describe what this pack does here.

namespace %s {

// TODO: replace with your actual implementation.
inline void hello() {}

}  // namespace %s
`, name, ns, ns)
	headerPath := filepath.Join(root, headerRel)
	if err := os.WriteFile(headerPath, []byte(headerContent), 0644); err != nil {
		return fmt.Errorf("failed to write %s: %w", headerPath, err)
	}

	manifest := packclient.Manifest{
		Name:        name,
		Version:     "0.1.0",
		Description: fmt.Sprintf("TODO: describe %s", name),
		License:     "MIT",
		Kind:        "directory",
		Placement: []packclient.ManifestPlacement{
			{Src: filepath.ToSlash(headerRel), Dest: filepath.ToSlash(headerRel)},
		},
	}
	if err := packclient.SaveManifest(root, manifest); err != nil {
		return err
	}

	readme := fmt.Sprintf(`# %s

A cmaker pack. Edit %s, update %s (version/description/dependencies as
needed), then publish it:

    cmaker login      # once, if you haven't already
    cmaker publish

Republishing an already-published version is rejected - bump the version
in %s first for any change after the first publish.
`, name, filepath.ToSlash(headerRel), packclient.ManifestFileName, packclient.ManifestFileName)
	readmePath := filepath.Join(root, "README.md")
	if err := os.WriteFile(readmePath, []byte(readme), 0644); err != nil {
		return fmt.Errorf("failed to write %s: %w", readmePath, err)
	}

	okf("Scaffolded pack %q in %s (%s, %s) - edit it, then 'cmaker publish'.", name, root, packclient.ManifestFileName, filepath.ToSlash(headerRel))
	return nil
}

// packNamespace turns a pack name (which may contain '-'/'.', not valid
// in a C++ identifier) into a usable namespace name.
func packNamespace(name string) string {
	var b strings.Builder
	for _, r := range name {
		if r == '-' || r == '.' {
			b.WriteRune('_')
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// packConflictsWithExplicitFlags reports an error if --pack was combined
// with any flag meant for a buildable-CMake-project scaffold - a pack
// isn't one (no cmaker.yaml/CMakeLists.txt at all), so those flags would
// silently do nothing, the same "fail loudly instead" reasoning
// describeConflictsWithExplicitFlags already established.
func packConflictsWithExplicitFlags(cmd *cobra.Command) error {
	for _, name := range []string{"template", "lang", "with-rust", "with-zig", "target-type", "lib", "describe", "with-benchmarks", "with-docs", "with-docker"} {
		if cmd.Flags().Changed(name) {
			return fmt.Errorf("--pack scaffolds a publishable pack, not a buildable CMake project - remove --%s or drop --pack", name)
		}
	}
	return nil
}
