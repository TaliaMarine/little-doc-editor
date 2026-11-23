-- Periodic Yjs document-state snapshots written by the `collab` sidecar
-- (SPEC.md §4): "persists periodic snapshots + update log to Postgres ...
-- so the api service can read consistent content for non-realtime
-- consumers (search indexing, export, revision snapshots)". `state` is the
-- raw Yjs update (Y.encodeStateAsUpdate), opaque to the api service
-- (Decision 1) — it relays/persists it without implementing CRDT logic.
CREATE TABLE collab_documents (
    page_id     uuid PRIMARY KEY REFERENCES pages (id) ON DELETE CASCADE,
    state       bytea NOT NULL,
    updated_at  timestamptz NOT NULL DEFAULT now()
);
