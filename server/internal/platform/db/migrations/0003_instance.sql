-- Instance-wide settings, owned by the admin module: exactly one row.

-- +goose Up
CREATE TABLE instance (
    -- Always true: the primary key and the check allow a single row.
    id                boolean PRIMARY KEY DEFAULT true CHECK (id),
    registration_open boolean NOT NULL DEFAULT true
);

INSERT INTO instance DEFAULT VALUES;

-- +goose Down
DROP TABLE instance;
