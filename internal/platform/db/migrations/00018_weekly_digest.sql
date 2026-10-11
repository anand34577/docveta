-- +goose Up
-- The weekly summary goes out once per person per week (weeks start Monday in their time zone).
CREATE TABLE digests_sent (
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    week    date NOT NULL,
    sent_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, week)
);

-- +goose Down
DROP TABLE digests_sent;
