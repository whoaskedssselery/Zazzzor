-- What moderators and admins did in a server, written in the transaction
-- of the action itself, owned by the audit module.

-- +goose Up
CREATE TABLE audit_log (
    id          bigint PRIMARY KEY,
    guild_id    bigint NOT NULL REFERENCES guilds ON DELETE CASCADE,
    -- The account that acted; gone when it is deleted.
    actor_id    bigint REFERENCES users ON DELETE SET NULL,
    action_type smallint NOT NULL CHECK (action_type > 0),
    -- The user, role, channel, invite (none) or emoji acted on.
    target_id   bigint,
    changes     jsonb NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(changes) = 'array'),
    -- Extra facts: the channel of a deleted message, an invite code.
    options     jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(options) = 'object'),
    reason      text CHECK (char_length(reason) <= 512),
    created_at  timestamptz NOT NULL
);

CREATE INDEX audit_log_guild_idx ON audit_log (guild_id, id DESC);

-- +goose Down
DROP TABLE audit_log;
