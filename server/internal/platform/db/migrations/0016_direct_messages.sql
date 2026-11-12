
-- +goose Up
ALTER TABLE channels DROP CONSTRAINT channels_type_check, DROP CONSTRAINT channels_position_key;
ALTER TABLE channels ALTER COLUMN guild_id DROP NOT NULL, ALTER COLUMN name DROP NOT NULL,
    ADD COLUMN owner_id bigint REFERENCES users ON DELETE SET NULL,
    ADD COLUMN position_parent_id bigint GENERATED ALWAYS AS (COALESCE(parent_id, 0)) STORED;
ALTER TABLE channels
    ADD CONSTRAINT channels_scope CHECK (
        (guild_id IS NOT NULL AND type IN (0,2,4) AND name IS NOT NULL AND owner_id IS NULL)
        OR (guild_id IS NULL AND type IN (1,3) AND parent_id IS NULL AND topic IS NULL
            AND position = 0 AND NOT permission_synced
            AND (type = 3 OR (owner_id IS NULL AND name IS NULL)))),
    ADD CONSTRAINT channels_position_key UNIQUE (guild_id, position_parent_id, position)
        DEFERRABLE INITIALLY DEFERRED;

CREATE TABLE channel_recipients (
    channel_id bigint NOT NULL REFERENCES channels ON DELETE CASCADE,
    user_id bigint NOT NULL REFERENCES users ON DELETE CASCADE,
    joined_at timestamptz NOT NULL,
    closed boolean NOT NULL DEFAULT false,
    PRIMARY KEY (channel_id, user_id)
);
CREATE INDEX channel_recipients_user_idx ON channel_recipients (user_id, channel_id);

CREATE TABLE direct_channel_pairs (
    low_id bigint NOT NULL REFERENCES users ON DELETE CASCADE,
    high_id bigint NOT NULL REFERENCES users ON DELETE CASCADE,
    channel_id bigint NOT NULL UNIQUE REFERENCES channels ON DELETE CASCADE,
    PRIMARY KEY (low_id, high_id),
    CHECK (low_id < high_id)
);

ALTER TABLE read_states ALTER COLUMN guild_id DROP NOT NULL,
    ADD COLUMN private_channel_id bigint GENERATED ALWAYS AS (
        CASE WHEN guild_id IS NULL THEN channel_id END
    ) STORED;
ALTER TABLE read_states ADD CONSTRAINT read_states_private_recipient_fkey
    FOREIGN KEY (private_channel_id, user_id) REFERENCES channel_recipients(channel_id, user_id) ON DELETE CASCADE;
CREATE INDEX read_states_private_recipient_idx ON read_states (private_channel_id, user_id)
    WHERE private_channel_id IS NOT NULL;

-- Maintain ownership and channel lifetime on both explicit leave and account deletion.
-- +goose StatementBegin
CREATE FUNCTION maintain_private_channel_after_recipient_delete() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    DELETE FROM channels AS c
    WHERE c.id = OLD.channel_id AND c.guild_id IS NULL AND
        (c.type = 1 OR NOT EXISTS (SELECT 1 FROM channel_recipients WHERE channel_id = c.id));
    UPDATE channels AS c
    SET owner_id = (SELECT r.user_id FROM channel_recipients AS r WHERE r.channel_id = c.id
                    ORDER BY r.joined_at, r.user_id LIMIT 1)
    WHERE c.id = OLD.channel_id AND c.type = 3 AND (c.owner_id = OLD.user_id OR c.owner_id IS NULL);
    RETURN NULL;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER maintain_private_channel_after_recipient_delete
    AFTER DELETE ON channel_recipients FOR EACH ROW EXECUTE FUNCTION maintain_private_channel_after_recipient_delete();

-- +goose Down
DROP TRIGGER maintain_private_channel_after_recipient_delete ON channel_recipients;
DROP FUNCTION maintain_private_channel_after_recipient_delete();
DELETE FROM channels WHERE guild_id IS NULL;
ALTER TABLE read_states DROP COLUMN private_channel_id, ALTER COLUMN guild_id SET NOT NULL;
DROP TABLE direct_channel_pairs;
DROP TABLE channel_recipients;
ALTER TABLE channels DROP CONSTRAINT channels_position_key, DROP CONSTRAINT channels_scope;
ALTER TABLE channels DROP COLUMN position_parent_id, DROP COLUMN owner_id,
    ALTER COLUMN guild_id SET NOT NULL, ALTER COLUMN name SET NOT NULL;
ALTER TABLE channels ADD CONSTRAINT channels_type_check CHECK (type IN (0,2,4)),
    ADD CONSTRAINT channels_position_key UNIQUE NULLS NOT DISTINCT (guild_id,parent_id,position)
        DEFERRABLE INITIALLY DEFERRED;
