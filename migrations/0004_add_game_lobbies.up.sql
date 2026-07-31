-- Game-agnostic lobby + seat tables. These live in the default schema and are
-- shared by every game; per-game hot state lives in its own schema (see 0005).

CREATE TABLE game_lobbies (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    game_key   TEXT NOT NULL,
    name       TEXT NOT NULL,
    host_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    status     TEXT NOT NULL DEFAULT 'waiting'
                   CHECK (status IN ('waiting', 'in_progress', 'finished')),
    max_seats  INT  NOT NULL CHECK (max_seats BETWEEN 2 AND 10),
    config     JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX game_lobbies_open_idx ON game_lobbies (game_key, status);

CREATE TABLE game_seats (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    lobby_id   UUID NOT NULL REFERENCES game_lobbies(id) ON DELETE CASCADE,
    seat_index INT  NOT NULL,
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    status     TEXT NOT NULL DEFAULT 'active'
                   CHECK (status IN ('active', 'left')),
    stack      BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT game_seats_seat_range CHECK (seat_index >= 0)
);

-- At most one player per physical seat in a lobby.
CREATE UNIQUE INDEX game_seats_lobby_seat_idx
    ON game_seats (lobby_id, seat_index) WHERE status = 'active';

-- Core rule: a user may hold exactly ONE active seat across the whole platform,
-- i.e. play only one lobby at a time and occupy only one place in it.
CREATE UNIQUE INDEX game_seats_one_active_per_user_idx
    ON game_seats (user_id) WHERE status = 'active';

CREATE INDEX game_seats_lobby_idx ON game_seats (lobby_id);
