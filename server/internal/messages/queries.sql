-- name: CreateMessage :one
-- created_at comes from the database clock, like edited_at.
INSERT INTO messages (id, channel_id, author_id, content, reply_to_id, created_at,
    mention_user_ids, mention_role_ids, mention_everyone, mention_here, embeds)
VALUES (@id, @channel_id, @author_id, @content, @reply_to_id, now(),
    @mention_user_ids, @mention_role_ids, @mention_everyone, @mention_here, @embeds)
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
UPDATE messages SET content = @content, edited_at = now(), mention_user_ids = @mention_user_ids,
    mention_role_ids = @mention_role_ids, mention_everyone = @mention_everyone, mention_here = @mention_here, embeds = @embeds
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

-- name: CreateAttachment :one
INSERT INTO message_attachments (id, channel_id, uploader_id, filename, content_type, size, width, height, digest, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING *;

-- name: AttachmentForReader :one
SELECT * FROM message_attachments
WHERE id = $1 AND channel_id = $2
  AND (message_id IS NOT NULL OR (uploader_id = sqlc.arg(reader_id) AND created_at > sqlc.arg(cutoff)));

-- name: DiscardPendingAttachment :execrows
DELETE FROM message_attachments
WHERE id = $1 AND channel_id = $2 AND uploader_id = $3 AND message_id IS NULL AND created_at > sqlc.arg(cutoff);

-- name: BindAttachments :many
UPDATE message_attachments AS a
SET message_id = sqlc.arg(message_id), position = picked.position::integer
FROM unnest(sqlc.arg(ids)::bigint[]) WITH ORDINALITY AS picked(id, position)
WHERE a.id = picked.id AND a.channel_id = sqlc.arg(channel_id) AND a.uploader_id = sqlc.arg(uploader_id)
  AND a.message_id IS NULL AND a.created_at > sqlc.arg(cutoff)
RETURNING a.*;

-- name: AttachmentsByMessages :many
SELECT * FROM message_attachments WHERE message_id = ANY(sqlc.arg(ids)::bigint[])
ORDER BY message_id, position;

-- name: ExpirePendingAttachments :exec
DELETE FROM message_attachments WHERE message_id IS NULL AND created_at <= sqlc.arg(cutoff);

-- name: AttachmentReferences :many
SELECT id, channel_id FROM message_attachments
UNION ALL SELECT id, 1::bigint AS channel_id FROM embed_images;

-- name: AttachmentReferenced :one
SELECT EXISTS (
    SELECT 1 FROM message_attachments AS a WHERE a.id = @id AND a.channel_id = @channel_id
    UNION ALL SELECT 1 FROM embed_images AS i WHERE i.id = @id AND @channel_id = 1
);

-- name: QueuedAttachmentTrash :many
SELECT * FROM attachment_trash ORDER BY id;

-- name: ForgetAttachmentTrash :exec
DELETE FROM attachment_trash WHERE id = $1;


-- name: SetLastMessage :exec
UPDATE channels SET last_message_id = GREATEST(last_message_id, @message_id::bigint) WHERE id = @channel_id;

-- name: RecomputeLastMessage :one
UPDATE channels
SET last_message_id = (
    SELECT id FROM messages WHERE channel_id = @channel_id ORDER BY id DESC LIMIT 1
)
WHERE id = @channel_id
RETURNING last_message_id;

-- name: PutMentionRecipients :exec
INSERT INTO message_mention_recipients (message_id, user_id)
SELECT @message_id::bigint, user_id FROM unnest(@user_ids::bigint[]) AS target(user_id);

-- name: ClearMentionRecipients :exec
DELETE FROM message_mention_recipients WHERE message_id = @message_id;

-- name: IncrementMentions :exec
INSERT INTO read_states (user_id, channel_id, guild_id, last_read_id, mention_count)
SELECT user_id, @channel_id, @guild_id, 0, 1
FROM unnest(@user_ids::bigint[]) AS target(user_id)
ON CONFLICT (user_id, channel_id) DO UPDATE SET mention_count = read_states.mention_count + 1
WHERE read_states.last_read_id < @message_id::bigint;

-- name: AdvanceReadStates :many
INSERT INTO read_states (user_id, guild_id, channel_id, last_read_id, mention_count)
SELECT @user_id, @guild_id, target.channel_id, target.message_id,
    (SELECT count(*) FROM messages AS m
        JOIN message_mention_recipients AS r ON r.message_id = m.id
        WHERE m.channel_id = target.channel_id AND m.id > target.message_id AND r.user_id = @user_id)
FROM (SELECT unnest(@channel_ids::bigint[]) AS channel_id, unnest(@message_ids::bigint[]) AS message_id) AS target
ON CONFLICT (user_id, channel_id) DO UPDATE
SET last_read_id = EXCLUDED.last_read_id, mention_count = EXCLUDED.mention_count
WHERE read_states.last_read_id < EXCLUDED.last_read_id
   OR (read_states.last_read_id = EXCLUDED.last_read_id AND read_states.mention_count <> EXCLUDED.mention_count)
RETURNING *;

-- name: ReadStatesForChannels :many
SELECT * FROM read_states WHERE user_id = @user_id AND channel_id = ANY(@channel_ids::bigint[])
ORDER BY channel_id;

-- name: ReactionGroupsForWrite :many
WITH locked AS (
    SELECT emoji, first_added_at FROM message_reactions
    WHERE message_id = $1
    FOR UPDATE
)
SELECT emoji, min(first_added_at)::timestamptz AS first_added_at
FROM locked
GROUP BY emoji
ORDER BY first_added_at;

-- name: AddMessageReaction :execrows
INSERT INTO message_reactions (message_id, emoji, user_id, first_added_at)
VALUES ($1, $2, $3, COALESCE(sqlc.narg(first_added_at)::timestamptz, (
    SELECT GREATEST(clock_timestamp(), max(mr.first_added_at) + interval '1 microsecond')
    FROM message_reactions AS mr WHERE mr.message_id = $1
)))
ON CONFLICT (message_id, emoji, user_id) DO NOTHING;

-- name: RemoveMessageReaction :execrows
DELETE FROM message_reactions WHERE message_id = $1 AND emoji = $2 AND user_id = $3;

-- name: ReactionSummaries :many
SELECT message_id, emoji, count(*) AS count, bool_or(user_id = sqlc.arg(caller_id)::bigint)::bool AS me
FROM message_reactions
WHERE message_id = ANY(sqlc.arg(message_ids)::bigint[])
GROUP BY message_id, emoji
ORDER BY message_id, min(first_added_at), emoji;

-- name: ReactionUserIDs :many
SELECT user_id FROM message_reactions
WHERE message_id = $1 AND emoji = $2 AND user_id > sqlc.arg(after_id)::bigint
ORDER BY user_id
LIMIT sqlc.arg(page_limit)::int;

-- name: LockPreviewReferences :exec
-- The cleanup writer excludes new references until its orphan scan commits.
SELECT pg_advisory_xact_lock_shared(91403221130845);

-- name: LockPreviewCleanup :exec
SELECT pg_advisory_xact_lock(91403221130845);

-- name: CachedPreviews :many
SELECT url, status, embed FROM link_previews
WHERE url = ANY(@urls::text[])
  AND fetched_at > CASE WHEN status THEN @success_cutoff::timestamptz ELSE @failure_cutoff::timestamptz END;

-- name: SavePreview :exec
INSERT INTO link_previews (url, fetched_at, status, embed)
VALUES (@url, @fetched_at, @status, sqlc.narg(embed))
ON CONFLICT (url) DO UPDATE
SET fetched_at = EXCLUDED.fetched_at, status = EXCLUDED.status, embed = EXCLUDED.embed;

-- name: SetMessageEmbeds :execrows
UPDATE messages SET embeds = @embeds
WHERE id = @id AND channel_id = @channel_id AND content = @content
  AND edited_at IS NOT DISTINCT FROM sqlc.narg(edited_at)
  AND embeds <> @embeds;

-- name: AddEmbedImage :exec
INSERT INTO embed_images (id, token, content_type, created_at) VALUES ($1, $2, $3, $4);

-- name: EmbedImageByToken :one
SELECT id, content_type FROM embed_images WHERE token = $1;

-- name: ExpirePreviews :exec
DELETE FROM link_previews
WHERE fetched_at <= CASE WHEN status THEN @success_cutoff::timestamptz ELSE @failure_cutoff::timestamptz END;

-- name: UnusedEmbedImages :many
SELECT i.id FROM embed_images AS i
WHERE i.created_at < @cutoff
  AND NOT EXISTS (
      SELECT 1 FROM messages AS m
      WHERE m.embeds <> '[]'::jsonb AND m.embeds @> jsonb_build_array(
          jsonb_build_object('image', jsonb_build_object('url', '/api/v1/embeds/' || i.token))))
  AND NOT EXISTS (
      SELECT 1 FROM link_previews AS p WHERE p.embed -> 'image' ->> 'url' = '/api/v1/embeds/' || i.token)
ORDER BY i.created_at
LIMIT 100;

-- name: ForgetEmbedImage :exec
WITH removed AS (
    DELETE FROM embed_images AS i WHERE i.id = @id::bigint RETURNING i.id
)
INSERT INTO attachment_trash (id, channel_id) SELECT id, 1 FROM removed;
