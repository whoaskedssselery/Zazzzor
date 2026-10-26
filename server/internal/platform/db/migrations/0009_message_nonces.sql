-- Nonces of messages just sent, owned by the messages module: a retry with
-- the same nonce (the client lost the response) gets the message the first
-- try stored instead of a second copy.

-- +goose Up
-- A claim is live until expires_at; the author's next send drops their
-- expired ones. Deleting the message drops its claim.
CREATE TABLE message_nonces (
    author_id  bigint NOT NULL REFERENCES users ON DELETE CASCADE,
    channel_id bigint NOT NULL,
    nonce      text NOT NULL,
    message_id bigint NOT NULL REFERENCES messages ON DELETE CASCADE,
    expires_at timestamptz NOT NULL,
    PRIMARY KEY (author_id, channel_id, nonce)
);

CREATE INDEX message_nonces_message_id_idx ON message_nonces (message_id);

-- +goose Down
DROP TABLE message_nonces;
