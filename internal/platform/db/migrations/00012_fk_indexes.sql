-- +goose Up
-- Indexes behind foreign keys. Without them, deleting a row PostgreSQL must cascade or null out
-- scans the whole referencing table: purging a document scanned every page of every document
-- (pages.ocr_run_id), and deleting a user scanned all documents, history and notes. Harmless
-- on small installs, essential once there are hundreds of thousands of pages.
CREATE INDEX IF NOT EXISTS pages_ocr_run_idx ON pages (ocr_run_id) WHERE ocr_run_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS documents_owner_idx ON documents (owner_id) WHERE owner_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS document_events_actor_idx ON document_events (actor_id) WHERE actor_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS notes_author_idx ON notes (author_id) WHERE author_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS field_sources_set_by_idx ON field_sources (set_by) WHERE set_by IS NOT NULL;
CREATE INDEX IF NOT EXISTS document_versions_created_by_idx ON document_versions (created_by) WHERE created_by IS NOT NULL;
CREATE INDEX IF NOT EXISTS processing_tasks_worker_idx ON processing_tasks (worker_id) WHERE worker_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS workflow_runs_doc_idx ON workflow_runs (document_id);
CREATE INDEX IF NOT EXISTS suggestions_provider_idx ON suggestions (provider_id) WHERE provider_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS suggestions_resolved_by_idx ON suggestions (resolved_by) WHERE resolved_by IS NOT NULL;
CREATE INDEX IF NOT EXISTS user_identities_user_idx ON user_identities (user_id);
CREATE INDEX IF NOT EXISTS shares_space_idx ON shares (space_id);
CREATE INDEX IF NOT EXISTS saved_views_space_idx ON saved_views (space_id) WHERE space_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS uploads_user_idx ON uploads (user_id);

-- +goose Down
DROP INDEX IF EXISTS pages_ocr_run_idx, documents_owner_idx, document_events_actor_idx, notes_author_idx, field_sources_set_by_idx,
    document_versions_created_by_idx, processing_tasks_worker_idx, workflow_runs_doc_idx, suggestions_provider_idx,
    suggestions_resolved_by_idx, user_identities_user_idx, shares_space_idx, saved_views_space_idx, uploads_user_idx;
