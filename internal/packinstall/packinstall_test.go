package packinstall

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"

	"cmaker/internal/packclient"
)

// buildTarGz is the test-only mirror of cmd/publish.go's buildPackTarball -
// a small, independent implementation (not imported from cmd, which
// internal/ packages never depend on) so these tests don't need a real
// pack directory on disk.
func buildTarGz(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, content := range files {
		hdr := &tar.Header{Name: name, Mode: 0644, Size: int64(len(content))}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestExtractWholeTarballNoPlacement(t *testing.T) {
	data := buildTarGz(t, map[string]string{
		"manifest.yaml":      "name: mypack\n",
		"include/mypack.hpp": "#pragma once\n",
		"src/mypack.cpp":     "// impl\n",
	})
	dest := t.TempDir()

	if err := Extract(data, dest, nil); err != nil {
		t.Fatalf("Extract() error = %v", err)
	}

	for _, rel := range []string{"manifest.yaml", "include/mypack.hpp", "src/mypack.cpp"} {
		if _, err := os.Stat(filepath.Join(dest, rel)); err != nil {
			t.Errorf("expected %s to exist: %v", rel, err)
		}
	}
}

func TestExtractWithPlacement(t *testing.T) {
	data := buildTarGz(t, map[string]string{
		"manifest.yaml":      "name: mypack\n",
		"include/mypack.hpp": "#pragma once\ninline void hello() {}\n",
	})
	dest := t.TempDir()

	placement := []packclient.ManifestPlacement{
		{Src: "include/mypack.hpp", Dest: "vendor/mypack/mypack.hpp"},
	}
	if err := Extract(data, dest, placement); err != nil {
		t.Fatalf("Extract() error = %v", err)
	}

	// Only the placed file should exist, not manifest.yaml (placement
	// scopes extraction to exactly what it lists).
	if _, err := os.Stat(filepath.Join(dest, "vendor/mypack/mypack.hpp")); err != nil {
		t.Errorf("expected the placed file to exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "manifest.yaml")); err == nil {
		t.Error("manifest.yaml should not have been extracted when placement is given")
	}

	got, err := os.ReadFile(filepath.Join(dest, "vendor/mypack/mypack.hpp"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "#pragma once\ninline void hello() {}\n" {
		t.Errorf("extracted content = %q, unexpected", got)
	}
}

func TestExtractPlacementSrcMissingFromTarball(t *testing.T) {
	data := buildTarGz(t, map[string]string{"manifest.yaml": "name: mypack\n"})
	dest := t.TempDir()

	placement := []packclient.ManifestPlacement{{Src: "include/missing.hpp", Dest: "include/missing.hpp"}}
	if err := Extract(data, dest, placement); err == nil {
		t.Error("Extract() with a placement Src not in the tarball: expected an error, got nil")
	}
}

func TestExtractRejectsPathTraversalInTarball(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	evil := "../../etc/passwd"
	hdr := &tar.Header{Name: evil, Mode: 0644, Size: 4}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	tw.Write([]byte("evil"))
	tw.Close()
	gz.Close()

	dest := t.TempDir()
	if err := Extract(buf.Bytes(), dest, nil); err == nil {
		t.Error("Extract() with a path-traversal tarball entry: expected an error, got nil")
	}
}

func TestExtractRejectsPathTraversalInPlacementDest(t *testing.T) {
	data := buildTarGz(t, map[string]string{"a.hpp": "content"})
	dest := t.TempDir()

	placement := []packclient.ManifestPlacement{{Src: "a.hpp", Dest: "../../evil.hpp"}}
	if err := Extract(data, dest, placement); err == nil {
		t.Error("Extract() with a path-traversal placement Dest: expected an error, got nil")
	}
}

func TestExtractSkipsNonRegularEntries(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	// A directory entry - should be silently skipped, not written as a file.
	tw.WriteHeader(&tar.Header{Name: "include/", Typeflag: tar.TypeDir, Mode: 0755})
	tw.WriteHeader(&tar.Header{Name: "include/a.hpp", Mode: 0644, Size: 1})
	tw.Write([]byte("x"))
	tw.Close()
	gz.Close()

	dest := t.TempDir()
	if err := Extract(buf.Bytes(), dest, nil); err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "include/a.hpp")); err != nil {
		t.Errorf("expected include/a.hpp to exist: %v", err)
	}
}
