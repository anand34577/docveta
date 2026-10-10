-- +goose Up
-- How much text the provider's chat model can take in one request (its context window, in
-- tokens). Prompts are cut to fit. Local models often run with 4096.
ALTER TABLE ai_providers ADD COLUMN context_tokens int NOT NULL DEFAULT 8192;

-- How a space wants its AI to behave (Space settings → AI assistance).
ALTER TABLE spaces ADD COLUMN ai_auto_confidence smallint NOT NULL DEFAULT 85 CHECK (ai_auto_confidence BETWEEN 50 AND 100); -- % sure before "apply automatically" applies
ALTER TABLE spaces ADD COLUMN ai_new_confidence smallint NOT NULL DEFAULT 60 CHECK (ai_new_confidence BETWEEN 0 AND 100);    -- % sure before proposing a name that doesn't exist yet
ALTER TABLE spaces ADD COLUMN ai_max_new_tags smallint NOT NULL DEFAULT 3 CHECK (ai_max_new_tags BETWEEN 1 AND 10);         -- new tags per document
ALTER TABLE spaces ADD COLUMN ai_new_types boolean NOT NULL DEFAULT true;                                                   -- may propose document types the space doesn't have

-- +goose Down
ALTER TABLE spaces DROP COLUMN ai_new_types;
ALTER TABLE spaces DROP COLUMN ai_max_new_tags;
ALTER TABLE spaces DROP COLUMN ai_new_confidence;
ALTER TABLE spaces DROP COLUMN ai_auto_confidence;
ALTER TABLE ai_providers DROP COLUMN context_tokens;
