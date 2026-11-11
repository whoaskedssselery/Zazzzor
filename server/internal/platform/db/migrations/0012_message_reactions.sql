-- +goose Up
CREATE TABLE message_reactions (
    message_id bigint NOT NULL REFERENCES messages ON DELETE CASCADE,
    emoji text COLLATE "C" NOT NULL CHECK (octet_length(emoji) BETWEEN 1 AND 32),
    user_id bigint NOT NULL REFERENCES users ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    first_added_at timestamptz NOT NULL,
    PRIMARY KEY (message_id, emoji, user_id)
);
CREATE INDEX message_reactions_user_idx ON message_reactions (user_id);

UPDATE roles SET permissions = permissions | 64 WHERE id = guild_id;

-- +goose Down
UPDATE roles SET permissions = permissions & ~64::bigint WHERE id = guild_id;
DROP TABLE message_reactions;
