
-- +goose Up
CREATE TABLE guild_emojis (
    id bigint PRIMARY KEY,
    guild_id bigint NOT NULL REFERENCES guilds ON DELETE CASCADE,
    name text COLLATE "C" NOT NULL CHECK (name ~ '^[A-Za-z0-9_]{2,32}$'),
    animated boolean NOT NULL,
    creator_id bigint REFERENCES users ON DELETE SET NULL,
    created_at timestamptz NOT NULL,
    deleted_at timestamptz
);
CREATE INDEX guild_emojis_guild_idx ON guild_emojis (guild_id);
CREATE UNIQUE INDEX guild_emojis_active_name_idx ON guild_emojis (guild_id, lower(name))
    WHERE deleted_at IS NULL;

ALTER TABLE message_reactions ADD COLUMN emoji_id bigint REFERENCES guild_emojis ON DELETE CASCADE;
ALTER TABLE message_reactions ADD CONSTRAINT reaction_emoji_identity CHECK (
    (emoji_id IS NULL AND emoji NOT LIKE 'c:%') OR
    (emoji_id IS NOT NULL AND emoji = 'c:' || emoji_id::text)
);
CREATE INDEX message_reactions_emoji_idx ON message_reactions (emoji_id) WHERE emoji_id IS NOT NULL;

-- +goose StatementBegin
CREATE FUNCTION queue_guild_emoji_trash() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.deleted_at IS NULL THEN
        IF TG_OP = 'DELETE' OR NEW.deleted_at IS NOT NULL THEN
            INSERT INTO attachment_trash (id, channel_id) VALUES (OLD.id, 2)
            ON CONFLICT (id) DO NOTHING;
        END IF;
    END IF;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER queue_guild_emoji_trash
    AFTER UPDATE OF deleted_at OR DELETE ON guild_emojis
    FOR EACH ROW EXECUTE FUNCTION queue_guild_emoji_trash();

-- +goose Down
UPDATE roles SET permissions = permissions & ~(1::bigint << 30) WHERE permissions & (1::bigint << 30) <> 0;
UPDATE overwrites SET allow = allow & ~(1::bigint << 30), deny = deny & ~(1::bigint << 30)
WHERE (allow | deny) & (1::bigint << 30) <> 0;
DELETE FROM guild_emojis;
DROP TRIGGER queue_guild_emoji_trash ON guild_emojis;
DROP FUNCTION queue_guild_emoji_trash();
ALTER TABLE message_reactions DROP CONSTRAINT reaction_emoji_identity;
ALTER TABLE message_reactions DROP COLUMN emoji_id;
DROP TABLE guild_emojis;
