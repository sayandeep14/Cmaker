package api

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"cmaker-packs-server/internal/store"
	"cmaker-packs-server/internal/token"
)

type contextKey int

const userContextKey contextKey = 0

// requireAuth wraps next so it only ever runs for a request bearing a
// live "Authorization: Bearer <token>" header, resolving it to the
// owning store.User and stashing it in the request context (see
// userFromContext) - every non-login, non-healthz endpoint uses this.
func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		tok, ok := strings.CutPrefix(authHeader, "Bearer ")
		if !ok || tok == "" {
			writeError(w, http.StatusUnauthorized, "missing or malformed Authorization header")
			return
		}

		user, err := s.Store.UserByTokenHash(r.Context(), token.Hash(tok))
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusUnauthorized, "invalid or revoked token")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to verify token")
			return
		}

		ctx := context.WithValue(r.Context(), userContextKey, user)
		next(w, r.WithContext(ctx))
	}
}

// userFromContext retrieves the store.User requireAuth already resolved
// and attached - handlers reachable only through requireAuth can assume
// this always succeeds.
func userFromContext(ctx context.Context) (store.User, bool) {
	u, ok := ctx.Value(userContextKey).(store.User)
	return u, ok
}
