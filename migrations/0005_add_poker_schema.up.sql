-- Poker keeps its hot, in-play state as a protobuf-serialized blob in its own
-- schema. Postgres is the source of truth: each action locks game_states.state
-- FOR UPDATE, mutates, and writes it back. Completed hands are archived in
-- hand_history for audit/replay.

CREATE SCHEMA IF NOT EXISTS poker;

CREATE TABLE poker.game_states (
    lobby_id   UUID PRIMARY KEY REFERENCES game_lobbies(id) ON DELETE CASCADE,
    state      BYTEA NOT NULL,
    hand_no    INT NOT NULL DEFAULT 0,
    version    INT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE poker.hand_history (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    lobby_id   UUID NOT NULL REFERENCES game_lobbies(id) ON DELETE CASCADE,
    hand_no    INT NOT NULL,
    result     BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX poker_hand_history_lobby_idx ON poker.hand_history (lobby_id, hand_no);
