package app.docveta.android.data

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject

// Shapes of the Docveta REST API (see /api/v1/openapi.yaml). Every field the app doesn't need is left out.

@Serializable
data class Ref(val id: String, val name: String, val color: String = "")

/** Pages read so far; pagesTotal is 0 when only the worker knows the page count. */
@Serializable
data class Progress(@SerialName("pages_done") val pagesDone: Int = 0, @SerialName("pages_total") val pagesTotal: Int = 0)

@Serializable
data class Space(
    val id: String,
    val name: String,
    val kind: String = "shared", // personal | shared
    val description: String = "",
    val color: String = "indigo",
    val role: String = "viewer", // owner | editor | viewer
    @SerialName("member_count") val memberCount: Int = 0,
    @SerialName("document_count") val documentCount: Int = 0,
    @SerialName("split_on_separators") val splitOnSeparators: Boolean = false,
    @SerialName("read_asn_barcodes") val readAsnBarcodes: Boolean = false,
    @SerialName("ai_policy") val aiPolicy: String = "off", // off | local_only | any
    @SerialName("ai_apply_mode") val aiApplyMode: String = "suggest", // suggest | auto
    @SerialName("ai_new_tags") val aiNewTags: Boolean = true,
    @SerialName("ai_auto_confidence") val aiAutoConfidence: Int = 85, // % sure before "apply automatically" applies
    @SerialName("ai_new_confidence") val aiNewConfidence: Int = 60, // % sure before proposing a name that doesn't exist yet
    @SerialName("ai_max_new_tags") val aiMaxNewTags: Int = 3,
    @SerialName("ai_new_types") val aiNewTypes: Boolean = true,
    @SerialName("default_language") val defaultLanguage: String = "en",
) {
    val isPersonal get() = kind == "personal"
    val label get() = if (isPersonal) "Personal" else name
    val canWrite get() = role != "viewer"
    val isOwner get() = role == "owner"
}

@Serializable
data class Me(
    val id: String,
    val email: String,
    @SerialName("display_name") val displayName: String,
    @SerialName("is_admin") val isAdmin: Boolean = false,
    @SerialName("date_format") val dateFormat: String = "DD/MM/YYYY",
    val timezone: String = "UTC",
    val locale: String = "en",
    val theme: String = "system",
    @SerialName("has_password") val hasPassword: Boolean = true,
    val spaces: List<Space> = emptyList(),
)

@Serializable
data class ServerStatus(
    @SerialName("setup_needed") val setupNeeded: Boolean = false,
    val version: String = "",
    @SerialName("password_login") val passwordLogin: Boolean = true,
    val oidc: OidcStatus = OidcStatus(),
)

@Serializable
data class OidcStatus(val enabled: Boolean = false, @SerialName("button_label") val buttonLabel: String = "")

@Serializable
data class Segment(val text: String, val hit: Boolean = false)

@Serializable
data class CustomValue(
    @SerialName("field_id") val fieldId: String,
    val name: String,
    @SerialName("data_type") val dataType: String = "text",
    val value: JsonElement = JsonNull,
    val currency: String? = null,
)

@Serializable
data class Document(
    val id: String,
    val space: Ref,
    val title: String,
    @SerialName("document_date") val documentDate: String? = null,
    @SerialName("added_at") val addedAt: String = "",
    val correspondent: Ref? = null,
    @SerialName("document_type") val documentType: Ref? = null,
    val tags: List<Ref> = emptyList(),
    @SerialName("page_count") val pageCount: Int? = null,
    val inbox: Boolean = false,
    val status: String = "ready", // processing | ready | failed | needs_password
    @SerialName("processing_stage") val processingStage: String = "",
    @SerialName("processing_error") val processingError: String = "",
    val progress: Progress? = null, // while text is being read
    @SerialName("mime_type") val mimeType: String = "",
    @SerialName("size_bytes") val sizeBytes: Long = 0,
    @SerialName("original_filename") val originalFilename: String = "",
    @SerialName("has_archive") val hasArchive: Boolean = false,
    @SerialName("has_thumbnail") val hasThumbnail: Boolean = false,
    @SerialName("has_derived") val hasDerived: Boolean = false,
    @SerialName("custom_fields") val customFields: List<CustomValue> = emptyList(),
    @SerialName("suggestion_count") val suggestionCount: Int = 0,
    @SerialName("note_count") val noteCount: Int = 0,
    val version: Int = 1,
    @SerialName("deleted_at") val deletedAt: String? = null,
    val snippet: List<Segment> = emptyList(),
    @SerialName("matched_page") val matchedPage: Int? = null,
    val language: String = "",
    val asn: Long? = null,
    @SerialName("physical_location") val physicalLocation: String = "",
    val owner: Ref? = null,
    @SerialName("updated_at") val updatedAt: String = "",
) {
    val isPdf get() = mimeType == "application/pdf"
    val isImage get() = mimeType.startsWith("image/")
    /** Pages can be turned, reordered and removed (PDFs), or a picture turned (JPEG/PNG). */
    val pagesEditable get() = isPdf || mimeType == "image/jpeg" || mimeType == "image/png"
}

@Serializable
data class DocumentList(
    val items: List<Document> = emptyList(),
    val total: Int? = null,
    @SerialName("total_capped") val totalCapped: Boolean = false,
    @SerialName("next_cursor") val nextCursor: String? = null,
    val mode: String? = null,
    val facets: Facets? = null,
)

@Serializable
data class Facets(
    val tags: Map<String, Int> = emptyMap(),
    val correspondents: Map<String, Int> = emptyMap(),
    val types: Map<String, Int> = emptyMap(),
)

@Serializable
data class Stats(
    val total: Int = 0,
    val inbox: Int = 0,
    val processing: Int = 0,
    val failed: Int = 0,
    val trash: Int = 0,
    @SerialName("added_this_week") val addedThisWeek: Int = 0,
    @SerialName("ocr_available") val ocrAvailable: Boolean = true,
    @SerialName("trash_retention_days") val trashRetentionDays: Int = 30,
)

@Serializable
data class Taxonomy(
    val id: String,
    @SerialName("space_id") val spaceId: String = "",
    val name: String,
    val color: String = "",
    @SerialName("document_count") val documentCount: Int = 0,
    @SerialName("match_algorithm") val matchAlgorithm: String = "none", // none | any | all | exact | regex | fuzzy
    @SerialName("match_pattern") val matchPattern: String = "",
    @SerialName("case_sensitive") val caseSensitive: Boolean = false,
)

@Serializable
data class Note(
    val id: String,
    val author: Ref? = null,
    val body: String,
    @SerialName("created_at") val createdAt: String = "",
    @SerialName("can_delete") val canDelete: Boolean = false,
)

@Serializable
data class AiSuggestion(
    val id: String,
    val field: String, // tag | correspondent | document_type | document_date | title | custom_field
    val value: SuggestionValue = SuggestionValue(),
    val confidence: Double = 0.0,
)

@Serializable
data class SuggestionValue(val name: String = "", val id: String? = null, @SerialName("new") val isNew: Boolean = false, val field: String? = null)

@Serializable
data class Notification(
    val id: String,
    @SerialName("event_type") val eventType: String = "",
    val title: String,
    val body: String = "",
    val link: String = "",
    val severity: String = "info",
    @SerialName("read_at") val readAt: String? = null,
    @SerialName("created_at") val createdAt: String = "",
)

@Serializable
data class NotificationList(val items: List<Notification> = emptyList(), val unread: Int = 0)

@Serializable
data class Share(
    val id: String,
    val title: String = "",
    @SerialName("allow_download") val allowDownload: Boolean = true,
    @SerialName("has_password") val hasPassword: Boolean = false,
    @SerialName("expires_at") val expiresAt: String? = null,
    @SerialName("access_count") val accessCount: Int = 0,
    val status: String = "active",
)

@Serializable
data class ShareCreated(val share: Share, val link: String)

@Serializable
data class Citation(val n: Int, @SerialName("document_id") val documentId: String, val title: String, val page: Int = 1, val snippet: String = "")

@Serializable
data class Conversation(val id: String, val title: String = "", @SerialName("updated_at") val updatedAt: String = "", @SerialName("document_ids") val documentIds: List<String> = emptyList())

@Serializable
data class ConversationMessage(val id: String, val role: String, val content: String, val citations: List<Citation> = emptyList())

@Serializable
data class AiStatus(val enabled: Boolean = false, val chat: Boolean = false, val embeddings: Boolean = false)

@Serializable
data class VersionInfo(
    @SerialName("version_no") val versionNo: Int,
    val note: String = "",
    @SerialName("created_by") val createdBy: Ref? = null,
    @SerialName("created_at") val createdAt: String = "",
    @SerialName("size_bytes") val sizeBytes: Long = 0,
    @SerialName("mime_type") val mimeType: String = "",
    val current: Boolean = false,
)

@Serializable
data class HistoryEntry(
    val id: String,
    val actor: Ref? = null,
    @SerialName("actor_type") val actorType: String = "",
    val action: String,
    val details: JsonObject = JsonObject(emptyMap()),
    @SerialName("created_at") val createdAt: String = "",
)

@Serializable
data class PageText(val text: String = "", @SerialName("page_no") val pageNo: Int, val confidence: Double? = null, val rotation: Int = 0)

@Serializable
data class SimilarDoc(val document: Document, val score: Double = 0.0, val reason: String = "details")

@Serializable
data class CustomField(
    val id: String,
    @SerialName("space_id") val spaceId: String,
    val name: String,
    @SerialName("data_type") val dataType: String = "text",
    val options: FieldOptions = FieldOptions(),
    @SerialName("document_count") val documentCount: Int = 0,
)

@Serializable
data class FieldOptions(val choices: List<String> = emptyList(), val currency: String? = null)

@Serializable
data class BulkResult(val succeeded: Int = 0, val failed: List<BulkFailure> = emptyList(), val remaining: Int = 0)

@Serializable
data class BulkFailure(val id: String = "", val message: String = "")

@Serializable
data class SavedView(
    val id: String,
    @SerialName("space_id") val spaceId: String? = null,
    val name: String,
    val query: JsonObject = JsonObject(emptyMap()),
    val pinned: Boolean = false,
    @SerialName("sort_order") val sortOrder: Int = 0,
    @SerialName("can_edit") val canEdit: Boolean = true,
)

/* ---------------------------------------------------------------- spaces */

@Serializable
data class Member(
    @SerialName("user_id") val userId: String,
    val email: String = "",
    @SerialName("display_name") val displayName: String = "",
    val role: String = "viewer",
)

@Serializable
data class DirectoryEntry(val id: String, val email: String = "", @SerialName("display_name") val displayName: String = "")

@Serializable
data class AiStats(val accepted: Int = 0, val rejected: Int = 0, val pending: Int = 0, @SerialName("accept_rate") val acceptRate: Double = 0.0)

@Serializable
data class Workflow(
    val id: String,
    @SerialName("space_id") val spaceId: String,
    val name: String,
    val enabled: Boolean = true,
    val trigger: String = "added", // added | processed | updated | schedule
    @SerialName("schedule_time") val scheduleTime: String = "",
    val conditions: JsonObject = JsonObject(emptyMap()),
    val actions: List<JsonObject> = emptyList(),
    @SerialName("last_run_at") val lastRunAt: String? = null,
    @SerialName("run_count") val runCount: Int = 0,
)

@Serializable
data class WorkflowRun(
    val id: String,
    @SerialName("document_id") val documentId: String? = null,
    @SerialName("document_title") val documentTitle: String = "",
    val status: String = "done",
    val summary: String = "",
    @SerialName("ran_at") val ranAt: String = "",
)

/* ---------------------------------------------------------------- account */

@Serializable
data class Channel(
    val id: String,
    val system: Boolean = false,
    val name: String,
    val type: String, // gotify | email | ntfy | webhook | apprise
    val config: Map<String, String> = emptyMap(),
    val events: List<String> = emptyList(),
    val enabled: Boolean = true,
    @SerialName("last_error") val lastError: String = "",
)

@Serializable
data class ChannelList(val items: List<Channel> = emptyList(), @SerialName("event_types") val eventTypes: List<String> = emptyList(), @SerialName("email_ready") val emailReady: Boolean = true)

@Serializable
data class WebSessionInfo(
    val id: String,
    @SerialName("user_agent") val userAgent: String = "",
    val ip: String = "",
    @SerialName("last_seen_at") val lastSeenAt: String = "",
    val current: Boolean = false,
)

@Serializable
data class ApiToken(
    val id: String,
    val name: String,
    val prefix: String = "",
    val scopes: List<String> = emptyList(),
    @SerialName("expires_at") val expiresAt: String? = null,
    @SerialName("last_used_at") val lastUsedAt: String? = null,
)

@Serializable
data class Identity(val id: String, val provider: String = "", val email: String = "", @SerialName("created_at") val createdAt: String = "")

@Serializable
data class TwoFactorStatus(val enabled: Boolean = false, @SerialName("recovery_codes_left") val recoveryCodesLeft: Int = 0)

@Serializable
data class TwoFactorSetup(val secret: String = "", val uri: String = "", val qr: String = "")

@Serializable
data class RecoveryCodes(@SerialName("recovery_codes") val codes: List<String> = emptyList())

@Serializable
data class NotificationPrefs(
    @SerialName("quiet_enabled") val quietEnabled: Boolean = false,
    @SerialName("quiet_start") val quietStart: String = "22:00",
    @SerialName("quiet_end") val quietEnd: String = "07:00",
)

@Serializable
data class InvitePreview(
    val email: String? = null,
    @SerialName("display_name") val displayName: String = "",
    @SerialName("invited_by") val invitedBy: String = "",
    @SerialName("expires_at") val expiresAt: String = "",
    @SerialName("password_login") val passwordLogin: Boolean = true,
)

/* ---------------------------------------------------------------- administration */

@Serializable
data class AdminUser(
    val id: String,
    val email: String,
    @SerialName("display_name") val displayName: String,
    @SerialName("is_admin") val isAdmin: Boolean = false,
    val status: String = "active",
    @SerialName("has_password") val hasPassword: Boolean = true,
    @SerialName("last_login_at") val lastLoginAt: String? = null,
)

@Serializable
data class Capability(val task_type: String = "", val engine: String = "", val languages: List<String> = emptyList(), val tags: List<String> = emptyList(), val concurrency: Int = 0)

@Serializable
data class Worker(
    val id: String,
    val name: String,
    val enabled: Boolean = true,
    val version: String = "",
    val host: String = "",
    val capabilities: List<Capability> = emptyList(),
    @SerialName("last_seen_at") val lastSeenAt: String? = null,
    val online: Boolean = false,
    @SerialName("active_tasks") val activeTasks: Int = 0,
    @SerialName("done_24h") val done24h: Int = 0,
    @SerialName("failed_24h") val failed24h: Int = 0,
)

@Serializable
data class WorkerToken(val token: String = "")

@Serializable
data class TaskView(
    val id: String,
    @SerialName("document_id") val documentId: String = "",
    @SerialName("document_title") val documentTitle: String = "",
    val type: String = "",
    val status: String = "",
    @SerialName("page_from") val pageFrom: Int? = null,
    @SerialName("page_to") val pageTo: Int? = null,
    val attempt: Int = 1,
    val worker: String? = null,
    @SerialName("last_error") val lastError: String = "",
)

@Serializable
data class QueueStats(val queued: Int = 0, val leased: Int = 0, @SerialName("failed_24h") val failed24h: Int = 0, @SerialName("done_24h") val done24h: Int = 0, @SerialName("pages_done_24h") val pagesDone24h: Int = 0)

@Serializable
data class TaskList(val items: List<TaskView> = emptyList(), val stats: QueueStats = QueueStats(), @SerialName("next_cursor") val nextCursor: String? = null)

@Serializable
data class ProcessingSettings(
    @SerialName("prefer_tags") val preferTags: List<String> = emptyList(),
    @SerialName("fallback_after_minutes") val fallbackAfterMinutes: Int = 0,
    @SerialName("page_batch_size") val pageBatchSize: Int = 20,
    @SerialName("skip_ocr_with_text") val skipOcrWithText: Boolean = true,
    val archive: Boolean = true,
    @SerialName("max_attempts") val maxAttempts: Int = 3,
    @SerialName("lease_seconds") val leaseSeconds: Int = 600,
    @SerialName("worker_offline_minutes") val workerOfflineMinutes: Int = 5,
)

@Serializable
data class OidcConfig(
    val enabled: Boolean = false,
    val issuer: String = "",
    @SerialName("client_id") val clientId: String = "",
    val scopes: List<String> = emptyList(),
    @SerialName("button_label") val buttonLabel: String = "",
    @SerialName("auto_provision") val autoProvision: Boolean = false,
    @SerialName("allowed_groups") val allowedGroups: List<String> = emptyList(),
    @SerialName("admin_groups") val adminGroups: List<String> = emptyList(),
    @SerialName("groups_claim") val groupsClaim: String = "groups",
    @SerialName("link_by_verified_email") val linkByVerifiedEmail: Boolean = false,
    @SerialName("disable_password_login") val disablePasswordLogin: Boolean = false,
    @SerialName("has_client_secret") val hasClientSecret: Boolean = false,
    @SerialName("redirect_uri") val redirectUri: String? = null,
)

@Serializable
data class SmtpConfig(
    val enabled: Boolean = false,
    val host: String = "",
    val port: Int = 587,
    val security: String = "starttls",
    val username: String = "",
    val from: String = "",
    @SerialName("has_password") val hasPassword: Boolean = false,
)

@Serializable
data class ServerSettings(
    @SerialName("public_url") val publicUrl: String = "",
    @SerialName("allow_local_targets") val allowLocalTargets: Boolean = false,
    @SerialName("base_url") val baseUrl: String = "",
    @SerialName("base_url_fixed") val baseUrlFixed: Boolean = false,
    val detected: String = "",
    @SerialName("allow_local_env") val allowLocalEnv: Boolean = false,
)

@Serializable
data class SystemInfo(
    val version: String = "",
    @SerialName("go_version") val goVersion: String = "",
    val platform: String = "",
    @SerialName("database_bytes") val databaseBytes: Long = 0,
    @SerialName("storage_free_bytes") val storageFreeBytes: Long = -1,
    val documents: Long = 0,
    val pages: Long = 0,
    val users: Int = 0,
    @SerialName("storage_bytes") val storageBytes: Long = 0,
    val queue: QueueStats = QueueStats(),
    @SerialName("workers_online") val workersOnline: Int = 0,
    @SerialName("workers_total") val workersTotal: Int = 0,
)

@Serializable
data class AuditEntry(
    val id: Long,
    val at: String = "",
    @SerialName("actor_name") val actorName: String = "",
    @SerialName("actor_type") val actorType: String = "",
    val action: String = "",
    val ip: String = "",
    val details: JsonObject? = null,
)

/** Server-wide AI settings (Administration → AI). */
@Serializable
data class AiTuning(
    @SerialName("common_types") val commonTypes: List<String> = emptyList(),
    @SerialName("ask_sources") val askSources: Int = 8,
    @SerialName("ask_sources_per_document") val askSourcesPerDocument: Int = 3,
    @SerialName("suggest_text_tokens") val suggestTextTokens: Int = 4000,
)

@Serializable
data class AiProvider(
    val id: String,
    val name: String,
    @SerialName("base_url") val baseUrl: String = "",
    @SerialName("chat_model") val chatModel: String = "",
    @SerialName("embedding_model") val embeddingModel: String = "",
    @SerialName("is_local") val isLocal: Boolean = false,
    @SerialName("is_default") val isDefault: Boolean = false,
    val enabled: Boolean = true,
    @SerialName("timeout_seconds") val timeoutSeconds: Int = 60,
    @SerialName("max_concurrency") val maxConcurrency: Int = 2,
    @SerialName("context_tokens") val contextTokens: Int = 8192,
    @SerialName("has_api_key") val hasApiKey: Boolean = false,
    @SerialName("last_error") val lastError: String = "",
    @SerialName("last_ok_at") val lastOkAt: String? = null,
)

@Serializable
data class AiTestResult(
    val ok: Boolean = false,
    val error: String? = null,
    @SerialName("chat_model_found") val chatModelFound: Boolean? = null,
    @SerialName("embedding_model_found") val embeddingModelFound: Boolean? = null,
)

@Serializable
data class InviteSpace(@SerialName("space_id") val spaceId: String, val role: String = "editor", val name: String? = null)

@Serializable
data class Invite(
    val id: String,
    val email: String? = null,
    @SerialName("display_name") val displayName: String = "",
    val spaces: List<InviteSpace> = emptyList(),
    @SerialName("invited_by") val invitedBy: String = "",
    @SerialName("expires_at") val expiresAt: String = "",
    val status: String = "pending",
)

@Serializable
data class InviteCreated(val link: String = "", @SerialName("email_sent") val emailSent: Boolean = false, @SerialName("email_error") val emailError: String? = null)

@Serializable
data class WatchedFolder(
    val id: String,
    val path: String,
    @SerialName("space_id") val spaceId: String = "",
    val enabled: Boolean = true,
    val recursive: Boolean = true,
    val subfolders: String = "none",
    @SerialName("tag_ids") val tagIds: List<String> = emptyList(),
    @SerialName("after_import") val afterImport: String = "move",
    @SerialName("stable_seconds") val stableSeconds: Int = 5,
    @SerialName("last_scan_at") val lastScanAt: String? = null,
    @SerialName("last_error") val lastError: String = "",
    @SerialName("imported_count") val importedCount: Int = 0,
    @SerialName("failed_count") val failedCount: Int = 0,
)

@Serializable
data class FolderList(val items: List<WatchedFolder> = emptyList(), val roots: List<String> = emptyList())

@Serializable
data class ScanResult(val imported: Int = 0, val failed: Int = 0, val skipped: Int = 0)

@Serializable
data class OfficeInfo(val url: String = "", @SerialName("from_env") val fromEnv: Boolean = false, val enabled: Boolean = false)

@Serializable
data class Queued(val queued: Int = 0)

@Serializable
data class Items<T>(val items: List<T> = emptyList())

@Serializable
data class LoginReply(
    @SerialName("two_factor_required") val twoFactorRequired: Boolean = false,
    val challenge: String? = null,
)

@Serializable
data class NewToken(val secret: String, val token: TokenRef? = null)

@Serializable
data class TokenRef(val id: String, val scopes: List<String> = emptyList())

@Serializable
data class ApiProblem(
    val title: String = "",
    val code: String = "error",
    val errors: List<FieldProblem> = emptyList(),
    val extra: Map<String, JsonElement> = emptyMap(),
)

@Serializable
data class FieldProblem(val field: String = "", val message: String = "")
