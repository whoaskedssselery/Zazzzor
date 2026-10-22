-- Invites to a guild, owned by the invites module.

-- +goose Up
-- max_uses 0 means unlimited, expires_at null never. The composite key ties
-- the channel to the invite's guild; both cascade.
CREATE TABLE invites (
    code       text PRIMARY KEY CHECK (code ~ '^[0-9A-Za-z]{8}$'),
    guild_id   bigint NOT NULL REFERENCES guilds ON DELETE CASCADE,
    channel_id bigint NOT NULL,
    -- Accounts are disabled, never deleted.
    inviter_id bigint NOT NULL REFERENCES users,
    max_uses   integer NOT NULL CHECK (max_uses BETWEEN 0 AND 100),
    uses       integer NOT NULL DEFAULT 0 CHECK (uses >= 0 AND (max_uses = 0 OR uses <= max_uses)),
    expires_at timestamptz,
    created_at timestamptz NOT NULL,
    FOREIGN KEY (guild_id, channel_id) REFERENCES channels (guild_id, id) ON DELETE CASCADE
);

CREATE INDEX invites_guild_channel_idx ON invites (guild_id, channel_id);
CREATE INDEX invites_inviter_id_idx ON invites (inviter_id);

-- +goose Down
DROP TABLE invites;
