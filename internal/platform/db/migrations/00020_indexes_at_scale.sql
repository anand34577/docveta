-- +goose NO TRANSACTION
-- +goose Up
-- Indexes for large libraries, built without blocking uploads.
-- Sorting by document date or by last change walks one of these per space instead of sorting
-- the whole library for every page (100 ms and growing at 200,000 documents, against 2 ms).
-- Undated documents sort as the earliest, as the search orders them. The date index replaces
-- documents_space_date_idx, which no query could use.
CREATE INDEX CONCURRENTLY IF NOT EXISTS documents_space_docdate_idx ON documents (space_id, (coalesce(document_date, '-infinity'::date)), id) WHERE deleted_at IS NULL;
CREATE INDEX CONCURRENTLY IF NOT EXISTS documents_space_updated_idx ON documents (space_id, updated_at, id) WHERE deleted_at IS NULL;
DROP INDEX CONCURRENTLY IF EXISTS documents_space_date_idx;
-- Maintenance removes notifications older than half a year, oldest first.
CREATE INDEX CONCURRENTLY IF NOT EXISTS notifications_created_idx ON notifications (created_at);
-- Administration → Audit log filtered to one kind of entry, among millions of sign-ins.
CREATE INDEX CONCURRENTLY IF NOT EXISTS audit_log_action_idx ON audit_log ((action COLLATE "C"), id);

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS audit_log_action_idx;
DROP INDEX CONCURRENTLY IF EXISTS notifications_created_idx;
CREATE INDEX CONCURRENTLY IF NOT EXISTS documents_space_date_idx ON documents (space_id, document_date DESC NULLS LAST, id DESC) WHERE deleted_at IS NULL;
DROP INDEX CONCURRENTLY IF EXISTS documents_space_updated_idx;
DROP INDEX CONCURRENTLY IF EXISTS documents_space_docdate_idx;
