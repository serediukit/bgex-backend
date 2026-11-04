CREATE SCHEMA IF NOT EXISTS song_guess;

-- A pack of songs, grouped into round-select cells. author_label is the
-- pack document's own free-text "packAuthor" credit and is distinct from
-- created_by, which is the platform account with write access.
CREATE TABLE song_guess.packs (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    slug         TEXT NOT NULL UNIQUE,
    name         TEXT NOT NULL,
    author_label TEXT NOT NULL DEFAULT '',
    created_by   UUID REFERENCES users(id) ON DELETE SET NULL,
    is_official  BOOLEAN NOT NULL DEFAULT FALSE,
    is_public    BOOLEAN NOT NULL DEFAULT TRUE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Versions are immutable once published; a lobby pins (pack_id, version) at
-- Start so later edits can't affect a running game. total_songs is denormalized
-- from doc so the round-select table's parity check doesn't require a parse.
CREATE TABLE song_guess.pack_versions (
    pack_id      UUID NOT NULL REFERENCES song_guess.packs(id) ON DELETE CASCADE,
    version      INT  NOT NULL CHECK (version >= 1),
    status       TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','published')),
    doc          JSONB NOT NULL,
    doc_sha256   TEXT NOT NULL,
    validated    BOOLEAN NOT NULL DEFAULT TRUE,
    total_songs  INT  NOT NULL DEFAULT 0,
    published_at TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (pack_id, version)
);
CREATE INDEX song_guess_pack_versions_published_idx
    ON song_guess.pack_versions (pack_id, version DESC) WHERE status = 'published';

-- Hot state: protobuf blob, Postgres is the source of truth. Every mutation
-- locks this row FOR UPDATE (same contract as poker.game_states / ttr.game_states).
-- The composite FK below is the mechanism enforcing the "pin can never dangle"
-- requirement: a lobby can only reference a pack_versions row that actually exists.
CREATE TABLE song_guess.game_states (
    lobby_id     UUID PRIMARY KEY REFERENCES game_lobbies(id) ON DELETE CASCADE,
    pack_id      UUID NOT NULL,
    pack_version INT  NOT NULL,
    state        BYTEA NOT NULL,
    version      INT  NOT NULL DEFAULT 0,
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    FOREIGN KEY (pack_id, pack_version) REFERENCES song_guess.pack_versions (pack_id, version)
);

CREATE TABLE song_guess.action_log (
    lobby_id   UUID NOT NULL REFERENCES game_lobbies(id) ON DELETE CASCADE,
    seq        BIGINT NOT NULL,
    user_id    UUID REFERENCES users(id) ON DELETE SET NULL,
    action     JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (lobby_id, seq)
);

-- Final per-participant scoring breakdown. One row per seated participant,
-- including the judge (team = 'none'), one row per finished lobby.
CREATE TABLE song_guess.game_results (
    lobby_id   UUID NOT NULL REFERENCES game_lobbies(id) ON DELETE CASCADE,
    seat_index INT  NOT NULL,
    user_id    UUID REFERENCES users(id) ON DELETE SET NULL,
    team       TEXT NOT NULL CHECK (team IN ('none','a','b')),
    total      INT  NOT NULL,
    rank       INT  NOT NULL,
    is_judge   BOOLEAN NOT NULL DEFAULT FALSE,
    breakdown  JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (lobby_id, seat_index)
);
CREATE INDEX song_guess_game_results_user_idx ON song_guess.game_results (user_id);
