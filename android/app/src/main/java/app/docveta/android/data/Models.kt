package app.docveta.android.data

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull

// Shapes of the Docveta REST API (see /api/v1/openapi.yaml). Every field the app doesn't need is left out.

@Serializable
data class Ref(val id: String, val name: String, val color: String = "")

@Serializable
data class Space(
    val id: String,
    val name: String,
    val kind: String = "shared", // personal | shared
    val color: String = "indigo",
    val role: String = "viewer", // owner | editor | viewer
    @SerialName("member_count") val memberCount: Int = 0,
    @SerialName("document_count") val documentCount: Int = 0,
) {
    val isPersonal get() = kind == "personal"
    val label get() = if (isPersonal) "Personal" else name
    val canWrite get() = role != "viewer"
}

@Serializable
data class Me(
    val id: String,
    val email: String,
    @SerialName("display_name") val displayName: String,
    @SerialName("is_admin") val isAdmin: Boolean = false,
    @SerialName("date_format") val dateFormat: String = "DD/MM/YYYY",
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
) {
    val isPdf get() = mimeType == "application/pdf"
    val isImage get() = mimeType.startsWith("image/")
}

@Serializable
data class DocumentList(
    val items: List<Document> = emptyList(),
    val total: Int? = null,
    @SerialName("next_cursor") val nextCursor: String? = null,
    val mode: String? = null,
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
data class Conversation(val id: String, val title: String = "", @SerialName("updated_at") val updatedAt: String = "")

@Serializable
data class ConversationMessage(val id: String, val role: String, val content: String, val citations: List<Citation> = emptyList())

@Serializable
data class AiStatus(val enabled: Boolean = false, val chat: Boolean = false, val embeddings: Boolean = false)

@Serializable
data class VersionInfo(
    @SerialName("version_no") val versionNo: Int,
    val note: String = "",
    @SerialName("created_at") val createdAt: String = "",
    @SerialName("size_bytes") val sizeBytes: Long = 0,
    val current: Boolean = false,
)

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
data class TokenRef(val id: String)

@Serializable
data class ApiProblem(
    val title: String = "",
    val code: String = "error",
    val errors: List<FieldProblem> = emptyList(),
    val extra: Map<String, JsonElement> = emptyMap(),
)

@Serializable
data class FieldProblem(val field: String = "", val message: String = "")
