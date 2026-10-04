-- +goose Up
-- Change feed: a document that moves to another space is "removed" from the old one.
CREATE TABLE space_removals (
    entity_type text NOT NULL,
    entity_id   uuid NOT NULL,
    space_id    uuid NOT NULL, -- no FK: outlives the space
    change_seq  bigint NOT NULL DEFAULT nextval('change_seq'),
    removed_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (entity_type, entity_id, space_id)
);
CREATE INDEX space_removals_seq_idx ON space_removals (change_seq);

-- +goose StatementBegin
CREATE FUNCTION docveta_space_move() RETURNS trigger AS $$
BEGIN
    INSERT INTO space_removals (entity_type, entity_id, space_id)
    VALUES ('document', OLD.id, OLD.space_id)
    ON CONFLICT (entity_type, entity_id, space_id) DO UPDATE
        SET change_seq = nextval('change_seq'), removed_at = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER documents_space_move AFTER UPDATE OF space_id ON documents
    FOR EACH ROW WHEN (OLD.space_id IS DISTINCT FROM NEW.space_id) EXECUTE FUNCTION docveta_space_move();

-- Per-user notification preferences (quiet hours, digest).
ALTER TABLE users ADD COLUMN notify_prefs jsonb NOT NULL DEFAULT '{}';

-- Apprise bridge channel type.
ALTER TABLE notification_channels DROP CONSTRAINT notification_channels_type_check;
ALTER TABLE notification_channels ADD CONSTRAINT notification_channels_type_check
    CHECK (type IN ('gotify', 'email', 'ntfy', 'webhook', 'apprise'));

-- Document versions: every version of the original keeps its note and author.
CREATE TABLE document_versions (
    document_id uuid NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    version_no  int NOT NULL,
    note        text NOT NULL DEFAULT '',
    created_by  uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (document_id, version_no)
);
INSERT INTO document_versions (document_id, version_no, note, created_at)
    SELECT document_id, version_no, 'Uploaded', created_at FROM document_files WHERE kind = 'original';

-- +goose Down
DROP TABLE IF EXISTS document_versions;
ALTER TABLE notification_channels DROP CONSTRAINT notification_channels_type_check;
ALTER TABLE notification_channels ADD CONSTRAINT notification_channels_type_check
    CHECK (type IN ('gotify', 'email', 'ntfy', 'webhook'));
ALTER TABLE users DROP COLUMN notify_prefs;
DROP TRIGGER IF EXISTS documents_space_move ON documents;
DROP FUNCTION IF EXISTS docveta_space_move;
DROP TABLE IF EXISTS space_removals;
