-- name: UpsertCollabDocumentState :exec
INSERT INTO collab_documents (page_id, state, updated_at)
VALUES ($1, $2, now())
ON CONFLICT (page_id) DO UPDATE SET state = $2, updated_at = now();

-- name: GetCollabDocumentState :one
SELECT * FROM collab_documents WHERE page_id = $1;
