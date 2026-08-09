package cmd

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"cmaker/internal/packclient"
)

var publishCmd = &cobra.Command{
	Use:   "publish [path]",
	Short: "Publish a pack to the cmaker packs registry",
	Long: "Packages [path] (default: current directory) - which must contain a manifest.yaml, see\n" +
		"'cmaker new <name> --pack' - into a tarball and publishes it: registers the version,\n" +
		"uploads the tarball directly to the registry's storage, then finalizes the publish.\n" +
		"Republishing an already-published version is rejected - versions are immutable once\n" +
		"published; bump manifest.yaml's version instead.\n" +
		"\n" +
		"Requires 'cmaker login' first.",
	Example: "  cmaker publish\n  cmaker publish ./mypack",
	Args:    cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		dir := "."
		if len(args) == 1 {
			dir = args[0]
		}
		server, _ := cmd.Flags().GetString("server")
		return runPublish(dir, server)
	},
}

func init() {
	publishCmd.Flags().String("server", "", "override the cmaker packs API server URL (default: "+packclient.DefaultBaseURL+")")
}

func runPublish(dir, server string) error {
	manifest, err := packclient.LoadManifest(dir)
	if err != nil {
		return fmt.Errorf("%w\n(run 'cmaker new <name> --pack' to scaffold one, or write manifest.yaml by hand)", err)
	}

	token, _, err := packclient.LoadCredentials()
	if err != nil {
		return err
	}
	if token == "" {
		return fmt.Errorf("not logged in - run 'cmaker login' first")
	}

	infof("Packaging %s@%s from %s...", manifest.Name, manifest.Version, dir)
	tarball, err := buildPackTarball(dir)
	if err != nil {
		return err
	}
	checksum := sha256Hex(tarball)

	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("failed to encode manifest: %w", err)
	}

	client := packclient.NewClient(server, token)
	ctx := context.Background()

	infof("Registering %s@%s (%d bytes)...", manifest.Name, manifest.Version, len(tarball))
	resp, err := client.CreateVersion(ctx, manifest.Name, packclient.CreateVersionRequest{
		Version:        manifest.Version,
		Description:    manifest.Description,
		License:        manifest.License,
		ChecksumSHA256: checksum,
		SizeBytes:      int64(len(tarball)),
		Manifest:       manifestJSON,
	})
	if err != nil {
		return err
	}

	infof("Uploading...")
	if err := client.UploadTarball(ctx, resp.UploadURL, tarball); err != nil {
		return err
	}

	infof("Finalizing...")
	if err := client.CompleteUpload(ctx, manifest.Name, manifest.Version); err != nil {
		return err
	}

	okf("Published %s@%s.", manifest.Name, manifest.Version)
	return nil
}

// packTarballSkip is what buildPackTarball leaves out of the published
// tarball - version control and build-artifact clutter that was never
// meant to be part of the pack itself.
var packTarballSkip = map[string]bool{
	".git":      true,
	".cmaker":   true,
	"build":     true,
	".DS_Store": true,
}

// buildPackTarball gzip-tars every file under dir (paths relative to dir,
// forward-slash separated so the archive is portable), skipping
// packTarballSkip entries - manifest.yaml itself is included, exactly as
// it sits in dir, since the server stores its own copy from the request
// body but the tarball is still the thing 'cmaker install' eventually
// extracts wholesale when a pack has no explicit Placement rules.
func buildPackTarball(dir string) ([]byte, error) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	walkErr := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(dir, path)
		if relErr != nil {
			return relErr
		}
		if rel == "." {
			return nil
		}
		if packTarballSkip[d.Name()] {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return err
		}
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = filepath.ToSlash(rel)
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		_, err = tw.Write(data)
		return err
	})
	if walkErr != nil {
		return nil, fmt.Errorf("failed to package %s: %w", dir, walkErr)
	}
	if err := tw.Close(); err != nil {
		return nil, fmt.Errorf("failed to finalize tarball: %w", err)
	}
	if err := gz.Close(); err != nil {
		return nil, fmt.Errorf("failed to finalize gzip stream: %w", err)
	}
	return buf.Bytes(), nil
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
