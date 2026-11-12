-- Personal notification settings, owned by the notifysettings module:
-- per server (guild_id) or for the user's DMs and groups (guild_id NULL),
-- and per channel inside either. Only values that differ from the defaults
-- are stored; a missing row means the defaults.

-- +goose Up
CREATE TABLE user_guild_settings (
    user_id               bigint NOT NULL REFERENCES users ON DELETE CASCADE,
    guild_id              bigint,
    -- One row per scope: 0 stands for the DMs (@me).
    scope_id              bigint GENERATED ALWAYS AS (COALESCE(guild_id, 0)) STORED,
    muted                 boolean NOT NULL,
    mute_until            timestamptz,
    message_notifications smallint NOT NULL CHECK (message_notifications BETWEEN 0 AND 2),
    suppress_everyone     boolean NOT NULL,
    PRIMARY KEY (user_id, scope_id),
    -- Leaving or losing a server drops its settings.
    FOREIGN KEY (guild_id, user_id) REFERENCES members (guild_id, user_id) ON DELETE CASCADE,
    CHECK (mute_until IS NULL OR muted),
    CHECK (guild_id IS NOT NULL OR NOT suppress_everyone)
);

CREATE TABLE channel_notification_overrides (
    user_id               bigint NOT NULL REFERENCES users ON DELETE CASCADE,
    channel_id            bigint NOT NULL REFERENCES channels ON DELETE CASCADE,
    guild_id              bigint,
    private_channel_id    bigint GENERATED ALWAYS AS (CASE WHEN guild_id IS NULL THEN channel_id END) STORED,
    muted                 boolean NOT NULL,
    mute_until            timestamptz,
    -- 3: as the server (or the DMs) says.
    message_notifications smallint NOT NULL CHECK (message_notifications BETWEEN 0 AND 3),
    PRIMARY KEY (user_id, channel_id),
    FOREIGN KEY (guild_id, user_id) REFERENCES members (guild_id, user_id) ON DELETE CASCADE,
    -- Leaving a group, or being removed, drops its override.
    FOREIGN KEY (private_channel_id, user_id) REFERENCES channel_recipients (channel_id, user_id) ON DELETE CASCADE,
    CHECK (mute_until IS NULL OR muted)
);

CREATE INDEX channel_notification_overrides_scope_idx ON channel_notification_overrides (user_id, guild_id);
CREATE INDEX channel_notification_overrides_private_idx ON channel_notification_overrides (private_channel_id, user_id)
    WHERE private_channel_id IS NOT NULL;

-- +goose Down
DROP TABLE channel_notification_overrides;
DROP TABLE user_guild_settings;
