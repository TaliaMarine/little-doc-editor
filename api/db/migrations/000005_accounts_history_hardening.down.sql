-- Lossy by nature (columns are dropped): after down + up, logouts recorded in
-- sessions.revoked_at are forgotten (unexpired revoked tokens work again),
-- revision titles and the admin attachment policy are gone, and pending
-- invites cannot be redeemed (only their hash survives, see below).
DROP INDEX IF EXISTS space_members_group_id_idx;
DROP INDEX IF EXISTS space_members_user_id_idx;
DROP INDEX IF EXISTS group_members_user_id_idx;
ALTER TABLE instance_settings
    DROP COLUMN attachment_max_size_bytes,
    DROP COLUMN attachment_allowlist;
ALTER TABLE page_revisions DROP COLUMN title;
DROP INDEX IF EXISTS sessions_expires_at_idx;
ALTER TABLE sessions DROP COLUMN revoked_at;
-- The plaintext of an invite token cannot be recovered from its hash; rolled-
-- back invites keep the hash in `token` and so can no longer be redeemed
-- (they would have to be re-issued). Documented, deliberate data loss.
ALTER TABLE invites
    ADD COLUMN token text;
UPDATE invites SET token = token_hash;
ALTER TABLE invites
    ALTER COLUMN token SET NOT NULL,
    ADD CONSTRAINT invites_token_key UNIQUE (token),
    DROP COLUMN revoked_at,
    DROP COLUMN token_hash;
