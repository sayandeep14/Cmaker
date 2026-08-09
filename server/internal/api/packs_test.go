package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"cmaker-packs-server/internal/store"
	"cmaker-packs-server/internal/token"
)

// newTestServer builds a Server wired with a default fakeObjectStore -
// the shared entrypoint every handler test uses, so adding a new
// Server dependency only ever means updating this one helper.
func newTestServer(fs *fakeStore, gh *fakeGitHub) *Server {
	return New(fs, gh, newFakeObjectStore())
}

// fakeObjectStore is an in-memory stand-in for R2 - PresignPut/PresignGet
// return deterministic fake URLs (never dialed out in a handler test),
// and "uploading" is simulated by handler tests calling put() directly on
// the fake to mark an object key as present, exactly mirroring what a
// real PUT to a presigned URL would result in.
type fakeObjectStore struct {
	objects map[string]int64 // key -> size in bytes
}

func newFakeObjectStore() *fakeObjectStore {
	return &fakeObjectStore{objects: map[string]int64{}}
}

func (f *fakeObjectStore) put(key string, size int64) { f.objects[key] = size }

func (f *fakeObjectStore) PresignPut(ctx context.Context, key string, expires time.Duration) (string, error) {
	return "https://fake-r2.example.com/put/" + key, nil
}

func (f *fakeObjectStore) PresignGet(ctx context.Context, key string, expires time.Duration) (string, error) {
	return "https://fake-r2.example.com/get/" + key, nil
}

func (f *fakeObjectStore) HeadObject(ctx context.Context, key string) (int64, error) {
	size, ok := f.objects[key]
	if !ok {
		return 0, fmt.Errorf("object %s not found", key)
	}
	return size, nil
}

// --- fakeStore pack-related state and methods ---

type fakePackVersion = store.PackVersion

func (f *fakeStore) ensurePacks() {
	if f.packs == nil {
		f.packs = map[string]store.Pack{}
	}
	if f.versions == nil {
		f.versions = map[string]map[string]fakePackVersion{}
	}
}

func (f *fakeStore) GetPackByName(ctx context.Context, name string) (store.Pack, error) {
	f.ensurePacks()
	p, ok := f.packs[name]
	if !ok {
		return store.Pack{}, store.ErrNotFound
	}
	return p, nil
}

func (f *fakeStore) CreatePack(ctx context.Context, name string, ownerUserID int64, description, license string) (store.Pack, error) {
	f.ensurePacks()
	f.nextID++
	p := store.Pack{ID: f.nextID, Name: name, OwnerUserID: ownerUserID, Description: description, License: license}
	f.packs[name] = p
	f.versions[name] = map[string]fakePackVersion{}
	return p, nil
}

func (f *fakeStore) UpsertVersionForPublish(ctx context.Context, packID int64, version, checksum, objectKey string, size int64, manifest []byte) (store.PackVersion, error) {
	f.ensurePacks()
	packName := f.packNameByID(packID)
	if existing, ok := f.versions[packName][version]; ok {
		if existing.Status == "published" {
			return store.PackVersion{}, store.ErrConflict
		}
		return existing, nil
	}
	f.nextVersionID++
	v := store.PackVersion{
		ID: f.nextVersionID, PackID: packID, Version: version,
		ChecksumSHA256: checksum, R2ObjectKey: objectKey, SizeBytes: size,
		ManifestJSON: manifest, Status: "pending",
	}
	f.versions[packName][version] = v
	return v, nil
}

func (f *fakeStore) packNameByID(packID int64) string {
	for name, p := range f.packs {
		if p.ID == packID {
			return name
		}
	}
	return ""
}

func (f *fakeStore) GetVersion(ctx context.Context, packName, version string) (store.PackVersion, error) {
	f.ensurePacks()
	vs, ok := f.versions[packName]
	if !ok {
		return store.PackVersion{}, store.ErrNotFound
	}
	v, ok := vs[version]
	if !ok {
		return store.PackVersion{}, store.ErrNotFound
	}
	return v, nil
}

func (f *fakeStore) PublishVersion(ctx context.Context, versionID int64) error {
	f.ensurePacks()
	for packName, vs := range f.versions {
		for ver, v := range vs {
			if v.ID == versionID {
				v.Status = "published"
				f.versions[packName][ver] = v
				return nil
			}
		}
	}
	return store.ErrNotFound
}

func (f *fakeStore) ListVersions(ctx context.Context, packID int64) ([]store.PackVersion, error) {
	f.ensurePacks()
	packName := f.packNameByID(packID)
	var out []store.PackVersion
	for _, v := range f.versions[packName] {
		out = append(out, v)
	}
	return out, nil
}

func (f *fakeStore) SearchPacks(ctx context.Context, query string, limit, offset int) ([]store.Pack, error) {
	f.ensurePacks()
	var out []store.Pack
	for _, p := range f.packs {
		out = append(out, p)
	}
	return out, nil
}

// --- handler tests ---

func loggedInUser(fs *fakeStore, githubID int64, login string) (userID int64, apiToken string) {
	u, _ := fs.UpsertUser(context.Background(), githubID, login, "")
	apiToken = "cmpk_live_" + login
	fs.tokens[token.Hash(apiToken)] = u.ID
	return u.ID, apiToken
}

func TestHandleCreateVersionFirstPublishCreatesPack(t *testing.T) {
	fs := newFakeStore()
	_, tok := loggedInUser(fs, 1, "octocat")
	srv := newTestServer(fs, &fakeGitHub{})

	body, _ := json.Marshal(createVersionRequest{
		Version: "1.0.0", Description: "a pack", License: "MIT",
		ChecksumSHA256: "abc123", SizeBytes: 42, Manifest: json.RawMessage(`{"name":"mypack"}`),
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/packs/mypack/versions", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
	var resp createVersionResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.ObjectKey != "packs/mypack/1.0.0.tar.gz" {
		t.Errorf("ObjectKey = %q, unexpected", resp.ObjectKey)
	}
	if resp.UploadURL == "" {
		t.Error("expected a non-empty upload URL")
	}
}

func TestHandleCreateVersionRejectsNonOwner(t *testing.T) {
	fs := newFakeStore()
	ownerID, _ := loggedInUser(fs, 1, "owner")
	_, otherTok := loggedInUser(fs, 2, "other")
	fs.CreatePack(context.Background(), "mypack", ownerID, "", "")
	srv := newTestServer(fs, &fakeGitHub{})

	body, _ := json.Marshal(createVersionRequest{
		Version: "2.0.0", ChecksumSHA256: "abc", SizeBytes: 1, Manifest: json.RawMessage(`{}`),
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/packs/mypack/versions", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+otherTok)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", w.Code)
	}
}

func TestHandleCreateVersionRejectsRepublishingPublished(t *testing.T) {
	fs := newFakeStore()
	ownerID, tok := loggedInUser(fs, 1, "owner")
	pack, _ := fs.CreatePack(context.Background(), "mypack", ownerID, "", "")
	v, _ := fs.UpsertVersionForPublish(context.Background(), pack.ID, "1.0.0", "abc", "packs/mypack/1.0.0.tar.gz", 1, []byte(`{}`))
	fs.PublishVersion(context.Background(), v.ID)
	srv := newTestServer(fs, &fakeGitHub{})

	body, _ := json.Marshal(createVersionRequest{
		Version: "1.0.0", ChecksumSHA256: "abc", SizeBytes: 1, Manifest: json.RawMessage(`{}`),
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/packs/mypack/versions", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409", w.Code)
	}
}

func TestHandleCreateVersionRejectsBadName(t *testing.T) {
	fs := newFakeStore()
	_, tok := loggedInUser(fs, 1, "octocat")
	srv := newTestServer(fs, &fakeGitHub{})

	body, _ := json.Marshal(createVersionRequest{Version: "not a valid version!", ChecksumSHA256: "a", SizeBytes: 1, Manifest: json.RawMessage(`{}`)})
	req := httptest.NewRequest(http.MethodPost, "/v1/packs/mypack/versions", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestCompletePublishFlow(t *testing.T) {
	fs := newFakeStore()
	ownerID, tok := loggedInUser(fs, 1, "owner")
	pack, _ := fs.CreatePack(context.Background(), "mypack", ownerID, "", "")
	v, _ := fs.UpsertVersionForPublish(context.Background(), pack.ID, "1.0.0", "abc", "packs/mypack/1.0.0.tar.gz", 5, []byte(`{"name":"mypack"}`))
	srv := newTestServer(fs, &fakeGitHub{})
	objs := srv.Objects.(*fakeObjectStore)

	// Downloading before /complete must fail - not published yet.
	reqEarly := httptest.NewRequest(http.MethodGet, "/v1/packs/mypack/versions/1.0.0", nil)
	reqEarly.Header.Set("Authorization", "Bearer "+tok)
	wEarly := httptest.NewRecorder()
	srv.ServeHTTP(wEarly, reqEarly)
	if wEarly.Code != http.StatusNotFound {
		t.Fatalf("pre-complete download: status = %d, want 404", wEarly.Code)
	}

	// /complete before the object actually exists in R2 must fail.
	reqTooEarly := httptest.NewRequest(http.MethodPost, "/v1/packs/mypack/versions/1.0.0/complete", nil)
	reqTooEarly.Header.Set("Authorization", "Bearer "+tok)
	wTooEarly := httptest.NewRecorder()
	srv.ServeHTTP(wTooEarly, reqTooEarly)
	if wTooEarly.Code != http.StatusBadRequest {
		t.Fatalf("complete before upload: status = %d, want 400", wTooEarly.Code)
	}

	// Simulate the CLI's direct PUT to R2 actually landing.
	objs.put(v.R2ObjectKey, 5)

	reqComplete := httptest.NewRequest(http.MethodPost, "/v1/packs/mypack/versions/1.0.0/complete", nil)
	reqComplete.Header.Set("Authorization", "Bearer "+tok)
	wComplete := httptest.NewRecorder()
	srv.ServeHTTP(wComplete, reqComplete)
	if wComplete.Code != http.StatusOK {
		t.Fatalf("complete: status = %d, want 200; body = %s", wComplete.Code, wComplete.Body.String())
	}

	// Now the download endpoint should work.
	reqDownload := httptest.NewRequest(http.MethodGet, "/v1/packs/mypack/versions/1.0.0", nil)
	reqDownload.Header.Set("Authorization", "Bearer "+tok)
	wDownload := httptest.NewRecorder()
	srv.ServeHTTP(wDownload, reqDownload)
	if wDownload.Code != http.StatusOK {
		t.Fatalf("download: status = %d, want 200; body = %s", wDownload.Code, wDownload.Body.String())
	}
	var resp getVersionResponse
	if err := json.Unmarshal(wDownload.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.DownloadURL == "" {
		t.Error("expected a non-empty download URL")
	}
	if resp.ChecksumSHA256 != "abc" {
		t.Errorf("ChecksumSHA256 = %q, want abc", resp.ChecksumSHA256)
	}
}

func TestCompleteVersionSizeMismatchRejected(t *testing.T) {
	fs := newFakeStore()
	ownerID, tok := loggedInUser(fs, 1, "owner")
	pack, _ := fs.CreatePack(context.Background(), "mypack", ownerID, "", "")
	v, _ := fs.UpsertVersionForPublish(context.Background(), pack.ID, "1.0.0", "abc", "packs/mypack/1.0.0.tar.gz", 100, []byte(`{}`))
	srv := newTestServer(fs, &fakeGitHub{})
	objs := srv.Objects.(*fakeObjectStore)
	objs.put(v.R2ObjectKey, 5) // wrong size - claimed 100, actually 5

	req := httptest.NewRequest(http.MethodPost, "/v1/packs/mypack/versions/1.0.0/complete", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for a size mismatch", w.Code)
	}
}

func TestHandleGetPackNotFound(t *testing.T) {
	fs := newFakeStore()
	_, tok := loggedInUser(fs, 1, "octocat")
	srv := newTestServer(fs, &fakeGitHub{})

	req := httptest.NewRequest(http.MethodGet, "/v1/packs/doesnotexist", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestHandleSearchPacksRequiresAuth(t *testing.T) {
	srv := newTestServer(newFakeStore(), &fakeGitHub{})

	req := httptest.NewRequest(http.MethodGet, "/v1/packs?q=json", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 with no Authorization header", w.Code)
	}
}

func TestHandleSearchPacksFindsMatch(t *testing.T) {
	fs := newFakeStore()
	ownerID, tok := loggedInUser(fs, 1, "octocat")
	fs.CreatePack(context.Background(), "json-helpers", ownerID, "small json utils", "MIT")
	srv := newTestServer(fs, &fakeGitHub{})

	req := httptest.NewRequest(http.MethodGet, "/v1/packs?q=json", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
	var results []searchResult
	if err := json.Unmarshal(w.Body.Bytes(), &results); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(results) != 1 || results[0].Name != "json-helpers" {
		t.Errorf("results = %+v, want [json-helpers]", results)
	}
}
