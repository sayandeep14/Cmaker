// Package store is the SQL access layer for M1's three tables
// (allowlist, users, api_tokens) - raw SQL via pgx, no ORM, matching
// PACKS_PLAN.md §7's stated approach.
package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound is returned by lookups that find nothing - callers branch
// on it (e.g. "not on the allowlist" vs "a real database error") rather
// than string-matching pgx.ErrNoRows directly, keeping that dependency
// out of every caller.
var ErrNotFound = errors.New("not found")

type User struct {
	ID          int64
	GitHubID    int64
	GitHubLogin string
	AvatarURL   string
	CreatedAt   time.Time
}

// Store wraps a pgxpool.Pool with the specific queries M1's auth flow
// needs - a thin struct, not an interface, since there's exactly one
// real implementation and handler tests use a small hand-rolled fake
// (see internal/api's own test file) rather than mocking this directly.
type Store struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// IsAllowlisted reports whether githubLogin (case-insensitive) has an
// allowlist entry.
func (s *Store) IsAllowlisted(ctx context.Context, githubLogin string) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM allowlist WHERE lower(github_login) = lower($1))`,
		githubLogin,
	).Scan(&exists)
	return exists, err
}

// UpsertUser inserts a user row for (githubID, githubLogin, avatarURL) if
// none exists yet, or updates the login/avatar/last_login_at of the
// existing one (a GitHub login can change; github_id is the stable
// identity) - returns the resulting row either way.
func (s *Store) UpsertUser(ctx context.Context, githubID int64, githubLogin, avatarURL string) (User, error) {
	var u User
	err := s.pool.QueryRow(ctx, `
		INSERT INTO users (github_id, github_login, avatar_url)
		VALUES ($1, $2, $3)
		ON CONFLICT (github_id) DO UPDATE
			SET github_login = EXCLUDED.github_login,
			    avatar_url = EXCLUDED.avatar_url,
			    last_login_at = now()
		RETURNING id, github_id, github_login, avatar_url, created_at
	`, githubID, githubLogin, avatarURL).Scan(&u.ID, &u.GitHubID, &u.GitHubLogin, &u.AvatarURL, &u.CreatedAt)
	return u, err
}

// CreateToken records tokenHash as a new, live token for userID.
func (s *Store) CreateToken(ctx context.Context, userID int64, tokenHash string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO api_tokens (user_id, token_hash) VALUES ($1, $2)`,
		userID, tokenHash,
	)
	return err
}

// UserByTokenHash resolves a live (not revoked) token's owning user, and
// best-effort bumps last_used_at - ErrNotFound for an unknown or revoked
// token, so callers (the auth middleware) treat both identically as "not
// authenticated" without distinguishing why.
func (s *Store) UserByTokenHash(ctx context.Context, tokenHash string) (User, error) {
	var u User
	err := s.pool.QueryRow(ctx, `
		SELECT u.id, u.github_id, u.github_login, u.avatar_url, u.created_at
		FROM api_tokens t
		JOIN users u ON u.id = t.user_id
		WHERE t.token_hash = $1 AND t.revoked_at IS NULL
	`, tokenHash).Scan(&u.ID, &u.GitHubID, &u.GitHubLogin, &u.AvatarURL, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, err
	}

	// Best-effort: a failure to record last_used_at shouldn't fail the
	// request the token just successfully authenticated.
	_, _ = s.pool.Exec(ctx, `UPDATE api_tokens SET last_used_at = now() WHERE token_hash = $1`, tokenHash)
	return u, nil
}

// RevokeToken marks tokenHash revoked (cmaker logout) - a no-op, not an
// error, if it was already revoked or never existed, since the caller's
// intent ("this token should not work") is satisfied either way.
func (s *Store) RevokeToken(ctx context.Context, tokenHash string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE api_tokens SET revoked_at = now() WHERE token_hash = $1 AND revoked_at IS NULL`,
		tokenHash,
	)
	return err
}
