-- +goose Up
ALTER TABLE messages DROP CONSTRAINT messages_content_check,
    ADD CONSTRAINT messages_content_check CHECK (char_length(content) <= 4000);

CREATE TABLE message_attachments (
    id bigint PRIMARY KEY,
    channel_id bigint NOT NULL REFERENCES channels ON DELETE CASCADE,
    uploader_id bigint REFERENCES users ON DELETE SET NULL,
    message_id bigint REFERENCES messages ON DELETE CASCADE,
    filename text NOT NULL,
    content_type text NOT NULL,
    size bigint NOT NULL CHECK (size BETWEEN 0 AND 26214400),
    width integer CHECK (width > 0),
    height integer CHECK (height > 0),
    digest text NOT NULL,
    position integer NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL
);
CREATE INDEX message_attachments_channel_idx ON message_attachments (channel_id);
CREATE INDEX message_attachments_uploader_idx ON message_attachments (uploader_id);
CREATE INDEX message_attachments_message_idx ON message_attachments (message_id, position);
CREATE INDEX message_attachments_pending_idx ON message_attachments (created_at) WHERE message_id IS NULL;

-- The key survives cascades and a crash between commit and the file move.
CREATE TABLE attachment_trash (
    id bigint PRIMARY KEY,
    channel_id bigint NOT NULL
);

-- +goose StatementBegin
CREATE FUNCTION queue_attachment_trash() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO attachment_trash (id, channel_id) VALUES (OLD.id, OLD.channel_id);
    RETURN OLD;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER queue_attachment_trash
    AFTER DELETE ON message_attachments
    FOR EACH ROW EXECUTE FUNCTION queue_attachment_trash();

-- +goose Down
-- The old server requires text even for messages created with only files.
UPDATE messages SET content = '[attachment]' WHERE content = '';
DROP TABLE message_attachments;
DROP FUNCTION queue_attachment_trash();
DROP TABLE attachment_trash;
ALTER TABLE messages DROP CONSTRAINT messages_content_check,
    ADD CONSTRAINT messages_content_check CHECK (char_length(content) BETWEEN 1 AND 4000);
