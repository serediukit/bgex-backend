CREATE TABLE friend_requests (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    requester_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    addressee_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    status       TEXT NOT NULL DEFAULT 'pending'
                     CHECK (status IN ('pending', 'accepted', 'declined')),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT friend_requests_no_self_friend CHECK (requester_id != addressee_id)
);

-- Canonical pair index prevents A→B and B→A duplicates simultaneously
CREATE UNIQUE INDEX friend_requests_canonical_pair_idx ON friend_requests (
    LEAST(requester_id::text, addressee_id::text),
    GREATEST(requester_id::text, addressee_id::text)
);
CREATE INDEX friend_requests_addressee_pending_idx ON friend_requests (addressee_id) WHERE status = 'pending';
CREATE INDEX friend_requests_requester_idx ON friend_requests (requester_id, status);
