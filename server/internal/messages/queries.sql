-- name: CreateMessage :one
-- created_at comes from the database clock, like edited_at.
INSERT INTO messages (id, channel_id, author_id, content, reply_to_id, created_at)
VALUES (@id, @channel_id, @author_id, @content, @reply_to_id, now())
RETURNING *;

-- name: MessageInChannel :one
SELECT * FROM messages WHERE id = @id AND channel_id = @channel_id;

-- name: MessagesBefore :many
-- Newest first; before 0 means from the newest.
SELECT * FROM messages
WHERE channel_id = @channel_id AND (@before::bigint = 0 OR id < @before::bigint)
ORDER BY id DESC
LIMIT @lim;

-- name: MessagesAfter :many
-- Oldest first.
SELECT * FROM messages
WHERE channel_id = @channel_id AND id > @after::bigint
ORDER BY id
LIMIT @lim;

-- name: MessagesByIDs :many
-- The messages of the channel among ids (the originals replies point to).
SELECT * FROM messages WHERE channel_id = @channel_id AND id = ANY(@ids::bigint[]);

-- name: EditMessage :one
-- Changes the content of the author's message if it differs; no row when
-- the message is gone, not the author's or already says that.
UPDATE messages SET content = @content, edited_at = now()
WHERE id = @id AND channel_id = @channel_id AND author_id = @author_id AND content <> @content
RETURNING *;

-- name: DeleteMessage :execrows
DELETE FROM messages WHERE id = @id AND channel_id = @channel_id;

-- name: DropExpiredNonces :exec
DELETE FROM message_nonces WHERE author_id = @author_id AND expires_at <= now();

-- name: ClaimNonce :execrows
-- Claims the nonce for the message; no row when a claim holds it (after
-- DropExpiredNonces, a live one). A claim being inserted concurrently is
-- waited for.
INSERT INTO message_nonces (author_id, channel_id, nonce, message_id, expires_at)
VALUES (@author_id, @channel_id, @nonce, @message_id, now() + make_interval(secs => @ttl_seconds::float8))
ON CONFLICT DO NOTHING;

-- name: MessageByNonce :one
-- The message a live claim of the nonce points to.
SELECT * FROM messages
WHERE id = (SELECT n.message_id FROM message_nonces n
            WHERE n.author_id = @author_id AND n.channel_id = @channel_id AND n.nonce = @nonce AND n.expires_at > now());
