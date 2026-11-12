-- Full-text search of messages in Russian (Latin words are stemmed as
-- English by the same configuration). An expression index: no column, so
-- reading history does not carry the vectors.

-- +goose Up
CREATE INDEX messages_search_idx ON messages USING gin (to_tsvector('russian', content));

-- +goose Down
DROP INDEX messages_search_idx;
