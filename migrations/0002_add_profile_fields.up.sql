ALTER TABLE users
    ADD COLUMN username CITEXT UNIQUE,
    ADD COLUMN bio      TEXT,
    ADD COLUMN country  CHAR(2);

CREATE INDEX users_username_idx ON users (username) WHERE username IS NOT NULL;