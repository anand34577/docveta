-- +goose Up
-- Watched folders: files dropped into a folder (scanner, SMB share) become documents.
CREATE TABLE watched_folders (
    id             uuid PRIMARY KEY,
    path           text NOT NULL UNIQUE,
    space_id       uuid NOT NULL REFERENCES spaces (id) ON DELETE CASCADE,
    created_by     uuid REFERENCES users (id) ON DELETE SET NULL,
    enabled        boolean NOT NULL DEFAULT true,
    recursive      boolean NOT NULL DEFAULT true,
    subfolders     text NOT NULL DEFAULT 'none' CHECK (subfolders IN ('none', 'tag', 'space')),
    tag_ids        uuid[] NOT NULL DEFAULT '{}',
    after_import   text NOT NULL DEFAULT 'move' CHECK (after_import IN ('move', 'delete')),
    stable_seconds int NOT NULL DEFAULT 10,
    last_scan_at   timestamptz,
    last_error     text NOT NULL DEFAULT '',
    imported_count int NOT NULL DEFAULT 0,
    failed_count   int NOT NULL DEFAULT 0,
    created_at     timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE IF EXISTS watched_folders;
