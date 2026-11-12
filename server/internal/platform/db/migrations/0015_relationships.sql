
-- +goose Up
CREATE TABLE relationships (
    user_id bigint NOT NULL REFERENCES users ON DELETE CASCADE,
    other_id bigint NOT NULL REFERENCES users ON DELETE CASCADE,
    type smallint NOT NULL CHECK (type BETWEEN 1 AND 4),
    since timestamptz NOT NULL,
    PRIMARY KEY (user_id, other_id),
    CHECK (user_id <> other_id)
);
CREATE INDEX relationships_other_idx ON relationships (other_id);
CREATE INDEX relationships_user_type_idx ON relationships (user_id, type);

-- A bounded rolling log counts requests even when the target is missing,
-- the caller retries, or the API restarts.
CREATE TABLE relationship_request_windows (
    user_id bigint PRIMARY KEY REFERENCES users ON DELETE CASCADE,
    attempts timestamptz[] NOT NULL DEFAULT '{}',
    CHECK (cardinality(attempts) <= 100)
);

-- +goose Down
DROP TABLE relationship_request_windows;
DROP TABLE relationships;
