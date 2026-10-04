// Types mirroring the Docveta REST API (internal/*/ JSON structs).

export type UUID = string;

export interface Ref {
  id: UUID;
  name: string;
  color?: string;
}

export type SpaceRole = "owner" | "editor" | "viewer";

export interface Space {
  id: UUID;
  name: string;
  kind: "personal" | "shared";
  description: string;
  color: string;
  split_on_separators?: boolean;
  read_asn_barcodes?: boolean;
  ai_policy: "off" | "local_only" | "any";
  ai_apply_mode: "suggest" | "auto";
  default_language: string;
  role: SpaceRole;
  member_count: number;
  document_count: number;
  created_at: string;
}

export interface Member {
  user_id: UUID;
  email: string;
  display_name: string;
  role: SpaceRole;
  added_at: string;
}

export interface User {
  id: UUID;
  email: string;
  display_name: string;
  is_admin: boolean;
  status: "active" | "disabled";
  locale: string;
  timezone: string;
  date_format: string;
  theme: "system" | "light" | "dark";
  has_password: boolean;
  last_login_at: string | null;
  created_at: string;
}

export interface Me extends User {
  spaces: Space[];
}

export interface Status {
  setup_needed: boolean;
  version: string;
  password_login: boolean;
  oidc: { enabled: boolean; button_label: string };
}

export type DocStatus = "processing" | "ready" | "failed" | "needs_password";

export interface Segment {
  text: string;
  hit?: boolean;
}

export interface Document {
  id: UUID;
  space: Ref;
  title: string;
  document_date: string | null;
  added_at: string;
  updated_at: string;
  correspondent: Ref | null;
  document_type: Ref | null;
  tags: Ref[];
  language: string;
  page_count: number | null;
  asn: number | null;
  physical_location: string;
  inbox: boolean;
  status: DocStatus;
  processing_stage: string;
  processing_error?: string;
  mime_type: string;
  size_bytes: number;
  original_filename: string;
  source: string;
  owner: Ref | null;
  has_archive: boolean;
  has_thumbnail: boolean;
  note_count: number;
  version: number;
  has_derived?: boolean;
  custom_fields?: CustomValue[];
  suggestion_count?: number;
  deleted_at?: string | null;
  snippet?: Segment[];
  matched_page?: number;
}

export interface DocumentList {
  items: Document[];
  total?: number;
  next_cursor: string | null;
  mode?: "keyword" | "semantic" | "hybrid";
  facets?: Facets;
}

export interface TaxonomyItem {
  id: UUID;
  space_id: UUID;
  name: string;
  color?: string;
  match_algorithm: MatchAlgorithm;
  match_pattern: string;
  case_sensitive: boolean;
  document_count: number;
  updated_at: string;
}

export type MatchAlgorithm = "none" | "any" | "all" | "exact" | "regex" | "fuzzy";
export type TaxonomyKind = "tags" | "correspondents" | "document-types";

export interface Note {
  id: UUID;
  author: Ref | null;
  body: string;
  mentions: UUID[];
  created_at: string;
  can_delete: boolean;
}

export interface HistoryEntry {
  id: UUID;
  actor: Ref | null;
  actor_type: string;
  action: string;
  details: Record<string, unknown>;
  created_at: string;
}

export interface Page {
  page_no: number;
  text: string;
  confidence: number | null;
  rotation: number;
}

export interface Stats {
  total: number;
  inbox: number;
  processing: number;
  failed: number;
  trash: number;
  added_this_week: number;
  bytes: number;
  ocr_available?: boolean;
  trash_retention_days?: number;
}

export interface SavedView {
  id: UUID;
  space_id: UUID | null;
  owner_id: UUID;
  name: string;
  query: DocQuery;
  display: { layout?: "grid" | "list" };
  pinned: boolean;
  sort_order: number;
  can_edit: boolean;
  updated_at: string;
}

/** Filters used by the documents page; serialized into URL search params. */
export interface DocQuery {
  q?: string;
  space_id?: UUID[];
  tag_id?: UUID[];
  correspondent_id?: UUID[];
  document_type_id?: UUID[];
  date_from?: string;
  date_to?: string;
  inbox?: boolean;
  status?: string;
  untagged?: boolean;
  trash?: boolean;
  sort?: string;
  mode?: "keyword" | "semantic" | "hybrid";
}

export interface Notification {
  id: UUID;
  event_type: string;
  title: string;
  body: string;
  link: string;
  severity: "info" | "success" | "warning" | "error";
  read_at: string | null;
  created_at: string;
}

export interface Channel {
  id: UUID;
  system: boolean;
  name: string;
  type: "gotify" | "email" | "ntfy" | "webhook" | "apprise";
  config: Record<string, string>;
  events: string[];
  enabled: boolean;
  failure_count: number;
  last_error: string;
}

export interface Session {
  id: UUID;
  user_agent: string;
  ip: string;
  created_at: string;
  last_seen_at: string;
  expires_at: string;
  current: boolean;
}

export interface ApiToken {
  id: UUID;
  name: string;
  prefix: string;
  scopes: string[];
  expires_at: string | null;
  last_used_at: string | null;
  created_at: string;
}

export interface DirectoryEntry {
  id: UUID;
  email: string;
  display_name: string;
}

export interface Capability {
  task_type: string;
  engine: string;
  engine_version?: string;
  languages?: string[];
  input_mime?: string[];
  concurrency?: number;
  tags?: string[];
}

export interface Worker {
  id: UUID;
  name: string;
  token_prefix: string;
  enabled: boolean;
  protocol_version: number;
  version: string;
  host: string;
  capabilities: Capability[];
  last_seen_at: string | null;
  online: boolean;
  active_tasks: number;
  done_24h: number;
  failed_24h: number;
  created_at: string;
}

export interface TaskView {
  id: UUID;
  document_id: UUID;
  document_title: string;
  type: string;
  status: string;
  page_from: number | null;
  page_to: number | null;
  priority: number;
  attempt: number;
  worker: string | null;
  last_error: string;
  created_at: string;
  finished_at: string | null;
}

export interface QueueStats {
  queued: number;
  leased: number;
  failed_24h: number;
  done_24h: number;
  pages_done_24h: number;
}

export interface ProcessingSettings {
  prefer_tags: string[];
  fallback_after_minutes: number;
  page_batch_size: number;
  skip_ocr_with_text: boolean;
  archive: boolean;
  max_attempts: number;
  lease_seconds: number;
  worker_offline_minutes: number;
}

export interface OIDCConfig {
  enabled: boolean;
  issuer: string;
  client_id: string;
  scopes: string[];
  button_label: string;
  auto_provision: boolean;
  allowed_groups: string[];
  admin_groups: string[];
  groups_claim: string;
  link_by_verified_email: boolean;
  disable_password_login: boolean;
  has_client_secret: boolean;
  client_secret?: string;
}

export interface SMTPConfig {
  enabled: boolean;
  host: string;
  port: number;
  security: "starttls" | "tls" | "none";
  username: string;
  from: string;
  password?: string;
  has_password: boolean;
}

export interface SystemInfo {
  version: string;
  go_version: string;
  platform: string;
  database_bytes: number;
  storage_free_bytes: number;
  documents: number;
  pages: number;
  users: number;
  storage_bytes: number;
  queue: QueueStats;
  workers_online: number;
  workers_total: number;
}

export interface AuditEntry {
  id: number;
  at: string;
  actor_id: UUID | null;
  actor_name: string;
  actor_type: string;
  action: string;
  target_type: string;
  target_id: string;
  ip: string;
  user_agent: string;
  details: Record<string, unknown>;
}

/* ---- Custom fields, AI, sharing, invitations, workflows, folders ---- */

export type FieldType = "text" | "longtext" | "integer" | "decimal" | "monetary" | "date" | "boolean" | "url" | "select" | "multiselect" | "document";

export interface CustomField {
  id: UUID;
  space_id: UUID;
  name: string;
  data_type: FieldType;
  options: { choices?: string[]; currency?: string } & Record<string, unknown>;
  document_count: number;
}

export interface CustomValue {
  field_id: UUID;
  name: string;
  data_type: FieldType;
  value: unknown;
  currency?: string | null;
}

export interface AISuggestion {
  id: UUID;
  field: "tag" | "correspondent" | "document_type" | "document_date" | "title" | "custom_field";
  value: Record<string, unknown>;
  confidence: number;
  status: string;
}

export interface AIProvider {
  id: UUID;
  name: string;
  base_url: string;
  chat_model: string;
  embedding_model: string;
  is_local: boolean;
  is_default: boolean;
  enabled: boolean;
  timeout_seconds: number;
  max_concurrency: number;
  has_api_key: boolean;
  last_error: string;
  last_ok_at?: string | null;
}

export interface AITestResult {
  ok: boolean;
  error?: string | null;
  models: string[];
  chat_model_found?: boolean | null;
  embedding_model_found?: boolean | null;
}

export interface SimilarDoc {
  document: Document;
  score: number;
  reason: "meaning" | "details";
}

export interface Citation {
  n: number;
  document_id: UUID;
  title: string;
  page: number;
  snippet: string;
}

export interface Conversation {
  id: UUID;
  title: string;
  updated_at: string;
}

export interface ConversationMessage {
  id: UUID;
  role: "user" | "assistant";
  content: string;
  citations: Citation[];
  created_at: string;
}

export interface VersionInfo {
  version_no: number;
  note: string;
  created_by?: Ref | null;
  created_at: string;
  size_bytes: number;
  mime_type: string;
  current: boolean;
}

export interface Share {
  id: UUID;
  kind: "document" | "view";
  document_id?: UUID | null;
  view_id?: UUID | null;
  title: string;
  allow_download: boolean;
  has_password: boolean;
  note: string;
  expires_at?: string | null;
  access_count: number;
  last_access_at?: string | null;
  created_by: string;
  created_at: string;
  status: "active" | "expired" | "revoked";
}

export interface PublicDoc {
  id: UUID;
  title: string;
  document_date?: string | null;
  mime_type: string;
  page_count?: number | null;
  size_bytes: number;
  has_archive: boolean;
  has_thumbnail: boolean;
  has_derived: boolean;
}

export interface PublicShare {
  kind: string;
  title: string;
  requires_password: boolean;
  allow_download: boolean;
  expires_at?: string | null;
  documents: PublicDoc[];
}

export interface InviteSpace {
  space_id: UUID;
  role: SpaceRole;
  name?: string | null;
}

export interface Invite {
  id: UUID;
  email?: string | null;
  display_name: string;
  is_admin: boolean;
  spaces: InviteSpace[];
  note: string;
  invited_by: string;
  created_at: string;
  expires_at: string;
  accepted_at?: string | null;
  status: "pending" | "accepted" | "expired" | "revoked";
}

export interface InvitePreview {
  email?: string | null;
  display_name: string;
  invited_by: string;
  expires_at: string;
  password_login: boolean;
}

export interface WatchedFolder {
  id: UUID;
  path: string;
  space_id: UUID;
  enabled: boolean;
  recursive: boolean;
  subfolders: "none" | "tag" | "space";
  tag_ids: UUID[];
  after_import: "move" | "delete";
  stable_seconds: number;
  last_scan_at?: string | null;
  last_error: string;
  imported_count: number;
  failed_count: number;
}

export interface WorkflowAction {
  type: "add_tags" | "remove_tags" | "set_correspondent" | "set_document_type" | "set_field" | "set_inbox" | "move_to_space" | "notify" | "webhook" | "run_ai";
  names?: string[];
  name?: string;
  field_id?: UUID;
  value?: unknown;
  space_id?: UUID;
  to?: "owner" | "space_owners" | "space_members";
  title?: string;
  message?: string;
  url?: string;
}

export interface Workflow {
  id: UUID;
  space_id: UUID;
  name: string;
  enabled: boolean;
  trigger: "added" | "processed" | "updated" | "schedule";
  schedule_time: string;
  conditions: Record<string, unknown>;
  actions: WorkflowAction[];
  last_run_at?: string | null;
  run_count: number;
}

export interface WorkflowRun {
  id: UUID;
  document_id?: UUID | null;
  document_title: string;
  trigger: string;
  status: "done" | "skipped" | "failed";
  summary: string;
  ran_at: string;
}

export interface Facets {
  tags: Record<string, number>;
  correspondents: Record<string, number>;
  types: Record<string, number>;
  statuses: Record<string, number>;
}

export interface NotificationPrefs {
  quiet_enabled: boolean;
  quiet_start: string;
  quiet_end: string;
}
