-- +goose Up
-- The background sweep finds expired retry claims without scanning live ones.
CREATE INDEX message_nonces_expires_at_idx ON message_nonces (expires_at);

-- +goose Down
DROP INDEX message_nonces_expires_at_idx;
