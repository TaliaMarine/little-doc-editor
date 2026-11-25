-- Fail fast instead of queueing every other statement behind a long reader:
-- golang-migrate applies this file as one transaction, so the locks taken
-- below are held until COMMIT.
SET LOCAL lock_timeout = '15s';

-- Accounts (invites), revision history, session revocation, the admin-set
-- attachment policy (Decision 17) and group membership lookups.
--
-- Rollout safety (Decision 3, multiple api versions during a rolling deploy):
-- every change here is additive for the *old* binary except invites.token,
-- and no released binary reads or writes the invites table (there were no
-- invite endpoints before this migration), so replacing it is safe.

-- Invite tokens are bearer credentials: store only a SHA-256 hash (hex). The
-- tokens are 256-bit random values, so an unsalted fast hash is sufficient;
-- the plaintext is shown once, at creation. Existing rows (none in practice)
-- are backfilled with the hash of their old plaintext token.
ALTER TABLE invites
    ADD COLUMN token_hash text,
    ADD COLUMN revoked_at timestamptz;
UPDATE invites SET token_hash = encode(sha256(convert_to(token, 'UTF8')), 'hex');
ALTER TABLE invites
    ALTER COLUMN token_hash SET NOT NULL,
    ADD CONSTRAINT invites_token_hash_key UNIQUE (token_hash),
    DROP COLUMN token;

-- Server-side logout (SPEC.md §8). Treated as a deny-list: a session JWT is
-- rejected when a row with its token_hash has revoked_at set. Absence of a
-- row never rejects a token (recording is best-effort at login).
ALTER TABLE sessions ADD COLUMN revoked_at timestamptz;
CREATE INDEX sessions_expires_at_idx ON sessions (expires_at);

-- Revisions remember the page title too, so a restore can bring it back.
ALTER TABLE page_revisions ADD COLUMN title text;

-- Decision 17: admin-configurable attachment policy. NULL = use the
-- deployment default from env/Helm (ATTACHMENT_ALLOWLIST /
-- ATTACHMENT_MAX_SIZE_BYTES).
ALTER TABLE instance_settings
    ADD COLUMN attachment_allowlist text[],
    ADD COLUMN attachment_max_size_bytes bigint CHECK (attachment_max_size_bytes IS NULL OR attachment_max_size_bytes > 0);

-- Group-based space membership: resolving a user's role looks up
-- group_members by user.
CREATE INDEX group_members_user_id_idx ON group_members (user_id);

-- ListSpacesForUser filters space_members by principal alone (no space_id),
-- and the same columns are the ON DELETE CASCADE targets for users/groups.
CREATE INDEX space_members_user_id_idx ON space_members (user_id) WHERE user_id IS NOT NULL;
CREATE INDEX space_members_group_id_idx ON space_members (group_id) WHERE group_id IS NOT NULL;
