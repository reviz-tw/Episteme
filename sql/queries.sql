-- name: CountUsers :one
SELECT count(*) FROM users;

-- name: PurgeExpiredSessions :exec
DELETE FROM sessions WHERE expires_at < now();

-- name: PublishedChunks :many
SELECT id, document_id, indexed_content, indexed_breadcrumbs, indexed_revision, indexed_document_metadata
FROM document_chunks
WHERE indexed_revision > 0 AND NOT is_excluded
ORDER BY document_id, chunk_index;
