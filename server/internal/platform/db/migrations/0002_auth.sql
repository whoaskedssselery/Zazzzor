-- Accounts and cookie sessions, owned by the auth module.

-- +goose Up
CREATE TABLE users (
    id            bigint PRIMARY KEY,
    -- Stored lower-cased, so the unique index is case-insensitive.
    username      text NOT NULL UNIQUE CHECK (username ~ '^[a-z0-9_.]{2,32}$'),
    display_name  text NOT NULL,
    -- Argon2id PHC string; never leaves the auth module.
    password_hash text NOT NULL,
    avatar_hash   text CHECK (avatar_hash ~ '^[0-9a-f]{16}$'),
    disabled_at   timestamptz,
    created_at    timestamptz NOT NULL
);

CREATE TABLE sessions (
    id           bigint PRIMARY KEY,
    user_id      bigint NOT NULL REFERENCES users ON DELETE CASCADE,
    -- SHA-256 of the cookie token; the token itself is never stored.
    token_hash   bytea NOT NULL UNIQUE CHECK (length(token_hash) = 32),
    created_at   timestamptz NOT NULL,
    last_used_at timestamptz NOT NULL,
    expires_at   timestamptz NOT NULL,
    user_agent   text,
    ip           inet
);

CREATE INDEX sessions_user_id_idx ON sessions (user_id);

-- +goose Down
DROP TABLE sessions;
DROP TABLE users;
