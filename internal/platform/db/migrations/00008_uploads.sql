-- +goose Up
-- Resumable uploads (tus protocol): the bytes live in <data>/uploads/<id>, this row tracks progress.
CREATE TABLE uploads (
    id          uuid PRIMARY KEY,
    user_id     uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    length      bigint NOT NULL,
    "offset"    bigint NOT NULL DEFAULT 0,
    metadata    jsonb NOT NULL DEFAULT '{}',
    expires_at  timestamptz NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX uploads_expires_idx ON uploads (expires_at);

-- +goose Down
DROP TABLE IF EXISTS uploads;
