-- +goose Up
-- The documents a conversation is about ("Ask about this document"), so follow-up questions
-- and a conversation reopened later stay on them. Empty: all the user's documents.
ALTER TABLE ai_conversations ADD COLUMN document_ids uuid[] NOT NULL DEFAULT '{}';

-- +goose Down
ALTER TABLE ai_conversations DROP COLUMN document_ids;
