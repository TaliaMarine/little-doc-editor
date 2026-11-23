-- Single-row instance-wide settings (SPEC.md Decision 14): an admin can
-- disable internal/password login once OIDC is configured. The break-glass
-- override that ignores this flag is an env var on the pod
-- (INTERNAL_LOGIN_BREAK_GLASS), not stored here — a DB-only toggle would be
-- a lockout risk if OIDC itself is what's misconfigured.
CREATE TABLE instance_settings (
    id                      boolean PRIMARY KEY DEFAULT true CHECK (id),
    internal_login_disabled boolean NOT NULL DEFAULT false,
    updated_at              timestamptz NOT NULL DEFAULT now()
);

INSERT INTO instance_settings (id) VALUES (true);
