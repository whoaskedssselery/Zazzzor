
-- name: RelationshipUserByUsername :one
SELECT id FROM users WHERE username = $1;

-- name: LockRelationshipUsers :many
-- Lock in ID order across all pairs, so different peers cannot race a quota.
SELECT id, username, display_name, avatar_hash, created_at
FROM users WHERE id IN (@actor_id::bigint, @other_id::bigint)
ORDER BY id FOR NO KEY UPDATE;

-- name: RelationshipPair :many
SELECT user_id, other_id, type FROM relationships
WHERE (user_id = @actor_id AND other_id = @other_id)
   OR (user_id = @other_id AND other_id = @actor_id);

-- name: CountRelationships :one
SELECT count(*) FROM relationships WHERE user_id = $1 AND type = $2;

-- name: SaveRelationship :exec
INSERT INTO relationships(user_id, other_id, type, since) VALUES ($1, $2, $3, $4)
ON CONFLICT (user_id, other_id) DO UPDATE SET type = EXCLUDED.type, since = EXCLUDED.since;

-- name: DeleteRelationship :exec
DELETE FROM relationships WHERE user_id = $1 AND other_id = $2;

-- name: ListRelationships :many
SELECT r.other_id, r.type, r.since, u.username, u.display_name, u.avatar_hash, u.created_at
FROM relationships AS r JOIN users AS u ON u.id = r.other_id
WHERE r.user_id = $1 ORDER BY r.since, r.other_id;

-- name: EnsureRequestWindow :exec
INSERT INTO relationship_request_windows(user_id) VALUES ($1) ON CONFLICT DO NOTHING;

-- name: LockRequestWindow :one
SELECT attempts FROM relationship_request_windows WHERE user_id = $1 FOR UPDATE;

-- name: SaveRequestWindow :exec
UPDATE relationship_request_windows SET attempts = $2 WHERE user_id = $1;
