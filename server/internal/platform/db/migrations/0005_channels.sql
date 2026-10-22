-- Channels, categories and their permission overwrites, owned by the
-- channels module.

-- +goose Up
-- type: 0 text, 2 voice, 4 category. A category has no parent; the parent
-- of a channel is a category of the same guild (the composite key keeps it
-- in the guild, the module checks it is a category). position is dense
-- (0..n-1) within each parent, the top level being one scope; uniqueness is
-- checked at commit, so a transaction can renumber freely.
-- permission_synced: the effective overwrites are the parent category's.
CREATE TABLE channels (
    id                bigint PRIMARY KEY,
    guild_id          bigint NOT NULL REFERENCES guilds ON DELETE CASCADE,
    type              smallint NOT NULL CHECK (type IN (0, 2, 4)),
    name              text NOT NULL,
    topic             text,
    parent_id         bigint,
    position          integer NOT NULL CHECK (position >= 0),
    permission_synced boolean NOT NULL DEFAULT true,
    created_at        timestamptz NOT NULL,
    CONSTRAINT channels_guild_id_id_key UNIQUE (guild_id, id),
    CONSTRAINT channels_parent_fkey FOREIGN KEY (guild_id, parent_id)
        REFERENCES channels (guild_id, id) ON DELETE SET NULL (parent_id),
    CONSTRAINT channels_category_top_level CHECK (type <> 4 OR parent_id IS NULL),
    CONSTRAINT channels_synced_needs_parent CHECK (parent_id IS NOT NULL OR NOT permission_synced),
    CONSTRAINT channels_position_key UNIQUE NULLS NOT DISTINCT (guild_id, parent_id, position)
        DEFERRABLE INITIALLY DEFERRED
);

-- target_type: 0 role (the @everyone role has the guild's ID), 1 member.
-- The overwrites of a role or member are dropped with it (role deletion,
-- leaving the guild) through guilds.OverwriteCleaner, so a member who
-- rejoins starts without them.
CREATE TABLE overwrites (
    channel_id  bigint NOT NULL REFERENCES channels ON DELETE CASCADE,
    target_id   bigint NOT NULL,
    target_type smallint NOT NULL CHECK (target_type IN (0, 1)),
    allow       bigint NOT NULL,
    deny        bigint NOT NULL,
    PRIMARY KEY (channel_id, target_id),
    CHECK (allow & deny = 0)
);

-- +goose Down
DROP TABLE overwrites;
DROP TABLE channels;
