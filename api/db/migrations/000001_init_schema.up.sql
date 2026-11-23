-- Initial domain model, per SPEC.md §3.1.
--
-- gen_random_uuid() is part of Postgres core since v13, no extension needed.
-- pg_trgm IS an extension (Decision 2, for fuzzy search) and requires
-- CREATE EXTENSION privileges — if the operator's DB user lacks them, this
-- statement fails loudly here rather than silently degrading search later.
CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE TYPE auth_provider AS ENUM ('internal', 'oidc');
CREATE TYPE space_role AS ENUM ('owner', 'admin', 'editor', 'viewer');
CREATE TYPE share_link_permission AS ENUM ('view', 'edit');

-- Internal and OIDC-linked users share one table, discriminated by
-- auth_provider (SPEC.md §6.4). Internal accounts can be instance-disabled
-- once OIDC is configured (Decision 14); that's an instance-wide setting,
-- not a per-user column, so it isn't modeled here.
CREATE TABLE users (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    email           text NOT NULL UNIQUE,
    display_name    text NOT NULL,
    auth_provider   auth_provider NOT NULL,
    password_hash   text,               -- argon2id, set only when auth_provider = 'internal'
    oidc_issuer     text,               -- set only when auth_provider = 'oidc'
    oidc_subject    text,               -- set only when auth_provider = 'oidc'
    is_org_admin    boolean NOT NULL DEFAULT false,
    disabled        boolean NOT NULL DEFAULT false,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT users_oidc_identity_unique UNIQUE (oidc_issuer, oidc_subject),
    CONSTRAINT users_provider_fields_consistent CHECK (
        (auth_provider = 'internal' AND password_hash IS NOT NULL AND oidc_issuer IS NULL AND oidc_subject IS NULL)
        OR
        (auth_provider = 'oidc' AND password_hash IS NULL AND oidc_issuer IS NOT NULL AND oidc_subject IS NOT NULL)
    )
);

CREATE TABLE groups (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name        text NOT NULL UNIQUE,
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE group_members (
    group_id    uuid NOT NULL REFERENCES groups (id) ON DELETE CASCADE,
    user_id     uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    PRIMARY KEY (group_id, user_id)
);

-- Space-level lifecycle policy for revision history (Decision 9): prune by
-- whichever of max-count / max-age fires first. Either may be null to mean
-- "no limit on that dimension"; both null means no pruning at all.
CREATE TABLE spaces (
    id                      uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name                    text NOT NULL,
    slug                    text NOT NULL UNIQUE,
    icon                    text,
    history_max_count       integer,
    history_max_age_days    integer,
    created_at              timestamptz NOT NULL DEFAULT now(),
    updated_at              timestamptz NOT NULL DEFAULT now()
);

-- Space-level roles only (Decision 7, 24) — no per-page ACL table exists, and
-- none should be added without revisiting that decision. A member row is
-- exactly one of a user or a group, never both.
CREATE TABLE space_members (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    space_id    uuid NOT NULL REFERENCES spaces (id) ON DELETE CASCADE,
    user_id     uuid REFERENCES users (id) ON DELETE CASCADE,
    group_id    uuid REFERENCES groups (id) ON DELETE CASCADE,
    role        space_role NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT space_members_exactly_one_principal CHECK (
        (user_id IS NOT NULL AND group_id IS NULL) OR (user_id IS NULL AND group_id IS NOT NULL)
    ),
    CONSTRAINT space_members_unique_user UNIQUE (space_id, user_id),
    CONSTRAINT space_members_unique_group UNIQUE (space_id, group_id)
);

-- Anonymous edit-link access is attributed to a lightweight guest identity
-- carrying just the mandatory display name (Decision 6), not a full account.
CREATE TABLE guest_identities (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    display_name    text NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT now()
);

-- Content is ProseMirror/Tiptap JSON, the source of truth (Decision 4);
-- Markdown is generated on demand and never stored here. search_vector is
-- maintained by the Go layer's SearchIndexer, not a SQL trigger, since it
-- must be derived from the JSON content, not from a plain text column.
CREATE TABLE pages (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    space_id            uuid NOT NULL REFERENCES spaces (id) ON DELETE CASCADE,
    parent_id           uuid REFERENCES pages (id) ON DELETE CASCADE,
    position            double precision NOT NULL DEFAULT 0,
    title               text NOT NULL,
    content             jsonb NOT NULL DEFAULT '{}'::jsonb,
    search_vector       tsvector,
    history_enabled     boolean,             -- null = inherit the space default
    locked              boolean NOT NULL DEFAULT false,
    is_template         boolean NOT NULL DEFAULT false,
    template_key        text,                -- set for built-in templates (Decision 22)
    created_by_user_id  uuid REFERENCES users (id),
    created_by_guest_id uuid REFERENCES guest_identities (id),
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX pages_space_parent_idx ON pages (space_id, parent_id, position);
CREATE INDEX pages_search_vector_idx ON pages USING GIN (search_vector);
CREATE INDEX pages_title_trgm_idx ON pages USING GIN (title gin_trgm_ops);

-- Immutable revisions (SPEC.md §6.7): created on save or on a debounce
-- interval during live collaboration, never every keystroke. Pruned by the
-- space's lifecycle policy; a viewer may browse these but never restore
-- (Decision 30), which is an authorization rule enforced in the Go layer,
-- not a column here.
CREATE TABLE page_revisions (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    page_id             uuid NOT NULL REFERENCES pages (id) ON DELETE CASCADE,
    content             jsonb NOT NULL,
    author_user_id      uuid REFERENCES users (id),
    author_guest_id     uuid REFERENCES guest_identities (id),
    created_at          timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX page_revisions_page_id_created_at_idx ON page_revisions (page_id, created_at DESC);

-- Logged only when a cleanup run actually deletes something (Decision 9) —
-- enforced by the Go job only inserting a row when deleted_count > 0.
CREATE TABLE history_cleanup_logs (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    space_id            uuid NOT NULL REFERENCES spaces (id) ON DELETE CASCADE,
    deleted_count       integer NOT NULL CHECK (deleted_count > 0),
    oldest_deleted_at   timestamptz NOT NULL,
    newest_deleted_at   timestamptz NOT NULL,
    ran_at              timestamptz NOT NULL DEFAULT now()
);

-- Attachments reference S3-compatible object storage by key; the binary
-- itself never lands in Postgres (Decision 5). Allowlist/size-limit
-- enforcement (Decision 17) happens in the Go layer against instance
-- settings, not as a DB constraint.
CREATE TABLE attachments (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    page_id             uuid NOT NULL REFERENCES pages (id) ON DELETE CASCADE,
    s3_key              text NOT NULL,
    filename            text NOT NULL,
    content_type        text NOT NULL,
    size_bytes          bigint NOT NULL,
    uploaded_by_user_id uuid REFERENCES users (id),
    uploaded_by_guest_id uuid REFERENCES guest_identities (id),
    created_at          timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX attachments_page_id_idx ON attachments (page_id);

-- Opaque random token, not the page's real ID (SPEC.md §6.6). Scope defaults
-- to the page and its descendants (Decision 21) — resolved by walking the
-- page tree from page_id in the Go layer, not stored per-descendant here.
CREATE TABLE share_links (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    page_id         uuid NOT NULL REFERENCES pages (id) ON DELETE CASCADE,
    token           text NOT NULL UNIQUE,
    permission      share_link_permission NOT NULL,
    password_hash   text,
    expires_at      timestamptz,
    revoked_at      timestamptz,
    created_by_user_id uuid NOT NULL REFERENCES users (id),
    created_at      timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX share_links_page_id_idx ON share_links (page_id);

-- Visible to space admins/owners and the page's editors, not just admins
-- (Decision 16) — an authorization rule enforced in the Go layer's query,
-- not a column here. IP is stored hashed, never raw (SPEC.md §8).
CREATE TABLE share_link_access_logs (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    share_link_id       uuid NOT NULL REFERENCES share_links (id) ON DELETE CASCADE,
    accessed_at         timestamptz NOT NULL DEFAULT now(),
    ip_hash             text NOT NULL,
    user_agent          text,
    viewer_user_id      uuid REFERENCES users (id),
    viewer_guest_id     uuid REFERENCES guest_identities (id)
);

CREATE INDEX share_link_access_logs_link_id_idx ON share_link_access_logs (share_link_id, accessed_at DESC);

-- Sessions back both authenticated and guest identities. Never both null,
-- never both set.
CREATE TABLE sessions (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         uuid REFERENCES users (id) ON DELETE CASCADE,
    guest_identity_id uuid REFERENCES guest_identities (id) ON DELETE CASCADE,
    token_hash      text NOT NULL UNIQUE,
    expires_at      timestamptz NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT sessions_exactly_one_principal CHECK (
        (user_id IS NOT NULL AND guest_identity_id IS NULL) OR (user_id IS NULL AND guest_identity_id IS NOT NULL)
    )
);

-- Admin-invite only, no SMTP dependency (Decision 11, 15) — an invite is a
-- token shared out-of-band by the admin, not an emailed link.
CREATE TABLE invites (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    token           text NOT NULL UNIQUE,
    email           text,
    grants_org_admin boolean NOT NULL DEFAULT false,
    created_by_user_id uuid NOT NULL REFERENCES users (id),
    expires_at      timestamptz,
    redeemed_at     timestamptz,
    redeemed_by_user_id uuid REFERENCES users (id),
    created_at      timestamptz NOT NULL DEFAULT now()
);
