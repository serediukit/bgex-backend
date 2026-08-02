ALTER TABLE users
    ADD COLUMN role TEXT NOT NULL DEFAULT 'user'
        CHECK (role IN ('user','admin'));
CREATE INDEX users_admin_idx ON users (role) WHERE role = 'admin';
