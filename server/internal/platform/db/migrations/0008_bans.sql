-- Bans of a guild, owned by the guilds module.

-- +goose Up
-- A banned user cannot join the guild until unbanned. reason is stored
-- trimmed, at most 512 characters (code points); created_by is the
-- moderator, null once their account is gone.
CREATE TABLE guild_bans (
    guild_id   bigint NOT NULL REFERENCES guilds ON DELETE CASCADE,
    user_id    bigint NOT NULL REFERENCES users ON DELETE CASCADE,
    reason     text CHECK (char_length(reason) BETWEEN 1 AND 512),
    created_by bigint REFERENCES users ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (guild_id, user_id)
);

CREATE INDEX guild_bans_guild_created_idx ON guild_bans (guild_id, created_at, user_id);
CREATE INDEX guild_bans_user_id_idx ON guild_bans (user_id);
CREATE INDEX guild_bans_created_by_idx ON guild_bans (created_by);

-- +goose Down
DROP TABLE guild_bans;
