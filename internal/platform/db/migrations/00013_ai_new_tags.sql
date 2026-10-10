-- +goose Up
-- Whether the AI may propose tags that don't exist in the space yet (otherwise it only
-- picks from the space's own tags).
ALTER TABLE spaces ADD COLUMN ai_new_tags boolean NOT NULL DEFAULT true;

-- +goose Down
ALTER TABLE spaces DROP COLUMN ai_new_tags;
