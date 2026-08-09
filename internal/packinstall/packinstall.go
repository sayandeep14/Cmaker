// Package packinstall extracts a downloaded pack tarball into a project -
// the read side of cmd/publish.go's tarball building, used by `cmaker
// install`'s remote-pack fallback (see cmd/install.go). Path-traversal-safe:
// every entry name and every manifest-declared destination is checked
// before anything is written to disk, since a pack's tarball and manifest
// both come from a remote source this code has no reason to trust blindly.
package packinstall

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"cmaker/internal/packclient"
)

// Extract writes a pack's tarball contents into destRoot. If placement is
// empty, the whole tarball is extracted preserving relative paths (the
// "just drop a directory in" case - see PACKS_PLAN.md §4); otherwise only
// the files named in placement are written, each from its tarball path
// (Src) to its project-relative destination (Dest).
func Extract(data []byte, destRoot string, placement []packclient.ManifestPlacement) error {
	entries, err := readTarGz(data)
	if err != nil {
		return err
	}

	if len(placement) == 0 {
		for name, content := range entries {
			if err := writeUnder(destRoot, name, content); err != nil {
				return err
			}
		}
		return nil
	}

	for _, p := range placement {
		content, ok := entries[p.Src]
		if !ok {
			return fmt.Errorf("manifest references %q, but the tarball doesn't contain it", p.Src)
		}
		if err := writeUnder(destRoot, p.Dest, content); err != nil {
			return err
		}
	}
	return nil
}

// readTarGz decodes a gzip-compressed tar archive into path -> content,
// regular files only (a pack tarball has no reason to contain symlinks/
// devices/etc., and skipping them outright avoids having to reason about
// their own safety separately).
func readTarGz(data []byte) (map[string][]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("failed to open tarball: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)

	entries := map[string][]byte{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read tarball entry: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		if !safeRelPath(hdr.Name) {
			return nil, fmt.Errorf("tarball entry %q is not a safe relative path - refusing to extract", hdr.Name)
		}
		content, err := io.ReadAll(tr)
		if err != nil {
			return nil, fmt.Errorf("failed to read %s from tarball: %w", hdr.Name, err)
		}
		entries[hdr.Name] = content
	}
	return entries, nil
}

// safeRelPath rejects an absolute path or one that escapes upward
// (e.g. "../../etc/passwd") - the same defensive check
// cmd/improve.go's isSafeNewFilePath already applies to LLM-proposed
// paths, here applied to a remote pack's tarball entries and its
// manifest's declared placement destinations.
func safeRelPath(name string) bool {
	clean := filepath.Clean(name)
	if filepath.IsAbs(clean) {
		return false
	}
	return clean != ".." && !strings.HasPrefix(clean, ".."+string(filepath.Separator))
}

func writeUnder(root, rel string, content []byte) error {
	if !safeRelPath(rel) {
		return fmt.Errorf("refusing to write outside the project: %q", rel)
	}
	target := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return fmt.Errorf("failed to create directory for %s: %w", target, err)
	}
	if err := os.WriteFile(target, content, 0644); err != nil {
		return fmt.Errorf("failed to write %s: %w", target, err)
	}
	return nil
}
