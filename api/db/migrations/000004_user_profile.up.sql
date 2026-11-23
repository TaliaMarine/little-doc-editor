-- Profile information, filled from the OIDC claims on every login.
ALTER TABLE users
    ADD COLUMN username      text,
    ADD COLUMN avatar_url    text,
    ADD COLUMN idp_groups    text[] NOT NULL DEFAULT '{}',
    ADD COLUMN last_login_at timestamptz;
