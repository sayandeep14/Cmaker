// Package api implements the HTTP surface described in PACKS_PLAN.md §3:
// health check, the three auth endpoints (M1), and packs
// publish/download/search (M2).
package api

import (
	"context"
	"net/http"
	"time"

	"cmaker-packs-server/internal/githubapi"
	"cmaker-packs-server/internal/store"
)

// Store is the narrow slice of *store.Store this package actually calls -
// declared here (the consumer), satisfied structurally by the concrete
// type, so handler tests can supply a small fake instead of a real
// database. Mirrors the root cmaker CLI's own "per-consumer interface"
// convention for external dependencies (its Completer interfaces).
type Store interface {
	IsAllowlisted(ctx context.Context, githubLogin string) (bool, error)
	UpsertUser(ctx context.Context, githubID int64, githubLogin, avatarURL string) (store.User, error)
	CreateToken(ctx context.Context, userID int64, tokenHash string) error
	UserByTokenHash(ctx context.Context, tokenHash string) (store.User, error)
	RevokeToken(ctx context.Context, tokenHash string) error

	GetPackByName(ctx context.Context, name string) (store.Pack, error)
	CreatePack(ctx context.Context, name string, ownerUserID int64, description, license string) (store.Pack, error)
	UpsertVersionForPublish(ctx context.Context, packID int64, version, checksumSHA256, objectKey string, sizeBytes int64, manifestJSON []byte) (store.PackVersion, error)
	GetVersion(ctx context.Context, packName, version string) (store.PackVersion, error)
	PublishVersion(ctx context.Context, versionID int64) error
	ListVersions(ctx context.Context, packID int64) ([]store.PackVersion, error)
	SearchPacks(ctx context.Context, query string, limit, offset int) ([]store.Pack, error)
}

// ObjectStore is the narrow slice of *objectstore.Client the publish/
// download handlers need - same per-consumer-interface reasoning as
// Store above.
type ObjectStore interface {
	PresignPut(ctx context.Context, key string, expires time.Duration) (string, error)
	PresignGet(ctx context.Context, key string, expires time.Duration) (string, error)
	HeadObject(ctx context.Context, key string) (int64, error)
}

// Server holds every handler's dependencies.
type Server struct {
	Store          Store
	GitHub         githubapi.Verifier
	Objects        ObjectStore
	Mux            *http.ServeMux
	publishLimiter *publishRateLimiter

	// handler is Mux wrapped with loggingMiddleware - built once in New,
	// not on every ServeHTTP call.
	handler http.Handler
}

// New builds a Server with its routes registered.
func New(st Store, gh githubapi.Verifier, objects ObjectStore) *Server {
	s := &Server{Store: st, GitHub: gh, Objects: objects, Mux: http.NewServeMux(), publishLimiter: newPublishRateLimiter()}
	s.routes()
	s.handler = loggingMiddleware(s.Mux)
	return s
}

func (s *Server) routes() {
	s.Mux.HandleFunc("GET /healthz", s.handleHealthz)
	s.Mux.HandleFunc("POST /v1/auth/login", s.handleLogin)
	s.Mux.HandleFunc("GET /v1/auth/whoami", s.requireAuth(s.handleWhoami))
	s.Mux.HandleFunc("DELETE /v1/auth/token", s.requireAuth(s.handleLogout))

	s.Mux.HandleFunc("GET /v1/packs", s.requireAuth(s.handleSearchPacks))
	s.Mux.HandleFunc("GET /v1/packs/{name}", s.requireAuth(s.handleGetPack))
	s.Mux.HandleFunc("GET /v1/packs/{name}/versions/{version}", s.requireAuth(s.handleGetVersion))
	s.Mux.HandleFunc("POST /v1/packs/{name}/versions", s.requireAuth(s.handleCreateVersion))
	s.Mux.HandleFunc("POST /v1/packs/{name}/versions/{version}/complete", s.requireAuth(s.handleCompleteVersion))
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.handler.ServeHTTP(w, r)
}
