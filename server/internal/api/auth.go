package api

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"cmaker-packs-server/internal/token"
)

type loginRequest struct {
	GitHubToken string `json:"github_token"`
}

type userResponse struct {
	Login string `json:"login"`
	ID    int64  `json:"id"`
}

type loginResponse struct {
	Token string       `json:"token"`
	User  userResponse `json:"user"`
}

// handleLogin implements POST /v1/auth/login (PACKS_PLAN.md §3): verify
// the given GitHub token against GitHub's own API, reject anyone not on
// the allowlist, upsert the user row, mint a fresh opaque token, and
// return it - the GitHub token itself is never stored, only used for
// this one identity check.
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.GitHubToken == "" {
		writeError(w, http.StatusBadRequest, "github_token is required")
		return
	}

	ghUser, err := s.GitHub.VerifyToken(r.Context(), req.GitHubToken)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "GitHub token could not be verified")
		return
	}

	allowed, err := s.Store.IsAllowlisted(r.Context(), ghUser.Login)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to check allowlist")
		return
	}
	if !allowed {
		writeError(w, http.StatusForbidden, "this GitHub account isn't invited yet")
		return
	}

	user, err := s.Store.UpsertUser(r.Context(), ghUser.ID, ghUser.Login, ghUser.AvatarURL)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to record user")
		return
	}

	newToken, err := token.Generate()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate token")
		return
	}
	if err := s.Store.CreateToken(r.Context(), user.ID, token.Hash(newToken)); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to store token")
		return
	}

	writeJSON(w, http.StatusOK, loginResponse{
		Token: newToken,
		User:  userResponse{Login: user.GitHubLogin, ID: user.GitHubID},
	})
}

// handleWhoami implements GET /v1/auth/whoami - reachable only through
// requireAuth, so userFromContext always succeeds here.
func (s *Server) handleWhoami(w http.ResponseWriter, r *http.Request) {
	user, ok := userFromContext(r.Context())
	if !ok {
		log.Printf("handleWhoami reached without an authenticated user in context - requireAuth wiring bug")
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, userResponse{Login: user.GitHubLogin, ID: user.GitHubID})
}

// handleLogout implements DELETE /v1/auth/token: revokes the exact token
// this request authenticated with (never asks for a token in the body -
// you can only ever revoke the one you're currently using).
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	tok, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if err := s.Store.RevokeToken(r.Context(), token.Hash(tok)); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to revoke token")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}
