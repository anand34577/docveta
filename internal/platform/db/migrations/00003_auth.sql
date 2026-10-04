-- +goose Up
-- Invitations: admins send a link instead of typing passwords for other people.
CREATE TABLE invites (
    id           uuid PRIMARY KEY,
    token_hash   bytea NOT NULL UNIQUE,
    email        citext,
    display_name text NOT NULL DEFAULT '',
    is_admin     boolean NOT NULL DEFAULT false,
    spaces       jsonb NOT NULL DEFAULT '[]', -- [{"space_id": "...", "role": "editor"}]
    note         text NOT NULL DEFAULT '',
    created_by   uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    expires_at   timestamptz NOT NULL,
    accepted_at  timestamptz,
    accepted_by  uuid REFERENCES users (id) ON DELETE SET NULL,
    revoked_at   timestamptz
);
CREATE INDEX invites_created_idx ON invites (created_at DESC);

-- Two-factor sign-in (TOTP, RFC 6238) with one-time recovery codes.
CREATE TABLE user_totp (
    user_id      uuid PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    secret_enc   bytea NOT NULL,
    confirmed_at timestamptz,            -- NULL while the user is still enrolling
    last_counter bigint NOT NULL DEFAULT 0, -- a code can't be used twice
    created_at   timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE recovery_codes (
    id        uuid PRIMARY KEY,
    user_id   uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    code_hash bytea NOT NULL,
    used_at   timestamptz
);
CREATE INDEX recovery_codes_user_idx ON recovery_codes (user_id);

-- +goose Down
DROP TABLE IF EXISTS recovery_codes, user_totp, invites;
