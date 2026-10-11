-- +goose Up
-- Collections group related documents by hand (all the car papers: registration, insurance,
-- pollution certificate, service bills), across spaces. Personal (space_id NULL, only the owner)
-- or shared with a space (its editors change it, everyone in it sees it). Each person only ever
-- sees the documents in it that they can see anyway.
CREATE TABLE collections (
    id          uuid PRIMARY KEY,
    space_id    uuid REFERENCES spaces (id) ON DELETE CASCADE,
    owner_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name        text NOT NULL,
    description text NOT NULL DEFAULT '',
    color       text NOT NULL DEFAULT 'blue',
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX collections_space_idx ON collections (space_id);
CREATE INDEX collections_owner_idx ON collections (owner_id);

CREATE TABLE collection_items (
    collection_id uuid NOT NULL REFERENCES collections (id) ON DELETE CASCADE,
    document_id   uuid NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    added_by      uuid REFERENCES users (id) ON DELETE SET NULL,
    added_at      timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (collection_id, document_id)
);
CREATE INDEX collection_items_document_idx ON collection_items (document_id);
CREATE INDEX collection_items_added_by_idx ON collection_items (added_by);

-- +goose Down
DROP TABLE collection_items;
DROP TABLE collections;
