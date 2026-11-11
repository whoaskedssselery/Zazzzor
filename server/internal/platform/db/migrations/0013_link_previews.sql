
-- +goose Up
ALTER TABLE messages ADD COLUMN embeds jsonb NOT NULL DEFAULT '[]'::jsonb
    CHECK (jsonb_typeof(embeds) = 'array' AND jsonb_array_length(embeds) <= 5);

CREATE TABLE link_previews (
    url text PRIMARY KEY CHECK (octet_length(url) <= 2048),
    fetched_at timestamptz NOT NULL,
    status boolean NOT NULL,
    embed jsonb,
    CHECK (status = (embed IS NOT NULL))
);

CREATE TABLE embed_images (
    id bigint PRIMARY KEY CHECK (id > 0),
    token text NOT NULL UNIQUE,
    content_type text NOT NULL CHECK (content_type IN ('image/png', 'image/jpeg', 'image/gif', 'image/webp')),
    created_at timestamptz NOT NULL
);

CREATE INDEX messages_embeds_image_idx ON messages USING gin (embeds jsonb_path_ops)
    WHERE embeds <> '[]'::jsonb;
CREATE INDEX embed_images_created_idx ON embed_images (created_at);
CREATE INDEX link_previews_image_idx ON link_previews ((embed -> 'image' ->> 'url'));

-- +goose Down
DROP TABLE embed_images;
DROP TABLE link_previews;
DROP INDEX messages_embeds_image_idx;
ALTER TABLE messages DROP COLUMN embeds;
