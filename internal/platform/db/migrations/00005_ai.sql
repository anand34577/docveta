-- +goose Up
-- AI providers: any OpenAI-compatible server (Ollama, llama.cpp, vLLM, LM Studio, OpenAI, ...).
CREATE TABLE ai_providers (
    id              uuid PRIMARY KEY,
    name            text NOT NULL UNIQUE,
    base_url        text NOT NULL,           -- e.g. http://localhost:11434/v1
    api_key_enc     bytea,
    chat_model      text NOT NULL DEFAULT '',
    embedding_model text NOT NULL DEFAULT '',
    is_local        boolean NOT NULL DEFAULT false, -- runs on hardware you control: allowed by "local only" spaces
    is_default      boolean NOT NULL DEFAULT false,
    enabled         boolean NOT NULL DEFAULT true,
    timeout_seconds int NOT NULL DEFAULT 90,
    max_concurrency int NOT NULL DEFAULT 2,
    last_error      text NOT NULL DEFAULT '',
    last_ok_at      timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX ai_providers_default_uq ON ai_providers (is_default) WHERE is_default;

-- What the AI proposes for a document; accepted by a person or auto-applied.
CREATE TABLE suggestions (
    id          uuid PRIMARY KEY,
    document_id uuid NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    field       text NOT NULL CHECK (field IN ('tag', 'correspondent', 'document_type', 'document_date', 'title', 'custom_field')),
    value       jsonb NOT NULL,              -- {"name": "...", "id": "..."?, "new": bool?, "field_id": "..."?}
    confidence  real NOT NULL DEFAULT 0,
    status      text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'accepted', 'rejected', 'applied')),
    provider_id uuid REFERENCES ai_providers (id) ON DELETE SET NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    resolved_at timestamptz,
    resolved_by uuid REFERENCES users (id) ON DELETE SET NULL
);
CREATE INDEX suggestions_document_idx ON suggestions (document_id) WHERE status = 'pending';
CREATE INDEX suggestions_resolved_idx ON suggestions (document_id, status);

-- Text chunks and their embedding vectors (float32, little endian) for meaning-based search.
CREATE TABLE embeddings (
    document_id uuid NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    model       text NOT NULL,
    chunk_no    int NOT NULL,
    page_no     int NOT NULL DEFAULT 1,
    content     text NOT NULL,
    vec         bytea NOT NULL,
    dims        int NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (document_id, model, chunk_no)
);
CREATE INDEX embeddings_model_idx ON embeddings (model);

-- "Ask your documents": per-user conversations with cited answers.
CREATE TABLE ai_conversations (
    id         uuid PRIMARY KEY,
    user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    title      text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX ai_conversations_user_idx ON ai_conversations (user_id, updated_at DESC);

CREATE TABLE ai_messages (
    id              uuid PRIMARY KEY,
    conversation_id uuid NOT NULL REFERENCES ai_conversations (id) ON DELETE CASCADE,
    role            text NOT NULL CHECK (role IN ('user', 'assistant')),
    content         text NOT NULL,
    citations       jsonb NOT NULL DEFAULT '[]',
    created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX ai_messages_conversation_idx ON ai_messages (conversation_id, created_at);

-- +goose Down
DROP TABLE IF EXISTS ai_messages, ai_conversations, embeddings, suggestions, ai_providers;
