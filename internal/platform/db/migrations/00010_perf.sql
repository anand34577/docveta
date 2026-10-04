-- +goose Up
-- Sorting by title walks this index instead of sorting a whole space.
CREATE INDEX documents_space_title_idx ON documents (space_id, lower(title), id) WHERE deleted_at IS NULL;

-- +goose Down
DROP INDEX documents_space_title_idx;
