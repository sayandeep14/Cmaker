-- M1 schema: allowlist-gated login and opaque API tokens. packs/pack_versions
-- land in a later migration when M2 actually needs them (see PACKS_PLAN.md).

CREATE TABLE allowlist (
    github_login TEXT PRIMARY KEY,
    added_by     TEXT NOT NULL,
    added_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    note         TEXT
);

CREATE TABLE users (
    id            BIGSERIAL PRIMARY KEY,
    github_id     BIGINT NOT NULL UNIQUE,
    github_login  TEXT NOT NULL,
    avatar_url    TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_login_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE api_tokens (
    id           BIGSERIAL PRIMARY KEY,
    user_id      BIGINT NOT NULL REFERENCES users(id),
    token_hash   TEXT NOT NULL UNIQUE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used_at TIMESTAMPTZ,
    revoked_at   TIMESTAMPTZ
);

CREATE INDEX idx_api_tokens_user_id ON api_tokens (user_id);
