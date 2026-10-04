-- +goose Up
-- Workflows: "when a document from HDFC is added, tag it Bank and tell me".
CREATE TABLE workflows (
    id            uuid PRIMARY KEY,
    space_id      uuid NOT NULL REFERENCES spaces (id) ON DELETE CASCADE,
    name          text NOT NULL,
    enabled       boolean NOT NULL DEFAULT true,
    trigger       text NOT NULL CHECK (trigger IN ('added', 'processed', 'updated', 'schedule')),
    schedule_time text NOT NULL DEFAULT '03:00', -- for trigger 'schedule': daily, server time
    conditions    jsonb NOT NULL DEFAULT '{}',   -- a search query: the document must match it
    actions       jsonb NOT NULL DEFAULT '[]',
    sort_order    int NOT NULL DEFAULT 0,
    created_by    uuid REFERENCES users (id) ON DELETE SET NULL,
    last_run_at   timestamptz,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX workflows_space_idx ON workflows (space_id, trigger) WHERE enabled;

CREATE TABLE workflow_runs (
    id           uuid PRIMARY KEY,
    workflow_id  uuid NOT NULL REFERENCES workflows (id) ON DELETE CASCADE,
    document_id  uuid REFERENCES documents (id) ON DELETE CASCADE,
    trigger      text NOT NULL,
    status       text NOT NULL CHECK (status IN ('done', 'skipped', 'failed')),
    summary      text NOT NULL DEFAULT '',
    ran_at       timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX workflow_runs_workflow_idx ON workflow_runs (workflow_id, ran_at DESC);
CREATE INDEX workflow_runs_document_idx ON workflow_runs (workflow_id, document_id);

-- +goose Down
DROP TABLE IF EXISTS workflow_runs, workflows;
