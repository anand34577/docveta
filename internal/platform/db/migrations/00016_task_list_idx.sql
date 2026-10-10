-- +goose NO TRANSACTION
-- +goose Up
-- Administration → Processing pages through tasks newest first within each status, and counts
-- the last day's finished ones. With millions of tasks both need an index, built without
-- blocking uploads on a large installation.
CREATE INDEX CONCURRENTLY IF NOT EXISTS processing_tasks_status_updated_idx ON processing_tasks (status, updated_at DESC, id DESC);
CREATE INDEX CONCURRENTLY IF NOT EXISTS processing_tasks_finished_idx ON processing_tasks (status, finished_at) WHERE status IN ('done', 'failed');

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS processing_tasks_finished_idx;
DROP INDEX CONCURRENTLY IF EXISTS processing_tasks_status_updated_idx;
