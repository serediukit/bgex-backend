CREATE SCHEMA IF NOT EXISTS ttr;

-- A board definition. Versions are immutable once published; a lobby pins
-- (map_id, version) at Start so later edits can't affect a running game.
CREATE TABLE ttr.maps (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    slug         TEXT NOT NULL UNIQUE,
    name         TEXT NOT NULL,
    created_by   UUID REFERENCES users(id) ON DELETE SET NULL,
    is_official  BOOLEAN NOT NULL DEFAULT FALSE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE ttr.map_versions (
    map_id       UUID NOT NULL REFERENCES ttr.maps(id) ON DELETE CASCADE,
    version      INT  NOT NULL CHECK (version >= 1),
    status       TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','published')),
    doc          JSONB NOT NULL,
    doc_sha256   TEXT NOT NULL,
    published_at TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (map_id, version)
);
CREATE INDEX ttr_map_versions_published_idx
    ON ttr.map_versions (map_id, version DESC) WHERE status = 'published';

-- Content-addressed background images (plan Q7). 4 MB cap enforced in Go.
CREATE TABLE ttr.map_assets (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    mime       TEXT NOT NULL CHECK (mime IN ('image/png','image/jpeg','image/webp')),
    byte_size  INT  NOT NULL CHECK (byte_size > 0 AND byte_size <= 4194304),
    sha256     TEXT NOT NULL UNIQUE,
    bytes      BYTEA NOT NULL,
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Hot state: protobuf blob, Postgres is the source of truth. Every mutation
-- locks this row FOR UPDATE (same contract as poker.game_states).
CREATE TABLE ttr.game_states (
    lobby_id    UUID PRIMARY KEY REFERENCES game_lobbies(id) ON DELETE CASCADE,
    map_id      UUID NOT NULL,
    map_version INT  NOT NULL,
    state       BYTEA NOT NULL,
    version     INT  NOT NULL DEFAULT 0,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    FOREIGN KEY (map_id, map_version) REFERENCES ttr.map_versions (map_id, version)
);

CREATE TABLE ttr.action_log (
    lobby_id   UUID NOT NULL REFERENCES game_lobbies(id) ON DELETE CASCADE,
    seq        BIGINT NOT NULL,
    user_id    UUID REFERENCES users(id) ON DELETE SET NULL,
    action     JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (lobby_id, seq)
);

CREATE TABLE ttr.game_results (
    lobby_id    UUID NOT NULL REFERENCES game_lobbies(id) ON DELETE CASCADE,
    seat_index  INT  NOT NULL,
    user_id     UUID REFERENCES users(id) ON DELETE SET NULL,
    total       INT  NOT NULL,
    rank        INT  NOT NULL,
    breakdown   JSONB NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (lobby_id, seat_index)
);
CREATE INDEX ttr_game_results_user_idx ON ttr.game_results (user_id);
