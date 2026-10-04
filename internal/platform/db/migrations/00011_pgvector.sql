-- +goose Up
-- Meaning-based search in the database when pgvector is installed: vectors are also kept in a
-- `vector` column that HNSW indexes can use (created per embedding size by the application).
-- Without pgvector (or without the right to enable it) nothing changes: Docveta keeps scanning
-- the stored vectors itself, and picks up pgvector later if an administrator installs it.
-- +goose StatementBegin
DO $$
BEGIN
    BEGIN
        CREATE EXTENSION IF NOT EXISTS vector;
    EXCEPTION WHEN OTHERS THEN
        RAISE NOTICE 'pgvector is not available (%); meaning-based search runs in Docveta itself', SQLERRM;
    END;
    IF EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'vector') THEN
        EXECUTE 'ALTER TABLE embeddings ADD COLUMN IF NOT EXISTS embedding vector';
    END IF;
END $$;
-- +goose StatementEnd

-- Finding one document's chunks (re-embedding, "similar documents") and listing a space's.
CREATE INDEX IF NOT EXISTS embeddings_model_dims_idx ON embeddings (model, dims);

-- +goose Down
DROP INDEX IF EXISTS embeddings_model_dims_idx;
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'embeddings' AND column_name = 'embedding') THEN
        EXECUTE 'ALTER TABLE embeddings DROP COLUMN embedding';
    END IF;
END $$;
-- +goose StatementEnd
