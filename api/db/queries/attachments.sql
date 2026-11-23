-- name: CreateAttachment :one
INSERT INTO attachments (page_id, s3_key, filename, content_type, size_bytes, uploaded_by_user_id, uploaded_by_guest_id)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: GetAttachmentByID :one
SELECT * FROM attachments WHERE id = $1;

-- name: ListAttachmentsForPage :many
SELECT * FROM attachments WHERE page_id = $1 ORDER BY created_at DESC;
