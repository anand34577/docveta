-- +goose Up
-- Docveta initial schema. See docs/DESIGN.md §9 for the model and rationale.

CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE EXTENSION IF NOT EXISTS citext;

-- Global monotonic change sequence used for delta sync (DESIGN §21.3).
CREATE SEQUENCE change_seq;

------------------------------------------------------------------------------
-- Identity
------------------------------------------------------------------------------
CREATE TABLE users (
    id            uuid PRIMARY KEY,
    email         citext NOT NULL UNIQUE,
    display_name  text NOT NULL,
    password_hash text,
    is_admin      boolean NOT NULL DEFAULT false,
    status        text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
    locale        text NOT NULL DEFAULT 'en-IN',
    timezone      text NOT NULL DEFAULT 'Asia/Kolkata',
    date_format   text NOT NULL DEFAULT 'DD/MM/YYYY',
    theme         text NOT NULL DEFAULT 'system',
    last_login_at timestamptz,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE user_identities (
    id             uuid PRIMARY KEY,
    user_id        uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    provider       text NOT NULL,
    subject        text NOT NULL,
    email          text,
    email_verified boolean NOT NULL DEFAULT false,
    created_at     timestamptz NOT NULL DEFAULT now(),
    UNIQUE (provider, subject)
);

CREATE TABLE sessions (
    id           uuid PRIMARY KEY,
    user_id      uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token_hash   bytea NOT NULL UNIQUE,
    user_agent   text NOT NULL DEFAULT '',
    ip           text NOT NULL DEFAULT '',
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_seen_at timestamptz NOT NULL DEFAULT now(),
    expires_at   timestamptz NOT NULL,
    revoked_at   timestamptz
);
CREATE INDEX sessions_user_idx ON sessions (user_id);

CREATE TABLE api_tokens (
    id           uuid PRIMARY KEY,
    user_id      uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name         text NOT NULL,
    token_prefix text NOT NULL,
    token_hash   bytea NOT NULL UNIQUE,
    scopes       text[] NOT NULL DEFAULT '{}',
    expires_at   timestamptz,
    last_used_at timestamptz,
    revoked_at   timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX api_tokens_user_idx ON api_tokens (user_id);

------------------------------------------------------------------------------
-- Spaces
------------------------------------------------------------------------------
CREATE TABLE spaces (
    id               uuid PRIMARY KEY,
    name             text NOT NULL,
    kind             text NOT NULL CHECK (kind IN ('personal', 'shared')),
    description      text NOT NULL DEFAULT '',
    color            text NOT NULL DEFAULT 'indigo',
    ai_policy        text NOT NULL DEFAULT 'off' CHECK (ai_policy IN ('off', 'local_only', 'any')),
    ai_apply_mode    text NOT NULL DEFAULT 'suggest' CHECK (ai_apply_mode IN ('suggest', 'auto')),
    default_language text NOT NULL DEFAULT 'en',
    created_by       uuid REFERENCES users (id) ON DELETE SET NULL,
    change_seq       bigint NOT NULL DEFAULT nextval('change_seq'),
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE space_members (
    space_id   uuid NOT NULL REFERENCES spaces (id) ON DELETE CASCADE,
    user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    role       text NOT NULL CHECK (role IN ('owner', 'editor', 'viewer')),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (space_id, user_id)
);
CREATE INDEX space_members_user_idx ON space_members (user_id);

------------------------------------------------------------------------------
-- Taxonomy (scoped per space)
------------------------------------------------------------------------------
CREATE TABLE tags (
    id              uuid PRIMARY KEY,
    space_id        uuid NOT NULL REFERENCES spaces (id) ON DELETE CASCADE,
    name            text NOT NULL,
    color           text NOT NULL DEFAULT 'slate',
    match_algorithm text NOT NULL DEFAULT 'none'
        CHECK (match_algorithm IN ('none', 'any', 'all', 'exact', 'regex', 'fuzzy')),
    match_pattern   text NOT NULL DEFAULT '',
    case_sensitive  boolean NOT NULL DEFAULT false,
    change_seq      bigint NOT NULL DEFAULT nextval('change_seq'),
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX tags_space_name_uq ON tags (space_id, lower(name));

CREATE TABLE correspondents (
    id              uuid PRIMARY KEY,
    space_id        uuid NOT NULL REFERENCES spaces (id) ON DELETE CASCADE,
    name            text NOT NULL,
    match_algorithm text NOT NULL DEFAULT 'none'
        CHECK (match_algorithm IN ('none', 'any', 'all', 'exact', 'regex', 'fuzzy')),
    match_pattern   text NOT NULL DEFAULT '',
    case_sensitive  boolean NOT NULL DEFAULT false,
    change_seq      bigint NOT NULL DEFAULT nextval('change_seq'),
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX correspondents_space_name_uq ON correspondents (space_id, lower(name));

CREATE TABLE document_types (
    id              uuid PRIMARY KEY,
    space_id        uuid NOT NULL REFERENCES spaces (id) ON DELETE CASCADE,
    name            text NOT NULL,
    match_algorithm text NOT NULL DEFAULT 'none'
        CHECK (match_algorithm IN ('none', 'any', 'all', 'exact', 'regex', 'fuzzy')),
    match_pattern   text NOT NULL DEFAULT '',
    case_sensitive  boolean NOT NULL DEFAULT false,
    change_seq      bigint NOT NULL DEFAULT nextval('change_seq'),
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX document_types_space_name_uq ON document_types (space_id, lower(name));

CREATE TABLE custom_fields (
    id               uuid PRIMARY KEY,
    space_id         uuid NOT NULL REFERENCES spaces (id) ON DELETE CASCADE,
    name             text NOT NULL,
    data_type        text NOT NULL CHECK (data_type IN
        ('text', 'longtext', 'integer', 'decimal', 'monetary', 'date', 'boolean', 'url', 'select', 'multiselect', 'document')),
    options          jsonb NOT NULL DEFAULT '{}',
    remind           boolean NOT NULL DEFAULT false,
    remind_lead_days int[] NOT NULL DEFAULT '{30,7,1}',
    change_seq       bigint NOT NULL DEFAULT nextval('change_seq'),
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX custom_fields_space_name_uq ON custom_fields (space_id, lower(name));

------------------------------------------------------------------------------
-- Documents
------------------------------------------------------------------------------
CREATE SEQUENCE asn_seq;

CREATE TABLE documents (
    id                uuid PRIMARY KEY,
    space_id          uuid NOT NULL REFERENCES spaces (id),
    owner_id          uuid REFERENCES users (id) ON DELETE SET NULL,
    title             text NOT NULL,
    document_date     date,
    added_at          timestamptz NOT NULL DEFAULT now(),
    correspondent_id  uuid REFERENCES correspondents (id) ON DELETE SET NULL,
    document_type_id  uuid REFERENCES document_types (id) ON DELETE SET NULL,
    language          text NOT NULL DEFAULT 'en',
    page_count        int,
    asn               bigint UNIQUE,
    physical_location text NOT NULL DEFAULT '',
    inbox             boolean NOT NULL DEFAULT true,
    status            text NOT NULL DEFAULT 'processing'
        CHECK (status IN ('processing', 'ready', 'failed', 'needs_password')),
    processing_stage  text NOT NULL DEFAULT 'received',
    processing_error  text NOT NULL DEFAULT '',
    current_version   int NOT NULL DEFAULT 1,
    content_hash      bytea NOT NULL,
    mime_type         text NOT NULL,
    size_bytes        bigint NOT NULL,
    original_filename text NOT NULL DEFAULT '',
    source            text NOT NULL DEFAULT 'web',
    content           text NOT NULL DEFAULT '',
    meta_fts          tsvector NOT NULL DEFAULT ''::tsvector,
    content_fts       tsvector NOT NULL DEFAULT ''::tsvector,
    version           int NOT NULL DEFAULT 1,
    change_seq        bigint NOT NULL DEFAULT nextval('change_seq'),
    deleted_at        timestamptz,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX documents_space_idx ON documents (space_id, added_at DESC, id DESC) WHERE deleted_at IS NULL;
CREATE INDEX documents_space_date_idx ON documents (space_id, document_date DESC NULLS LAST, id DESC) WHERE deleted_at IS NULL;
CREATE INDEX documents_inbox_idx ON documents (space_id, added_at DESC) WHERE inbox AND deleted_at IS NULL;
CREATE INDEX documents_hash_idx ON documents (space_id, content_hash);
CREATE INDEX documents_correspondent_idx ON documents (correspondent_id) WHERE deleted_at IS NULL;
CREATE INDEX documents_type_idx ON documents (document_type_id) WHERE deleted_at IS NULL;
CREATE INDEX documents_status_idx ON documents (status) WHERE status <> 'ready';
CREATE INDEX documents_deleted_idx ON documents (deleted_at) WHERE deleted_at IS NOT NULL;
CREATE INDEX documents_change_seq_idx ON documents (change_seq);
CREATE INDEX documents_meta_fts_idx ON documents USING gin (meta_fts);
CREATE INDEX documents_content_fts_idx ON documents USING gin (content_fts);
CREATE INDEX documents_title_trgm_idx ON documents USING gin (lower(title) gin_trgm_ops);

CREATE TABLE document_files (
    id          uuid PRIMARY KEY,
    document_id uuid NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    kind        text NOT NULL CHECK (kind IN ('original', 'archive', 'thumbnail', 'derived')),
    version_no  int NOT NULL DEFAULT 1,
    blob_key    text NOT NULL,
    sha256      bytea NOT NULL,
    mime_type   text NOT NULL,
    size_bytes  bigint NOT NULL,
    width       int,
    height      int,
    created_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (document_id, kind, version_no)
);
CREATE INDEX document_files_blob_idx ON document_files (blob_key);

CREATE TABLE ocr_runs (
    id             uuid PRIMARY KEY,
    document_id    uuid NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    version_no     int NOT NULL,
    engine         text NOT NULL DEFAULT '',
    engine_version text NOT NULL DEFAULT '',
    profile        text NOT NULL DEFAULT 'default',
    status         text NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'running', 'done', 'failed', 'cancelled')),
    pages_total    int NOT NULL DEFAULT 0,
    pages_done     int NOT NULL DEFAULT 0,
    avg_confidence real,
    has_boxes      boolean NOT NULL DEFAULT false,
    is_current     boolean NOT NULL DEFAULT false,
    result_blob_key text, -- merged canonical OCR JSON for the whole document
    error          text NOT NULL DEFAULT '',
    started_at     timestamptz,
    finished_at    timestamptz,
    created_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX ocr_runs_document_idx ON ocr_runs (document_id);
CREATE UNIQUE INDEX ocr_runs_current_uq ON ocr_runs (document_id) WHERE is_current;

CREATE TABLE pages (
    document_id uuid NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    page_no     int NOT NULL,
    ocr_run_id  uuid REFERENCES ocr_runs (id) ON DELETE SET NULL,
    text        text NOT NULL DEFAULT '',
    confidence  real,
    rotation    int NOT NULL DEFAULT 0,
    fts         tsvector NOT NULL DEFAULT ''::tsvector,
    PRIMARY KEY (document_id, page_no)
);
CREATE INDEX pages_fts_idx ON pages USING gin (fts);

CREATE TABLE document_tags (
    document_id uuid NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    tag_id      uuid NOT NULL REFERENCES tags (id) ON DELETE CASCADE,
    source      text NOT NULL DEFAULT 'user' CHECK (source IN ('user', 'rule', 'ai', 'workflow')),
    PRIMARY KEY (document_id, tag_id)
);
CREATE INDEX document_tags_tag_idx ON document_tags (tag_id);

CREATE TABLE custom_field_values (
    document_id  uuid NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    field_id     uuid NOT NULL REFERENCES custom_fields (id) ON DELETE CASCADE,
    value_text   text,
    value_number numeric,
    value_date   date,
    value_bool   boolean,
    value_json   jsonb,
    source       text NOT NULL DEFAULT 'user',
    PRIMARY KEY (document_id, field_id)
);
CREATE INDEX cfv_field_date_idx ON custom_field_values (field_id, value_date) WHERE value_date IS NOT NULL;
CREATE INDEX cfv_field_number_idx ON custom_field_values (field_id, value_number) WHERE value_number IS NOT NULL;

-- Provenance of single-valued fields so automation never overwrites a human edit.
CREATE TABLE field_sources (
    document_id uuid NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    field       text NOT NULL,
    source      text NOT NULL,
    set_by      uuid REFERENCES users (id) ON DELETE SET NULL,
    set_at      timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (document_id, field)
);

CREATE TABLE notes (
    id          uuid PRIMARY KEY,
    document_id uuid NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    author_id   uuid REFERENCES users (id) ON DELETE SET NULL,
    body        text NOT NULL,
    mentions    uuid[] NOT NULL DEFAULT '{}',
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX notes_document_idx ON notes (document_id, created_at);

CREATE TABLE document_events (
    id          uuid PRIMARY KEY,
    document_id uuid NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    actor_id    uuid REFERENCES users (id) ON DELETE SET NULL,
    actor_type  text NOT NULL DEFAULT 'user',
    action      text NOT NULL,
    details     jsonb NOT NULL DEFAULT '{}',
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX document_events_document_idx ON document_events (document_id, created_at DESC);

CREATE TABLE saved_views (
    id         uuid PRIMARY KEY,
    space_id   uuid REFERENCES spaces (id) ON DELETE CASCADE,
    owner_id   uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name       text NOT NULL,
    query      jsonb NOT NULL DEFAULT '{}',
    display    jsonb NOT NULL DEFAULT '{}',
    pinned     boolean NOT NULL DEFAULT true,
    sort_order int NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX saved_views_owner_idx ON saved_views (owner_id);

-- Tombstones for delta sync of hard-deleted entities.
CREATE TABLE tombstones (
    entity_type text NOT NULL,
    entity_id   uuid NOT NULL,
    space_id    uuid, -- no FK: tombstones outlive their space
    change_seq  bigint NOT NULL DEFAULT nextval('change_seq'),
    deleted_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (entity_type, entity_id)
);
CREATE INDEX tombstones_seq_idx ON tombstones (change_seq);

------------------------------------------------------------------------------
-- Processing (external workers)
------------------------------------------------------------------------------
CREATE TABLE workers (
    id               uuid PRIMARY KEY,
    name             text NOT NULL UNIQUE,
    token_prefix     text NOT NULL,
    token_hash       bytea NOT NULL UNIQUE,
    enabled          boolean NOT NULL DEFAULT true,
    protocol_version int NOT NULL DEFAULT 0,
    version          text NOT NULL DEFAULT '',
    host             text NOT NULL DEFAULT '',
    capabilities     jsonb NOT NULL DEFAULT '[]',
    last_seen_at     timestamptz,
    offline_notified boolean NOT NULL DEFAULT false,
    created_at       timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE processing_tasks (
    id                 uuid PRIMARY KEY,
    document_id        uuid NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    ocr_run_id         uuid REFERENCES ocr_runs (id) ON DELETE CASCADE,
    type               text NOT NULL,
    mime_type          text NOT NULL,
    page_from          int,
    page_to            int,
    required_languages text[] NOT NULL DEFAULT '{}',
    required_tags      text[] NOT NULL DEFAULT '{}',
    fallback_at        timestamptz,
    payload            jsonb NOT NULL DEFAULT '{}',
    priority           smallint NOT NULL DEFAULT 2,
    status             text NOT NULL DEFAULT 'queued'
        CHECK (status IN ('queued', 'leased', 'done', 'failed', 'cancelled')),
    attempt            int NOT NULL DEFAULT 0,
    max_attempts       int NOT NULL DEFAULT 3,
    lease_id           uuid,
    worker_id          uuid REFERENCES workers (id) ON DELETE SET NULL,
    lease_expires_at   timestamptz,
    not_before         timestamptz NOT NULL DEFAULT now(),
    progress           jsonb NOT NULL DEFAULT '{}',
    result_blob_key    text,
    metrics            jsonb NOT NULL DEFAULT '{}',
    excluded_engines   text[] NOT NULL DEFAULT '{}',
    last_error         text NOT NULL DEFAULT '',
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),
    finished_at        timestamptz
);
CREATE INDEX processing_tasks_queue_idx ON processing_tasks (priority, created_at) WHERE status = 'queued'; -- 1 = most urgent
CREATE INDEX processing_tasks_lease_idx ON processing_tasks (lease_expires_at) WHERE status = 'leased';
CREATE INDEX processing_tasks_run_idx ON processing_tasks (ocr_run_id);
CREATE INDEX processing_tasks_document_idx ON processing_tasks (document_id);

------------------------------------------------------------------------------
-- Notifications
------------------------------------------------------------------------------
CREATE TABLE notification_channels (
    id               uuid PRIMARY KEY,
    owner_id         uuid REFERENCES users (id) ON DELETE CASCADE, -- NULL = system channel (admin managed)
    name             text NOT NULL,
    type             text NOT NULL CHECK (type IN ('gotify', 'email', 'ntfy', 'webhook')),
    config_encrypted bytea NOT NULL,
    events           text[] NOT NULL DEFAULT '{}', -- empty = all events the owner receives
    enabled          boolean NOT NULL DEFAULT true,
    failure_count    int NOT NULL DEFAULT 0,
    last_error       text NOT NULL DEFAULT '',
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX notification_channels_owner_idx ON notification_channels (owner_id);

CREATE TABLE notifications (
    id         uuid PRIMARY KEY,
    user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    event_type text NOT NULL,
    title      text NOT NULL,
    body       text NOT NULL DEFAULT '',
    link       text NOT NULL DEFAULT '',
    severity   text NOT NULL DEFAULT 'info',
    read_at    timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX notifications_user_idx ON notifications (user_id, created_at DESC);
CREATE INDEX notifications_unread_idx ON notifications (user_id) WHERE read_at IS NULL;

------------------------------------------------------------------------------
-- Platform
------------------------------------------------------------------------------
CREATE TABLE settings (
    key        text PRIMARY KEY,
    value      jsonb NOT NULL,
    updated_by uuid REFERENCES users (id) ON DELETE SET NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- Encrypted settings (SMTP password, OIDC client secret, ...).
CREATE TABLE secret_settings (
    key        text PRIMARY KEY,
    value      bytea NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE audit_log (
    id          bigserial PRIMARY KEY,
    at          timestamptz NOT NULL DEFAULT now(),
    actor_id    uuid,
    actor_type  text NOT NULL,
    action      text NOT NULL,
    target_type text NOT NULL DEFAULT '',
    target_id   text NOT NULL DEFAULT '',
    ip          text NOT NULL DEFAULT '',
    user_agent  text NOT NULL DEFAULT '',
    details     jsonb NOT NULL DEFAULT '{}'
);
CREATE INDEX audit_log_at_idx ON audit_log (at DESC);

------------------------------------------------------------------------------
-- Change tracking triggers (delta sync for mobile clients)
------------------------------------------------------------------------------
-- +goose StatementBegin
CREATE FUNCTION docveta_touch() RETURNS trigger AS $$
BEGIN
    NEW.change_seq := nextval('change_seq');
    NEW.updated_at := now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION docveta_tombstone() RETURNS trigger AS $$
BEGIN
    INSERT INTO tombstones (entity_type, entity_id, space_id)
    VALUES (TG_ARGV[0], OLD.id, OLD.space_id)
    ON CONFLICT (entity_type, entity_id) DO UPDATE
        SET change_seq = nextval('change_seq'), deleted_at = now();
    RETURN OLD;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER documents_touch BEFORE UPDATE ON documents FOR EACH ROW EXECUTE FUNCTION docveta_touch();
CREATE TRIGGER tags_touch BEFORE UPDATE ON tags FOR EACH ROW EXECUTE FUNCTION docveta_touch();
CREATE TRIGGER correspondents_touch BEFORE UPDATE ON correspondents FOR EACH ROW EXECUTE FUNCTION docveta_touch();
CREATE TRIGGER document_types_touch BEFORE UPDATE ON document_types FOR EACH ROW EXECUTE FUNCTION docveta_touch();
CREATE TRIGGER custom_fields_touch BEFORE UPDATE ON custom_fields FOR EACH ROW EXECUTE FUNCTION docveta_touch();
CREATE TRIGGER spaces_touch BEFORE UPDATE ON spaces FOR EACH ROW EXECUTE FUNCTION docveta_touch();

CREATE TRIGGER documents_tomb AFTER DELETE ON documents FOR EACH ROW EXECUTE FUNCTION docveta_tombstone('document');
CREATE TRIGGER tags_tomb AFTER DELETE ON tags FOR EACH ROW EXECUTE FUNCTION docveta_tombstone('tag');
CREATE TRIGGER correspondents_tomb AFTER DELETE ON correspondents FOR EACH ROW EXECUTE FUNCTION docveta_tombstone('correspondent');
CREATE TRIGGER document_types_tomb AFTER DELETE ON document_types FOR EACH ROW EXECUTE FUNCTION docveta_tombstone('document_type');
CREATE TRIGGER custom_fields_tomb AFTER DELETE ON custom_fields FOR EACH ROW EXECUTE FUNCTION docveta_tombstone('custom_field');

-- +goose Down
DROP FUNCTION IF EXISTS docveta_touch, docveta_tombstone CASCADE;
DROP TABLE IF EXISTS audit_log, secret_settings, settings, notifications, notification_channels,
    processing_tasks, workers, tombstones, saved_views, document_events, notes, field_sources,
    custom_field_values, document_tags, pages, ocr_runs, document_files, documents, custom_fields,
    document_types, correspondents, tags, space_members, spaces, api_tokens, sessions,
    user_identities, users CASCADE;
DROP SEQUENCE IF EXISTS asn_seq;
DROP SEQUENCE IF EXISTS change_seq;
