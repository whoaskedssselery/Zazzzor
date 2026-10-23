-- Servers (guilds), their members and roles, owned by the guilds module.

-- +goose Up
CREATE TABLE guilds (
    id         bigint PRIMARY KEY,
    name       text NOT NULL,
    icon_hash  text CHECK (icon_hash ~ '^[0-9a-f]{16}$'),
    -- Accounts are disabled, never deleted, so the owner always exists.
    owner_id   bigint NOT NULL REFERENCES users,
    created_at timestamptz NOT NULL
);

CREATE INDEX guilds_owner_id_idx ON guilds (owner_id);

CREATE TABLE members (
    guild_id  bigint NOT NULL REFERENCES guilds ON DELETE CASCADE,
    user_id   bigint NOT NULL REFERENCES users ON DELETE CASCADE,
    nick      text,
    joined_at timestamptz NOT NULL,
    PRIMARY KEY (guild_id, user_id)
);

CREATE INDEX members_user_id_idx ON members (user_id);

-- The @everyone role has the ID of its guild and position 0; the others
-- have the dense positions 1..n. The unique position is checked at commit,
-- so a transaction can renumber the roles freely.
CREATE TABLE roles (
    id          bigint PRIMARY KEY,
    guild_id    bigint NOT NULL REFERENCES guilds ON DELETE CASCADE,
    name        text NOT NULL,
    color       integer NOT NULL DEFAULT 0 CHECK (color BETWEEN 0 AND 16777215),
    position    integer NOT NULL CHECK (position >= 0),
    permissions bigint NOT NULL,
    hoist       boolean NOT NULL DEFAULT false,
    mentionable boolean NOT NULL DEFAULT false,
    created_at  timestamptz NOT NULL,
    CONSTRAINT roles_guild_position_key UNIQUE (guild_id, position) DEFERRABLE INITIALLY DEFERRED,
    -- Target of member_roles, so a member can only hold roles of its guild.
    CONSTRAINT roles_guild_id_id_key UNIQUE (guild_id, id)
);

CREATE TABLE member_roles (
    guild_id bigint NOT NULL,
    user_id  bigint NOT NULL,
    role_id  bigint NOT NULL,
    PRIMARY KEY (guild_id, user_id, role_id),
    FOREIGN KEY (guild_id, user_id) REFERENCES members (guild_id, user_id) ON DELETE CASCADE,
    FOREIGN KEY (guild_id, role_id) REFERENCES roles (guild_id, id) ON DELETE CASCADE
);

CREATE INDEX member_roles_role_idx ON member_roles (guild_id, role_id);

-- +goose Down
DROP TABLE member_roles;
DROP TABLE roles;
DROP TABLE members;
DROP TABLE guilds;
