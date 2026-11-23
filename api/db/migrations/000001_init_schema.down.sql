DROP TABLE IF EXISTS invites;
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS share_link_access_logs;
DROP TABLE IF EXISTS share_links;
DROP TABLE IF EXISTS attachments;
DROP TABLE IF EXISTS history_cleanup_logs;
DROP TABLE IF EXISTS page_revisions;
DROP TABLE IF EXISTS pages;
DROP TABLE IF EXISTS guest_identities;
DROP TABLE IF EXISTS space_members;
DROP TABLE IF EXISTS spaces;
DROP TABLE IF EXISTS group_members;
DROP TABLE IF EXISTS groups;
DROP TABLE IF EXISTS users;

DROP TYPE IF EXISTS share_link_permission;
DROP TYPE IF EXISTS space_role;
DROP TYPE IF EXISTS auth_provider;

DROP EXTENSION IF EXISTS pg_trgm;
