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
  deleted_at?: string | null;
  snippet?: Segment[];
  matched_page?: number;
}

export interface DocumentList {
  items: Document[];
  total?: number;
  next_cursor: string | null;
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
  type: "gotify" | "email" | "ntfy" | "webhook";
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
