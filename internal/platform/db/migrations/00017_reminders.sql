-- +goose Up
-- Date fields set to remind (Expiry date, Renewal date…) notify the space's members some days
-- before the date. One row per reminder sent, so each one goes out once even though the check
-- runs every few minutes.
CREATE TABLE reminders_sent (
    document_id uuid NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    field_id    uuid NOT NULL REFERENCES custom_fields (id) ON DELETE CASCADE,
    due         date NOT NULL,
    lead_days   int  NOT NULL,
    sent_at     timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (document_id, field_id, due, lead_days)
);
CREATE INDEX reminders_sent_field_idx ON reminders_sent (field_id);

-- +goose Down
DROP TABLE reminders_sent;
