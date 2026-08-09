package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"cmaker-packs-server/internal/store"
)

// presignExpiry bounds how long an upload/download URL stays valid - long
// enough for a slow connection to finish a small tarball, short enough
// that a leaked URL doesn't stay useful indefinitely.
const presignExpiry = 15 * time.Minute

// namePattern is what a pack name or version string is allowed to look
// like - used directly as an R2 object-key path segment, so this also
// guards against anything that could be mistaken for a path-traversal
// attempt, not just cosmetic validation.
var namePattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

type packVersionSummary struct {
	Version   string    `json:"version"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

type packResponse struct {
	Name        string               `json:"name"`
	Description string               `json:"description"`
	License     string               `json:"license"`
	Versions    []packVersionSummary `json:"versions"`
}

// handleGetPack implements GET /v1/packs/{name}.
func (s *Server) handleGetPack(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	pack, err := s.Store.GetPackByName(r.Context(), name)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, fmt.Sprintf("no pack named %q", name))
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to look up pack")
		return
	}

	versions, err := s.Store.ListVersions(r.Context(), pack.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list versions")
		return
	}

	resp := packResponse{Name: pack.Name, Description: pack.Description, License: pack.License}
	for _, v := range versions {
		resp.Versions = append(resp.Versions, packVersionSummary{Version: v.Version, Status: v.Status, CreatedAt: v.CreatedAt})
	}
	writeJSON(w, http.StatusOK, resp)
}

type searchResult struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// handleSearchPacks implements GET /v1/packs?q=<term>&limit=&offset=.
func (s *Server) handleSearchPacks(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	limit := queryInt(r, "limit", 25)
	offset := queryInt(r, "offset", 0)

	packs, err := s.Store.SearchPacks(r.Context(), query, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "search failed")
		return
	}

	results := make([]searchResult, 0, len(packs))
	for _, p := range packs {
		results = append(results, searchResult{Name: p.Name, Description: p.Description})
	}
	writeJSON(w, http.StatusOK, results)
}

func queryInt(r *http.Request, key string, def int) int {
	v := r.URL.Query().Get(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return def
	}
	return n
}

type getVersionResponse struct {
	Version        string          `json:"version"`
	ChecksumSHA256 string          `json:"checksum_sha256"`
	SizeBytes      int64           `json:"size_bytes"`
	Manifest       json.RawMessage `json:"manifest"`
	DownloadURL    string          `json:"download_url"`
}

// handleGetVersion implements GET /v1/packs/{name}/versions/{version}:
// only ever returns a presigned GET URL for an already-published
// version - a pending (never-completed) upload isn't something a caller
// should be able to download.
func (s *Server) handleGetVersion(w http.ResponseWriter, r *http.Request) {
	name, version := r.PathValue("name"), r.PathValue("version")

	v, err := s.Store.GetVersion(r.Context(), name, version)
	if errors.Is(err, store.ErrNotFound) || (err == nil && v.Status != "published") {
		writeError(w, http.StatusNotFound, fmt.Sprintf("no published version %q of %q", version, name))
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to look up version")
		return
	}

	url, err := s.Objects.PresignGet(r.Context(), v.R2ObjectKey, presignExpiry)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to presign download URL")
		return
	}

	writeJSON(w, http.StatusOK, getVersionResponse{
		Version:        v.Version,
		ChecksumSHA256: v.ChecksumSHA256,
		SizeBytes:      v.SizeBytes,
		Manifest:       json.RawMessage(v.ManifestJSON),
		DownloadURL:    url,
	})
}

type createVersionRequest struct {
	Version        string          `json:"version"`
	Description    string          `json:"description"`
	License        string          `json:"license"`
	ChecksumSHA256 string          `json:"checksum_sha256"`
	SizeBytes      int64           `json:"size_bytes"`
	Manifest       json.RawMessage `json:"manifest"`
}

type createVersionResponse struct {
	UploadURL string    `json:"upload_url"`
	ObjectKey string    `json:"object_key"`
	ExpiresAt time.Time `json:"expires_at"`
}

// handleCreateVersion implements POST /v1/packs/{name}/versions
// (PACKS_PLAN.md §3): creates the pack itself if this is its first
// version (owner = caller); a later version requires the caller to
// already own the pack. Inserts a pending pack_versions row and returns
// a presigned PUT URL - the caller uploads the tarball directly to R2,
// then calls the /complete endpoint.
func (s *Server) handleCreateVersion(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !namePattern.MatchString(name) {
		writeError(w, http.StatusBadRequest, "pack name must match ^[A-Za-z0-9._-]+$")
		return
	}

	var req createVersionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if !namePattern.MatchString(req.Version) {
		writeError(w, http.StatusBadRequest, "version must match ^[A-Za-z0-9._-]+$")
		return
	}
	if req.ChecksumSHA256 == "" || req.SizeBytes <= 0 || len(req.Manifest) == 0 {
		writeError(w, http.StatusBadRequest, "checksum_sha256, size_bytes, and manifest are required")
		return
	}

	user, ok := userFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if !s.publishLimiter.Allow(user.ID) {
		writeError(w, http.StatusTooManyRequests, fmt.Sprintf("publish rate limit exceeded - max %d per %s, try again later", publishRateLimitMax, publishRateLimitWindow))
		return
	}

	pack, err := s.Store.GetPackByName(r.Context(), name)
	switch {
	case errors.Is(err, store.ErrNotFound):
		pack, err = s.Store.CreatePack(r.Context(), name, user.ID, req.Description, req.License)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to create pack")
			return
		}
	case err != nil:
		writeError(w, http.StatusInternalServerError, "failed to look up pack")
		return
	case pack.OwnerUserID != user.ID:
		writeError(w, http.StatusForbidden, "you don't own this pack")
		return
	}

	objectKey := fmt.Sprintf("packs/%s/%s.tar.gz", pack.Name, req.Version)
	v, err := s.Store.UpsertVersionForPublish(r.Context(), pack.ID, req.Version, req.ChecksumSHA256, objectKey, req.SizeBytes, req.Manifest)
	if errors.Is(err, store.ErrConflict) {
		writeError(w, http.StatusConflict, fmt.Sprintf("%s@%s is already published - versions are immutable", name, req.Version))
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to record version")
		return
	}

	uploadURL, err := s.Objects.PresignPut(r.Context(), v.R2ObjectKey, presignExpiry)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to presign upload URL")
		return
	}

	writeJSON(w, http.StatusOK, createVersionResponse{
		UploadURL: uploadURL,
		ObjectKey: v.R2ObjectKey,
		ExpiresAt: time.Now().Add(presignExpiry),
	})
}

// handleCompleteVersion implements POST
// /v1/packs/{name}/versions/{version}/complete: confirms the object the
// caller was told to PUT to actually exists in R2 (a HEAD request, not
// trusting the client's own say-so) before flipping the version to
// published.
func (s *Server) handleCompleteVersion(w http.ResponseWriter, r *http.Request) {
	name, version := r.PathValue("name"), r.PathValue("version")

	v, err := s.Store.GetVersion(r.Context(), name, version)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, fmt.Sprintf("no pending version %q of %q", version, name))
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to look up version")
		return
	}
	if v.Status == "published" {
		writeJSON(w, http.StatusOK, map[string]string{"status": "published"})
		return
	}

	size, err := s.Objects.HeadObject(r.Context(), v.R2ObjectKey)
	if err != nil {
		writeError(w, http.StatusBadRequest, "upload not found yet - PUT the tarball to the upload_url first")
		return
	}
	if size != v.SizeBytes {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("uploaded object is %d bytes, expected %d", size, v.SizeBytes))
		return
	}

	if err := s.Store.PublishVersion(r.Context(), v.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to publish version")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "published"})
}
