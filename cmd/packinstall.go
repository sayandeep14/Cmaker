package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"cmaker/internal/config"
	"cmaker/internal/packclient"
	"cmaker/internal/packinstall"
	"cmaker/internal/registry"
)

// tryInstallPack attempts to resolve rawName (optionally "name@version")
// against the remote packs registry and, if found, downloads/verifies/
// extracts it into the current directory - returns installed=false (no
// error) if not logged in, or the pack genuinely doesn't exist remotely
// either, so the caller (runInstall) can fall through to its own single
// "not in registry" error message rather than surfacing a confusing
// distinction between "not found" and "couldn't even check."
func tryInstallPack(rawName string) (installed bool, err error) {
	token, _, err := packclient.LoadCredentials()
	if err != nil {
		return false, err
	}
	if token == "" {
		return false, nil
	}

	packName, version := splitPackNameVersion(rawName)
	client := packclient.NewClient("", token)
	ctx := context.Background()

	info, err := client.GetPack(ctx, packName)
	if err != nil {
		return false, nil
	}

	if version == "" {
		version, err = latestPublishedVersion(info)
		if err != nil {
			return false, fmt.Errorf("%s: %w", packName, err)
		}
	}

	return true, installPackVersion(ctx, client, packName, version)
}

// splitPackNameVersion splits "name@version" on its last '@' - a pack
// name/version never contain '@' themselves (both match namePattern in
// internal/packclient/manifest.go), so this is unambiguous.
func splitPackNameVersion(rawName string) (name, version string) {
	if idx := strings.LastIndex(rawName, "@"); idx != -1 {
		return rawName[:idx], rawName[idx+1:]
	}
	return rawName, ""
}

func latestPublishedVersion(info packclient.PackInfo) (string, error) {
	for _, v := range info.Versions {
		if v.Status == "published" {
			return v.Version, nil
		}
	}
	return "", fmt.Errorf("has no published versions")
}

// remotePackSuggestions asks the packs registry for names close to query
// (via its own substring search) - a "did you mean" hint for a failed
// pack lookup, the same spirit as registry.CloseMatches for the built-in
// registry. Returns nil on any failure (not logged in, a network error,
// no matches) - a missing hint is never worth failing the whole error
// path over.
func remotePackSuggestions(query string) []string {
	token, _, err := packclient.LoadCredentials()
	if err != nil || token == "" {
		return nil
	}
	results, err := packclient.NewClient("", token).Search(context.Background(), query)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(results))
	for _, r := range results {
		if !strings.EqualFold(r.Name, query) {
			names = append(names, r.Name)
		}
	}
	return names
}

// installPackVersion downloads, checksum-verifies, and extracts
// packName@version into the current directory, resolves the pack's own
// declared dependencies (registry entries via the normal install path,
// other packs recursively), and records the install in cmaker.lock's
// Packs section.
func installPackVersion(ctx context.Context, client *packclient.Client, packName, version string) error {
	versionInfo, err := client.GetVersion(ctx, packName, version)
	if err != nil {
		if hint := availableVersionsHint(ctx, client, packName); hint != "" {
			return fmt.Errorf("failed to fetch %s@%s: %w (%s)", packName, version, err, hint)
		}
		return fmt.Errorf("failed to fetch %s@%s: %w", packName, version, err)
	}

	infof("Downloading %s@%s...", packName, version)
	data, err := client.DownloadTarball(ctx, versionInfo.DownloadURL)
	if err != nil {
		return err
	}
	if got := sha256Hex(data); got != versionInfo.ChecksumSHA256 {
		return fmt.Errorf("checksum mismatch for %s@%s (got %s, want %s) - refusing to extract", packName, version, got, versionInfo.ChecksumSHA256)
	}

	var manifest packclient.Manifest
	if err := json.Unmarshal(versionInfo.Manifest, &manifest); err != nil {
		return fmt.Errorf("failed to parse %s@%s's manifest: %w", packName, version, err)
	}

	infof("Extracting %s@%s...", packName, version)
	if err := packinstall.Extract(data, ".", manifest.Placement); err != nil {
		return err
	}

	for _, dep := range manifest.Dependencies {
		if err := installPackDependency(dep); err != nil {
			warnf("failed to install %s's dependency %s: %v", packName, dep.Name, err)
		}
	}

	if err := registry.RecordPack(".", packName, version, versionInfo.ChecksumSHA256); err != nil {
		debugf("cmaker.lock update: %v", err)
	}

	okf("Installed pack %s@%s.", packName, version)
	return nil
}

// availableVersionsHint looks up packName's published versions to append
// to a failed GetVersion error (e.g. a typo'd or unpublished version) -
// "" on any failure to look them up, or if there are none, since this is
// purely a nice-to-have hint, never worth failing over or blocking on.
func availableVersionsHint(ctx context.Context, client *packclient.Client, packName string) string {
	info, err := client.GetPack(ctx, packName)
	if err != nil {
		return ""
	}
	var versions []string
	for _, v := range info.Versions {
		if v.Status == "published" {
			versions = append(versions, v.Version)
		}
	}
	if len(versions) == 0 {
		return ""
	}
	return "available versions: " + strings.Join(versions, ", ")
}

// installPackDependency resolves one of a pack's own declared
// dependencies - a "registry" source through the exact same
// registry.Find -> config.Dependency path runInstall itself uses
// (skipped silently if already present, rather than treating "already
// installed" as a failure the way a direct 'cmaker install' invocation
// would), a "pack" source recursively through tryInstallPack with its
// pinned version.
func installPackDependency(dep packclient.ManifestDependency) error {
	switch dep.Source {
	case "registry":
		if cfg, err := config.Load("cmaker.yaml"); err == nil {
			for _, d := range cfg.Dependencies {
				if strings.EqualFold(d.Name, dep.Name) {
					return nil
				}
			}
		}
		return runInstall(dep.Name, "", "", nil, nil, false)
	case "pack":
		_, err := tryInstallPack(dep.Name + "@" + dep.Version)
		return err
	default:
		return fmt.Errorf("unknown dependency source %q", dep.Source)
	}
}
