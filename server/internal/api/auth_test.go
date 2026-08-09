package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"cmaker-packs-server/internal/githubapi"
	"cmaker-packs-server/internal/store"
	"cmaker-packs-server/internal/token"
)

// fakeStore is a minimal in-memory Store for handler tests - no real
// database involved, matching the root cmaker CLI's own fakeCompleter
// convention for testing external-dependency-shaped code.
type fakeStore struct {
	allowlisted map[string]bool
	users       map[int64]store.User // by github ID
	nextID      int64
	tokens      map[string]int64 // token hash -> user ID
	revoked     map[string]bool

	// Pack-related state (see packs_test.go) - lazily initialized via
	// ensurePacks() so newFakeStore() doesn't need to know about M2 at
	// all.
	packs         map[string]store.Pack                 // by name
	versions      map[string]map[string]fakePackVersion // pack name -> version -> row
	nextVersionID int64
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		allowlisted: map[string]bool{},
		users:       map[int64]store.User{},
		tokens:      map[string]int64{},
		revoked:     map[string]bool{},
	}
}

func (f *fakeStore) IsAllowlisted(ctx context.Context, githubLogin string) (bool, error) {
	return f.allowlisted[githubLogin], nil
}

func (f *fakeStore) UpsertUser(ctx context.Context, githubID int64, githubLogin, avatarURL string) (store.User, error) {
	if u, ok := f.users[githubID]; ok {
		u.GitHubLogin = githubLogin
		u.AvatarURL = avatarURL
		f.users[githubID] = u
		return u, nil
	}
	f.nextID++
	u := store.User{ID: f.nextID, GitHubID: githubID, GitHubLogin: githubLogin, AvatarURL: avatarURL}
	f.users[githubID] = u
	return u, nil
}

func (f *fakeStore) CreateToken(ctx context.Context, userID int64, tokenHash string) error {
	f.tokens[tokenHash] = userID
	return nil
}

func (f *fakeStore) UserByTokenHash(ctx context.Context, tokenHash string) (store.User, error) {
	if f.revoked[tokenHash] {
		return store.User{}, store.ErrNotFound
	}
	userID, ok := f.tokens[tokenHash]
	if !ok {
		return store.User{}, store.ErrNotFound
	}
	for _, u := range f.users {
		if u.ID == userID {
			return u, nil
		}
	}
	return store.User{}, store.ErrNotFound
}

func (f *fakeStore) RevokeToken(ctx context.Context, tokenHash string) error {
	f.revoked[tokenHash] = true
	return nil
}

// fakeGitHub is a fake githubapi.Verifier.
type fakeGitHub struct {
	users map[string]githubapi.User // github token -> user
}

func (f *fakeGitHub) VerifyToken(ctx context.Context, githubToken string) (githubapi.User, error) {
	u, ok := f.users[githubToken]
	if !ok {
		return githubapi.User{}, errors.New("invalid token")
	}
	return u, nil
}

func TestHandleLoginSuccess(t *testing.T) {
	fs := newFakeStore()
	fs.allowlisted["octocat"] = true
	gh := &fakeGitHub{users: map[string]githubapi.User{
		"gh_tok": {ID: 42, Login: "octocat", AvatarURL: "https://example.com/a.png"},
	}}
	srv := newTestServer(fs, gh)

	body, _ := json.Marshal(loginRequest{GitHubToken: "gh_tok"})
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/login", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
	var resp loginResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Token == "" {
		t.Error("expected a non-empty token")
	}
	if resp.User.Login != "octocat" || resp.User.ID != 42 {
		t.Errorf("resp.User = %+v, unexpected", resp.User)
	}
}

func TestHandleLoginRejectsNotAllowlisted(t *testing.T) {
	fs := newFakeStore() // nothing allowlisted
	gh := &fakeGitHub{users: map[string]githubapi.User{
		"gh_tok": {ID: 42, Login: "notinvited"},
	}}
	srv := newTestServer(fs, gh)

	body, _ := json.Marshal(loginRequest{GitHubToken: "gh_tok"})
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/login", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", w.Code)
	}
}

func TestHandleLoginRejectsInvalidGitHubToken(t *testing.T) {
	fs := newFakeStore()
	gh := &fakeGitHub{users: map[string]githubapi.User{}}
	srv := newTestServer(fs, gh)

	body, _ := json.Marshal(loginRequest{GitHubToken: "bogus"})
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/login", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestHandleLoginRejectsEmptyBody(t *testing.T) {
	fs := newFakeStore()
	gh := &fakeGitHub{}
	srv := newTestServer(fs, gh)

	req := httptest.NewRequest(http.MethodPost, "/v1/auth/login", bytes.NewReader([]byte(`{}`)))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestHandleWhoamiRequiresAuth(t *testing.T) {
	srv := newTestServer(newFakeStore(), &fakeGitHub{})

	req := httptest.NewRequest(http.MethodGet, "/v1/auth/whoami", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 with no Authorization header", w.Code)
	}
}

func TestHandleWhoamiWithValidToken(t *testing.T) {
	fs := newFakeStore()
	fs.users[42] = store.User{ID: 1, GitHubID: 42, GitHubLogin: "octocat"}
	tok := "cmpk_live_test"
	fs.tokens[token.Hash(tok)] = 1
	srv := newTestServer(fs, &fakeGitHub{})

	req := httptest.NewRequest(http.MethodGet, "/v1/auth/whoami", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
	var resp userResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Login != "octocat" {
		t.Errorf("resp.Login = %q, want octocat", resp.Login)
	}
}

func TestHandleWhoamiRejectsRevokedToken(t *testing.T) {
	fs := newFakeStore()
	fs.users[42] = store.User{ID: 1, GitHubID: 42, GitHubLogin: "octocat"}
	tok := "cmpk_live_test"
	fs.tokens[token.Hash(tok)] = 1
	fs.revoked[token.Hash(tok)] = true
	srv := newTestServer(fs, &fakeGitHub{})

	req := httptest.NewRequest(http.MethodGet, "/v1/auth/whoami", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 for a revoked token", w.Code)
	}
}

func TestHandleLogoutRevokesToken(t *testing.T) {
	fs := newFakeStore()
	fs.users[42] = store.User{ID: 1, GitHubID: 42, GitHubLogin: "octocat"}
	tok := "cmpk_live_test"
	fs.tokens[token.Hash(tok)] = 1
	srv := newTestServer(fs, &fakeGitHub{})

	req := httptest.NewRequest(http.MethodDelete, "/v1/auth/token", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
	if !fs.revoked[token.Hash(tok)] {
		t.Error("expected the token to be revoked after logout")
	}

	// The same token must no longer authenticate anything.
	req2 := httptest.NewRequest(http.MethodGet, "/v1/auth/whoami", nil)
	req2.Header.Set("Authorization", "Bearer "+tok)
	w2 := httptest.NewRecorder()
	srv.ServeHTTP(w2, req2)
	if w2.Code != http.StatusUnauthorized {
		t.Errorf("whoami after logout: status = %d, want 401", w2.Code)
	}
}

func TestHandleHealthz(t *testing.T) {
	srv := newTestServer(newFakeStore(), &fakeGitHub{})

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}
