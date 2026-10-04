-- +goose Up
-- Share links: let someone without an account view a document or a saved view.
CREATE TABLE shares (
    id             uuid PRIMARY KEY,
    token_hash     bytea NOT NULL UNIQUE,
    document_id    uuid REFERENCES documents (id) ON DELETE CASCADE,
    view_id        uuid REFERENCES saved_views (id) ON DELETE CASCADE,
    space_id       uuid NOT NULL REFERENCES spaces (id) ON DELETE CASCADE,
    created_by     uuid REFERENCES users (id) ON DELETE SET NULL,
    password_hash  text,
    allow_download boolean NOT NULL DEFAULT false,
    note           text NOT NULL DEFAULT '',
    expires_at     timestamptz,
    revoked_at     timestamptz,
    access_count   int NOT NULL DEFAULT 0,
    last_access_at timestamptz,
    created_at     timestamptz NOT NULL DEFAULT now(),
    CHECK ((document_id IS NOT NULL) <> (view_id IS NOT NULL))
);
CREATE INDEX shares_document_idx ON shares (document_id) WHERE document_id IS NOT NULL;
CREATE INDEX shares_view_idx ON shares (view_id) WHERE view_id IS NOT NULL;

-- +goose Down
DROP TABLE IF EXISTS shares;
