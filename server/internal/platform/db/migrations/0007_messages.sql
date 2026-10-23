-- Messages of the text channels, owned by the messages module.

-- +goose Up
-- content is stored trimmed, 1 to 4000 characters (code points). An author
-- whose account is removed leaves the message with author_id null.
-- reply_to_id has no foreign key: a reply outlives the message it answers.
CREATE TABLE messages (
    id          bigint PRIMARY KEY,
    channel_id  bigint NOT NULL REFERENCES channels ON DELETE CASCADE,
    author_id   bigint REFERENCES users ON DELETE SET NULL,
    content     text NOT NULL CHECK (char_length(content) BETWEEN 1 AND 4000),
    reply_to_id bigint,
    edited_at   timestamptz,
    created_at  timestamptz NOT NULL
);

CREATE INDEX messages_channel_id_id_idx ON messages (channel_id, id DESC);
CREATE INDEX messages_author_id_idx ON messages (author_id);

-- +goose Down
DROP TABLE messages;
