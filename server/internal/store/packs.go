package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// ErrConflict signals a request that's well-formed but can't be honored
// given existing state - a name/owner mismatch, or republishing an
// already-published version (immutable once published, matching
// Cargo/crates.io semantics - see PACKS_PLAN.md §2). Handlers translate
// this to an HTTP 409/403, distinct from ErrNotFound's 404 and a plain
// error's 500.
var ErrConflict = errors.New("conflict")

type Pack struct {
	ID          int64
	Name        string
	OwnerUserID int64
	Description string
	License     string
	CreatedAt   time.Time
}

type PackVersion struct {
	ID             int64
	PackID         int64
	Version        string
	ChecksumSHA256 string
	R2ObjectKey    string
	SizeBytes      int64
	ManifestJSON   []byte
	Status         string
	CreatedAt      time.Time
}

// GetPackByName looks up a pack by its (case-insensitive) name.
func (s *Store) GetPackByName(ctx context.Context, name string) (Pack, error) {
	var p Pack
	err := s.pool.QueryRow(ctx, `
		SELECT id, name, owner_user_id, description, license, created_at
		FROM packs WHERE lower(name) = lower($1)
	`, name).Scan(&p.ID, &p.Name, &p.OwnerUserID, &p.Description, &p.License, &p.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Pack{}, ErrNotFound
	}
	return p, err
}

// CreatePack inserts a brand-new pack owned by ownerUserID - the first
// version published under a name is what creates its pack row (see
// UpsertVersionForPublish).
func (s *Store) CreatePack(ctx context.Context, name string, ownerUserID int64, description, license string) (Pack, error) {
	var p Pack
	err := s.pool.QueryRow(ctx, `
		INSERT INTO packs (name, owner_user_id, description, license)
		VALUES ($1, $2, $3, $4)
		RETURNING id, name, owner_user_id, description, license, created_at
	`, name, ownerUserID, description, license).Scan(&p.ID, &p.Name, &p.OwnerUserID, &p.Description, &p.License, &p.CreatedAt)
	return p, err
}

// UpsertVersionForPublish returns the pending pack_versions row to upload
// against for (packID, version): a fresh insert if this version has
// never been attempted, or the existing pending row if a previous
// publish attempt started but never called /complete (idempotent retry -
// reuses the same object key rather than orphaning the earlier upload).
// Returns ErrConflict if this exact version was already published -
// versions are immutable once published.
func (s *Store) UpsertVersionForPublish(ctx context.Context, packID int64, version, checksumSHA256, objectKey string, sizeBytes int64, manifestJSON []byte) (PackVersion, error) {
	existing, err := s.versionByPackAndVersion(ctx, packID, version)
	if err == nil {
		if existing.Status == "published" {
			return PackVersion{}, ErrConflict
		}
		return existing, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return PackVersion{}, err
	}

	var v PackVersion
	insertErr := s.pool.QueryRow(ctx, `
		INSERT INTO pack_versions (pack_id, version, checksum_sha256, r2_object_key, size_bytes, manifest_json)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, pack_id, version, checksum_sha256, r2_object_key, size_bytes, manifest_json, status, created_at
	`, packID, version, checksumSHA256, objectKey, sizeBytes, manifestJSON).Scan(
		&v.ID, &v.PackID, &v.Version, &v.ChecksumSHA256, &v.R2ObjectKey, &v.SizeBytes, &v.ManifestJSON, &v.Status, &v.CreatedAt,
	)
	return v, insertErr
}

func (s *Store) versionByPackAndVersion(ctx context.Context, packID int64, version string) (PackVersion, error) {
	var v PackVersion
	err := s.pool.QueryRow(ctx, `
		SELECT id, pack_id, version, checksum_sha256, r2_object_key, size_bytes, manifest_json, status, created_at
		FROM pack_versions WHERE pack_id = $1 AND version = $2
	`, packID, version).Scan(&v.ID, &v.PackID, &v.Version, &v.ChecksumSHA256, &v.R2ObjectKey, &v.SizeBytes, &v.ManifestJSON, &v.Status, &v.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return PackVersion{}, ErrNotFound
	}
	return v, err
}

// GetVersion looks up one pack's version by name+version string (the
// shape handlers actually have on hand from the URL path).
func (s *Store) GetVersion(ctx context.Context, packName, version string) (PackVersion, error) {
	var v PackVersion
	err := s.pool.QueryRow(ctx, `
		SELECT v.id, v.pack_id, v.version, v.checksum_sha256, v.r2_object_key, v.size_bytes, v.manifest_json, v.status, v.created_at
		FROM pack_versions v
		JOIN packs p ON p.id = v.pack_id
		WHERE lower(p.name) = lower($1) AND v.version = $2
	`, packName, version).Scan(&v.ID, &v.PackID, &v.Version, &v.ChecksumSHA256, &v.R2ObjectKey, &v.SizeBytes, &v.ManifestJSON, &v.Status, &v.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return PackVersion{}, ErrNotFound
	}
	return v, err
}

// PublishVersion flips a pending version to published - called only
// after the object store confirms the uploaded object actually exists
// (see the /complete handler).
func (s *Store) PublishVersion(ctx context.Context, versionID int64) error {
	_, err := s.pool.Exec(ctx, `UPDATE pack_versions SET status = 'published' WHERE id = $1`, versionID)
	return err
}

// ListVersions returns every version of packID, newest first.
func (s *Store) ListVersions(ctx context.Context, packID int64) ([]PackVersion, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, pack_id, version, checksum_sha256, r2_object_key, size_bytes, manifest_json, status, created_at
		FROM pack_versions WHERE pack_id = $1 ORDER BY created_at DESC
	`, packID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var versions []PackVersion
	for rows.Next() {
		var v PackVersion
		if err := rows.Scan(&v.ID, &v.PackID, &v.Version, &v.ChecksumSHA256, &v.R2ObjectKey, &v.SizeBytes, &v.ManifestJSON, &v.Status, &v.CreatedAt); err != nil {
			return nil, err
		}
		versions = append(versions, v)
	}
	return versions, rows.Err()
}

// SearchPacks does a case-insensitive substring match over name and
// description - a plain SQL LIKE, not full-text ranking (an explicit POC
// non-goal, see PACKS_PLAN.md §10).
func (s *Store) SearchPacks(ctx context.Context, query string, limit, offset int) ([]Pack, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, name, owner_user_id, description, license, created_at
		FROM packs
		WHERE name ILIKE '%' || $1 || '%' OR description ILIKE '%' || $1 || '%'
		ORDER BY name
		LIMIT $2 OFFSET $3
	`, query, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var packs []Pack
	for rows.Next() {
		var p Pack
		if err := rows.Scan(&p.ID, &p.Name, &p.OwnerUserID, &p.Description, &p.License, &p.CreatedAt); err != nil {
			return nil, err
		}
		packs = append(packs, p)
	}
	return packs, rows.Err()
}
