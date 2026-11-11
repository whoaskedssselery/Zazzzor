-- +goose Up
ALTER TABLE channels ADD COLUMN last_message_id bigint;
UPDATE channels AS c
SET last_message_id = (
    SELECT m.id FROM messages AS m WHERE m.channel_id = c.id ORDER BY m.id DESC LIMIT 1
)
WHERE c.type = 0;

ALTER TABLE messages
    ADD COLUMN mention_user_ids bigint[] NOT NULL DEFAULT '{}',
    ADD COLUMN mention_role_ids bigint[] NOT NULL DEFAULT '{}',
    ADD COLUMN mention_everyone boolean NOT NULL DEFAULT false,
    ADD COLUMN mention_here boolean NOT NULL DEFAULT false;

-- The read position survives deletion of the message that established it.
CREATE TABLE read_states (
    user_id bigint NOT NULL,
    channel_id bigint NOT NULL,
    guild_id bigint NOT NULL,
    last_read_id bigint NOT NULL CHECK (last_read_id >= 0),
    mention_count integer NOT NULL CHECK (mention_count >= 0),
    PRIMARY KEY (user_id, channel_id),
    FOREIGN KEY (guild_id, user_id) REFERENCES members(guild_id, user_id) ON DELETE CASCADE,
    FOREIGN KEY (guild_id, channel_id) REFERENCES channels(guild_id, id) ON DELETE CASCADE
);
CREATE INDEX read_states_members_idx ON read_states (guild_id, user_id);
CREATE INDEX read_states_channels_idx ON read_states (guild_id, channel_id);

-- Resolved recipients preserve visibility, role membership and presence at
-- the time a mention was evaluated. One row per recipient and message.
CREATE TABLE message_mention_recipients (
    message_id bigint NOT NULL REFERENCES messages ON DELETE CASCADE,
    user_id bigint NOT NULL REFERENCES users ON DELETE CASCADE,
    PRIMARY KEY (message_id, user_id)
);
CREATE INDEX message_mention_recipients_user_idx ON message_mention_recipients (user_id, message_id);

-- +goose Down
DROP TABLE message_mention_recipients;
DROP TABLE read_states;
ALTER TABLE messages
    DROP COLUMN mention_here,
    DROP COLUMN mention_everyone,
    DROP COLUMN mention_role_ids,
    DROP COLUMN mention_user_ids;
ALTER TABLE channels DROP COLUMN last_message_id;
