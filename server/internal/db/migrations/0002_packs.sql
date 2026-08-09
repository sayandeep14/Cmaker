-- M2 schema: packs and their versions (see PACKS_PLAN.md §2). Version
-- rows are immutable once published; dependencies live inside
-- manifest_json rather than a normalized table for the POC.

CREATE TABLE packs (
    id            BIGSERIAL PRIMARY KEY,
    name          TEXT NOT NULL UNIQUE,
    owner_user_id BIGINT NOT NULL REFERENCES users (id),
    description   TEXT,
    license       TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Postgres's own UNIQUE on name is already case-sensitive; this index is
-- what actually enforces "case-insensitive unique" (e.g. "Foo" and "foo"
-- can't both exist), matching PACKS_PLAN.md §2's stated requirement.
CREATE UNIQUE INDEX idx_packs_name_lower ON packs (lower(name));

CREATE TABLE pack_versions (
    id              BIGSERIAL PRIMARY KEY,
    pack_id         BIGINT NOT NULL REFERENCES packs (id),
    version         TEXT NOT NULL,
    checksum_sha256 TEXT NOT NULL,
    r2_object_key   TEXT NOT NULL,
    size_bytes      BIGINT NOT NULL,
    manifest_json   JSONB NOT NULL,
    status          TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'published')),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (pack_id, version)
);

CREATE INDEX idx_pack_versions_pack_id ON pack_versions (pack_id);
