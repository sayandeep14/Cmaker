package packclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// WhoamiResponse is GET /v1/auth/whoami's response shape.
type WhoamiResponse struct {
	Login string `json:"login"`
	ID    int64  `json:"id"`
}

func (c *Client) Whoami(ctx context.Context) (WhoamiResponse, error) {
	var resp WhoamiResponse
	err := c.doJSON(ctx, http.MethodGet, "/v1/auth/whoami", nil, &resp)
	return resp, err
}

// Logout revokes the token this Client is currently using.
func (c *Client) Logout(ctx context.Context) error {
	return c.doJSON(ctx, http.MethodDelete, "/v1/auth/token", nil, nil)
}

// SearchResult is one entry in GET /v1/packs?q=...'s response.
type SearchResult struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

func (c *Client) Search(ctx context.Context, query string) ([]SearchResult, error) {
	var results []SearchResult
	path := "/v1/packs?q=" + url.QueryEscape(query)
	err := c.doJSON(ctx, http.MethodGet, path, nil, &results)
	return results, err
}

type PackVersionSummary struct {
	Version   string    `json:"version"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

// PackInfo is GET /v1/packs/{name}'s response shape.
type PackInfo struct {
	Name        string               `json:"name"`
	Description string               `json:"description"`
	License     string               `json:"license"`
	Versions    []PackVersionSummary `json:"versions"`
}

func (c *Client) GetPack(ctx context.Context, name string) (PackInfo, error) {
	var info PackInfo
	err := c.doJSON(ctx, http.MethodGet, "/v1/packs/"+url.PathEscape(name), nil, &info)
	return info, err
}

// VersionInfo is GET /v1/packs/{name}/versions/{version}'s response
// shape - DownloadURL is a short-lived presigned R2 URL (see
// DownloadTarball).
type VersionInfo struct {
	Version        string          `json:"version"`
	ChecksumSHA256 string          `json:"checksum_sha256"`
	SizeBytes      int64           `json:"size_bytes"`
	Manifest       json.RawMessage `json:"manifest"`
	DownloadURL    string          `json:"download_url"`
}

func (c *Client) GetVersion(ctx context.Context, name, version string) (VersionInfo, error) {
	var v VersionInfo
	path := fmt.Sprintf("/v1/packs/%s/versions/%s", url.PathEscape(name), url.PathEscape(version))
	err := c.doJSON(ctx, http.MethodGet, path, nil, &v)
	return v, err
}

// CreateVersionRequest is POST /v1/packs/{name}/versions' request body.
type CreateVersionRequest struct {
	Version        string          `json:"version"`
	Description    string          `json:"description"`
	License        string          `json:"license"`
	ChecksumSHA256 string          `json:"checksum_sha256"`
	SizeBytes      int64           `json:"size_bytes"`
	Manifest       json.RawMessage `json:"manifest"`
}

// CreateVersionResponse carries the presigned R2 PUT URL to upload the
// tarball to directly (see UploadTarball) - the API server itself never
// sees the file bytes.
type CreateVersionResponse struct {
	UploadURL string    `json:"upload_url"`
	ObjectKey string    `json:"object_key"`
	ExpiresAt time.Time `json:"expires_at"`
}

func (c *Client) CreateVersion(ctx context.Context, packName string, req CreateVersionRequest) (CreateVersionResponse, error) {
	var resp CreateVersionResponse
	err := c.doJSON(ctx, http.MethodPost, "/v1/packs/"+url.PathEscape(packName)+"/versions", req, &resp)
	return resp, err
}

// CompleteUpload tells the server the tarball was PUT successfully - it
// HEADs the R2 object itself to confirm before flipping the version to
// published (see server/internal/api/packs.go's handleCompleteVersion).
func (c *Client) CompleteUpload(ctx context.Context, packName, version string) error {
	path := fmt.Sprintf("/v1/packs/%s/versions/%s/complete", url.PathEscape(packName), url.PathEscape(version))
	return c.doJSON(ctx, http.MethodPost, path, nil, nil)
}

// UploadTarball PUTs data directly to a presigned R2 URL - bypasses the
// API server entirely (PACKS_PLAN.md §1), so this doesn't go through
// doJSON/BaseURL at all.
func (c *Client) UploadTarball(ctx context.Context, uploadURL string, data []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, uploadURL, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to build upload request: %w", err)
	}
	req.Header.Set("Content-Type", "application/gzip")
	req.ContentLength = int64(len(data))

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to upload: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("upload failed with status %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

// DownloadTarball GETs directly from a presigned R2 URL.
func (c *Client) DownloadTarball(ctx context.Context, downloadURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build download request: %w", err)
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download failed with status %d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read downloaded data: %w", err)
	}
	return data, nil
}
