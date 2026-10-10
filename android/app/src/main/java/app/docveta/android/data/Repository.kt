package app.docveta.android.data

import java.io.File
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.channels.awaitClose
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.callbackFlow
import kotlinx.coroutines.flow.flowOn
import kotlinx.coroutines.launch
import kotlinx.serialization.builtins.ListSerializer
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import kotlinx.serialization.json.putJsonArray
import okhttp3.Cookie
import okhttp3.CookieJar
import okhttp3.HttpUrl
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody

/** What a documents screen asks for. The same shape as a saved view's query on the web. */
data class DocQuery(
    val q: String = "",
    val spaceId: String? = null,
    val tagIds: List<String> = emptyList(),
    val correspondentIds: List<String> = emptyList(),
    val typeIds: List<String> = emptyList(),
    val dateFrom: String? = null,
    val dateTo: String? = null,
    val untagged: Boolean = false,
    val inbox: Boolean? = null,
    val status: String? = null,
    val trash: Boolean = false,
    val sort: String? = null,
    val semantic: Boolean = false,
) {
    /** How many filters (not the search text or sort) are on. */
    val filterCount: Int
        get() = tagIds.size + correspondentIds.size + typeIds.size + (if (spaceId != null) 1 else 0) + (if (status != null) 1 else 0) +
            (if (dateFrom != null || dateTo != null) 1 else 0) + (if (untagged) 1 else 0)

    fun clearedFilters() = DocQuery(q = q, sort = sort, semantic = semantic, inbox = inbox, trash = trash)

    /** The web app's saved-view query (arrays for ids, strings for dates). */
    fun toJson(): JsonObject = buildJsonObject {
        if (q.isNotBlank()) put("q", q.trim())
        fun ids(k: String, l: List<String>) { if (l.isNotEmpty()) put(k, JsonArray(l.map { JsonPrimitive(it) })) }
        ids("space_id", listOfNotNull(spaceId))
        ids("tag_id", tagIds)
        ids("correspondent_id", correspondentIds)
        ids("document_type_id", typeIds)
        dateFrom?.let { put("date_from", it) }
        dateTo?.let { put("date_to", it) }
        inbox?.let { put("inbox", it) }
        status?.let { put("status", it) }
        if (untagged) put("untagged", true)
        if (trash) put("trash", true)
        sort?.let { put("sort", it) }
        if (semantic) put("mode", "hybrid")
    }

    companion object {
        fun fromJson(o: JsonObject): DocQuery {
            fun str(k: String) = (o[k] as? JsonPrimitive)?.takeIf { it.isString }?.content
            fun bool(k: String) = (o[k] as? JsonPrimitive)?.content?.toBooleanStrictOrNull()
            fun ids(k: String): List<String> = when (val v = o[k]) {
                is JsonArray -> v.mapNotNull { (it as? JsonPrimitive)?.content }
                is JsonPrimitive -> listOf(v.content)
                else -> emptyList()
            }
            return DocQuery(
                q = str("q").orEmpty(), spaceId = ids("space_id").firstOrNull(), tagIds = ids("tag_id"), correspondentIds = ids("correspondent_id"),
                typeIds = ids("document_type_id"), dateFrom = str("date_from"), dateTo = str("date_to"), untagged = bool("untagged") == true,
                inbox = bool("inbox"), status = str("status"), trash = bool("trash") == true, sort = str("sort"), semantic = str("mode") == "hybrid" || str("mode") == "semantic",
            )
        }
    }
}

/**
 * Quick date ranges for the filters, as label, from, to: this and last calendar year, and the
 * current and previous Indian financial year (April to March), which tax papers go by.
 */
fun datePresets(today: java.time.LocalDate): List<Triple<String, String, String>> {
    val y = today.year
    val fy = if (today.monthValue >= 4) y else y - 1
    fun financialYear(start: Int) = Triple("FY $start–${((start + 1) % 100).toString().padStart(2, '0')}", "$start-04-01", "${start + 1}-03-31")
    return listOf(Triple("This year", "$y-01-01", "$y-12-31"), Triple("Last year", "${y - 1}-01-01", "${y - 1}-12-31"), financialYear(fy), financialYear(fy - 1))
}

/** How much the phone keeps of documents opened for viewing, and of files handed to other apps. */
const val VIEW_CACHE_BYTES = 400L * 1024 * 1024
const val SHARE_CACHE_BYTES = 150L * 1024 * 1024

/**
 * Keeps a folder of downloaded copies under [maxBytes]: the files used longest ago go first,
 * never [keep] (the one just fetched) or a download in progress. Without it every document
 * ever opened stayed on the phone until Android cleared the cache itself.
 */
fun trimCache(dir: File, maxBytes: Long, keep: File? = null) {
    val files = dir.walkTopDown().filter { it.isFile && it != keep && !it.name.endsWith(".part") }.toList()
    var total = files.sumOf { it.length() } + (keep?.length() ?: 0L)
    for (f in files.sortedBy { it.lastModified() }) {
        if (total <= maxBytes) break
        val size = f.length()
        if (f.delete()) total -= size
    }
    dir.listFiles { d -> d.isDirectory && d.list()?.isEmpty() == true }?.forEach { it.delete() }
}

sealed interface SignIn {
    data object Done : SignIn
    data class NeedsCode(val challenge: String) : SignIn
}

sealed interface AskEvent {
    /** "searching" while documents are looked up, then "answering" while the model writes. */
    /** [conversationId] comes with the first one: an answer stopped part-way is kept under it. */
    data class Status(val stage: String, val conversationId: String = "") : AskEvent
    data class Citations(val list: List<Citation>) : AskEvent
    data class Delta(val text: String) : AskEvent
    data class Done(val conversationId: String) : AskEvent
    /** [gone]: the conversation no longer exists (deleted elsewhere). */
    data class Failed(val message: String, val gone: Boolean = false) : AskEvent
}

private class MemoryCookies : CookieJar {
    private val jar = HashMap<String, List<Cookie>>()
    override fun saveFromResponse(url: HttpUrl, cookies: List<Cookie>) {
        val old = jar[url.host].orEmpty().filter { o -> cookies.none { it.name == o.name } }
        jar[url.host] = old + cookies
    }

    override fun loadForRequest(url: HttpUrl) = jar[url.host].orEmpty().filter { it.matches(url) }
}

/** Everything the screens do against the server. */
class Repository(val api: ApiClient, val session: SessionStore, val cacheDir: File, private val deviceName: String = "Android phone") {

    /* ------------------------------------------------------------ sign-in */

    private var loginHttp: OkHttpClient = newLoginClient()
    private var loginBase: String = ""

    private fun newLoginClient(): OkHttpClient = ApiClient.baseHttp().newBuilder().cookieJar(MemoryCookies()).addInterceptor { chain ->
        val base = loginBase
        val b = chain.request().newBuilder().header("User-Agent", "DocvetaAndroid/0.1")
        if (base.isNotEmpty()) b.header("Origin", ApiClient.origin(base))
        chain.proceed(b.build())
    }.build()

    /** Checks that [base] is a Docveta server and tells what sign-in it offers. */
    suspend fun checkServer(base: String): ServerStatus {
        val req = Request.Builder().url("$base/api/v1/status").header("Accept", "application/json").build()
        return try {
            api.execute(req, ApiClient.baseHttp()) { AppJson.decodeFromString<ServerStatus>(it.body?.string().orEmpty()) }
        } catch (e: ApiException) {
            if (e.isNetwork) throw e
            throw ApiException(e.status, "not_docveta", "That address answered, but it doesn't look like Docveta.")
        } catch (e: Exception) {
            throw ApiException(0, "not_docveta", "That address answered, but it doesn't look like Docveta.")
        }
    }

    suspend fun signIn(base: String, email: String, password: String): SignIn {
        loginBase = base
        loginHttp = newLoginClient()
        val body = buildJsonObject { put("email", email.trim()); put("password", password) }.toString()
        val reply = loginPost("$base/api/v1/auth/login", body)
        val r = AppJson.decodeFromString<LoginReply>(reply)
        if (r.twoFactorRequired && r.challenge != null) return SignIn.NeedsCode(r.challenge)
        finishSignIn(base)
        return SignIn.Done
    }

    suspend fun signInWithCode(base: String, challenge: String, code: String) {
        loginBase = base
        val body = buildJsonObject { put("challenge", challenge); put("code", code.replace(" ", "")) }.toString()
        loginPost("$base/api/v1/auth/login/2fa", body)
        finishSignIn(base)
    }

    /** For servers that only offer single sign-on: a personal access token made in the web app (Settings, API tokens). */
    suspend fun signInWithToken(base: String, token: String) {
        session.serverUrl = base
        session.token = token.trim()
        try {
            val me = me()
            session.userName = me.displayName
            session.userEmail = me.email
        } catch (e: Exception) {
            session.signOut()
            throw e
        }
    }

    /**
     * Single sign-on happens in the browser. Returns the address to open; the server sends the
     * browser back to docveta://sso?code=…, which [signInWithSso] redeems. The code is useless
     * without the secret kept here (PKCE), so another app catching the link gains nothing.
     */
    fun ssoStartUrl(base: String): String {
        val bytes = ByteArray(32).also { java.security.SecureRandom().nextBytes(it) }
        val verifier = java.util.Base64.getUrlEncoder().withoutPadding().encodeToString(bytes)
        session.pendingSsoServer = base
        session.pendingSsoVerifier = verifier
        return "$base/api/v1/auth/oidc/start?app=1&code_challenge=${pkceChallenge(verifier)}"
    }

    suspend fun signInWithSso(code: String) {
        val base = session.pendingSsoServer ?: throw ApiException(0, "sso_expired", "Start single sign-on again from the app.")
        val verifier = session.pendingSsoVerifier ?: throw ApiException(0, "sso_expired", "Start single sign-on again from the app.")
        session.pendingSsoServer = null
        session.pendingSsoVerifier = null
        loginBase = base
        loginHttp = newLoginClient()
        val body = buildJsonObject { put("code", code); put("code_verifier", verifier) }.toString()
        loginPost("$base/api/v1/auth/oidc/app", body)
        finishSignIn(base)
    }

    private suspend fun loginPost(url: String, body: String): String {
        val req = Request.Builder().url(url).header("Accept", "application/json").post(body.toRequestBody(ApiClient.JSON)).build()
        return api.execute(req, loginHttp) { it.body?.string().orEmpty() }
    }

    private suspend fun loginGet(url: String): String {
        val req = Request.Builder().url(url).header("Accept", "application/json").build()
        return api.execute(req, loginHttp) { it.body?.string().orEmpty() }
    }

    /** First start of a new server: creates the administrator (and a shared space), then signs in. */
    suspend fun setup(base: String, name: String, email: String, password: String, sharedSpace: String) {
        loginBase = base
        loginHttp = newLoginClient()
        val body = buildJsonObject {
            put("display_name", name.trim()); put("email", email.trim()); put("password", password)
            if (sharedSpace.isNotBlank()) put("shared_space_name", sharedSpace.trim())
        }.toString()
        loginPost("$base/api/v1/setup", body)
        finishSignIn(base)
    }

    /** What an invitation link (…/invite/<token>) is for, before accepting it. */
    suspend fun invitePreview(base: String, token: String): InvitePreview {
        loginBase = base
        loginHttp = newLoginClient()
        return AppJson.decodeFromString(loginGet("$base/api/v1/invites/$token"))
    }

    suspend fun acceptInvite(base: String, token: String, name: String, email: String?, password: String) {
        loginBase = base
        val body = buildJsonObject {
            put("display_name", name.trim()); put("password", password)
            if (!email.isNullOrBlank()) put("email", email.trim())
        }.toString()
        loginPost("$base/api/v1/invites/$token/accept", body)
        finishSignIn(base)
    }

    /** Swaps the temporary web session for an access token that lasts, so the phone stays signed in. */
    private suspend fun finishSignIn(base: String) {
        // Administrators get the admin permission too, so Administration works in the app.
        val admin = runCatching { AppJson.decodeFromString<Me>(loginGet("$base/api/v1/me")).isAdmin }.getOrDefault(false)
        val created = AppJson.decodeFromString<NewToken>(loginPost("$base/api/v1/me/tokens", tokenRequest(admin)))
        session.serverUrl = base
        session.token = created.secret
        session.tokenId = created.token?.id
        session.tokenScopes = created.token?.scopes?.joinToString(",") ?: appScopes(admin).joinToString(",")
        runCatching { loginPost("$base/api/v1/auth/logout", "{}") } // the web session isn't needed any more
        loginHttp = newLoginClient()
        val me = me()
        session.userName = me.displayName
        session.userEmail = me.email
    }

    private fun appScopes(admin: Boolean) = listOf("documents:read", "documents:write", "upload") + if (admin) listOf("admin") else emptyList()

    private fun tokenRequest(admin: Boolean) = buildJsonObject {
        put("name", "Docveta Android ($deviceName)")
        putJsonArray("scopes") { appScopes(admin).forEach { add(JsonPrimitive(it)) } }
        put("expires_in_days", 365)
    }.toString()

    /** False when this phone signed in before the app asked for the admin permission. Unknown (older sign-ins) counts as missing. */
    val hasAdminScope: Boolean get() = session.tokenScopes?.split(',')?.contains("admin") == true

    suspend fun signOut() {
        session.tokenId?.let { id -> runCatching { api.delete("/me/tokens/$id") } }
        session.signOut()
        cacheDir.resolve("docs").deleteRecursively()
        cacheDir.resolve("shared").deleteRecursively()
    }

    /* ------------------------------------------------------------ confirming it's you */

    /**
     * Some account changes (password, two-step sign-in, new tokens) are only allowed from a fresh
     * sign-in, never with the phone's stored token. This signs in with the password (and code),
     * gives [block] that short-lived session, then signs it out again.
     * Throws [ApiException] with code "two_factor_required" when a code is needed and none was given.
     */
    suspend fun <T> withPassword(password: String, code: String?, block: suspend (WebSession) -> T): T {
        val s = openWebSession(password, code)
        try {
            return block(s)
        } finally {
            s.close()
        }
    }

    suspend fun openWebSession(password: String, code: String?): WebSession {
        val base = session.serverUrl ?: throw ApiException(401, "signed_out", "Please sign in again")
        val email = session.userEmail ?: me().email
        val s = WebSession(base, api, deviceName)
        val r = AppJson.decodeFromString<LoginReply>(s.postRaw("/auth/login", buildJsonObject { put("email", email); put("password", password) }.toString()))
        if (r.twoFactorRequired && r.challenge != null) {
            if (code.isNullOrBlank()) throw ApiException(428, "two_factor_required", "Enter the code from your authenticator app")
            s.postRaw("/auth/login/2fa", buildJsonObject { put("challenge", r.challenge); put("code", code.replace(" ", "")) }.toString())
        }
        return s
    }

    /** Replaces this phone's token with one that includes the admin permission. */
    suspend fun enableAdminOnPhone(password: String, code: String?) {
        withPassword(password, code) { s ->
            val created = AppJson.decodeFromString<NewToken>(s.postRaw("/me/tokens", tokenRequest(admin = true)))
            val old = session.tokenId
            session.token = created.secret
            session.tokenId = created.token?.id
            session.tokenScopes = created.token?.scopes?.joinToString(",") ?: appScopes(true).joinToString(",")
            if (old != null) runCatching { s.send("DELETE", "/me/tokens/$old") }
        }
    }

    /* ------------------------------------------------------------ reading */

    suspend fun me(): Me = api.get<Me>("/me").also { m -> runCatching { session.cachedMe = AppJson.encodeToString(Me.serializer(), m) } }

    /** The account from the last time the server answered, if any. */
    fun cachedMe(): Me? = session.cachedMe?.let { runCatching { AppJson.decodeFromString(Me.serializer(), it) }.getOrNull() }
    suspend fun stats(): Stats = api.get("/documents/stats")
    suspend fun aiStatus(): AiStatus = runCatching { api.get<AiStatus>("/ai/status") }.getOrDefault(AiStatus())

    suspend fun documents(q: DocQuery, cursor: String? = null, limit: Int = 40, facets: Boolean = false): DocumentList {
        val multi = buildList {
            q.tagIds.forEach { add("tag_id" to it) }
            q.correspondentIds.forEach { add("correspondent_id" to it) }
            q.typeIds.forEach { add("document_type_id" to it) }
        }
        val query = mapOf(
            "q" to q.q.trim(),
            "space_id" to q.spaceId,
            "date_from" to q.dateFrom,
            "date_to" to q.dateTo,
            "untagged" to if (q.untagged) "true" else null,
            "inbox" to q.inbox?.toString(),
            "status" to q.status,
            "trash" to if (q.trash) "true" else null,
            "sort" to q.sort,
            "mode" to if (q.semantic && q.q.isNotBlank()) "hybrid" else null,
            "cursor" to cursor,
            "limit" to limit.toString(),
            "facets" to if (facets) "true" else null,
        )
        return api.get("/documents", query, multi)
    }

    suspend fun document(id: String): Document = api.get("/documents/$id")

    suspend fun notes(id: String): List<Note> = api.get<Items<Note>>("/documents/$id/notes").items
    suspend fun addNote(id: String, body: String): Note = api.post("/documents/$id/notes", buildJsonObject { put("body", body) }.toString())
    suspend fun deleteNote(id: String, noteId: String) = api.delete("/documents/$id/notes/$noteId")

    suspend fun suggestions(id: String): List<AiSuggestion> = api.get<Items<AiSuggestion>>("/documents/$id/suggestions").items
    suspend fun resolveSuggestions(id: String, accept: Boolean, ids: List<String>? = null) {
        val body = buildJsonObject { if (ids != null) put("ids", JsonArray(ids.map { JsonPrimitive(it) })) }.toString()
        api.send("POST", "/documents/$id/suggestions/${if (accept) "accept" else "reject"}", body = body)
    }

    suspend fun versions(id: String): List<VersionInfo> = api.get<Items<VersionInfo>>("/documents/$id/versions").items

    suspend fun taxonomy(kind: String, spaceId: String?): List<Taxonomy> = api.get<Items<Taxonomy>>("/$kind", mapOf("space_id" to spaceId)).items
    suspend fun createTaxonomy(kind: String, spaceId: String, name: String): Taxonomy =
        api.post("/$kind", buildJsonObject { put("space_id", spaceId); put("name", name) }.toString())

    suspend fun notifications(unreadOnly: Boolean = false): NotificationList =
        api.get("/notifications", mapOf("limit" to "100", "unread" to if (unreadOnly) "true" else null))

    suspend fun markNotificationsRead(ids: List<String>? = null) {
        val body = buildJsonObject {
            if (ids == null) put("all", true) else put("ids", JsonArray(ids.map { JsonPrimitive(it) }))
        }.toString()
        api.send("POST", "/notifications/read", body = body)
    }

    /* ------------------------------------------------------------ changing */

    /** Applies a partial update. [version] makes the server refuse it if someone else changed the document meanwhile. */
    suspend fun update(id: String, patch: JsonObject, version: Int? = null): Document =
        api.patch("/documents/$id", patch.toString(), if (version != null) mapOf("If-Match" to "\"$version\"") else emptyMap())

    suspend fun setTitle(id: String, title: String, version: Int?) = update(id, buildJsonObject { put("title", title) }, version)
    suspend fun setDate(id: String, iso: String?, version: Int?) = update(id, buildJsonObject { put("document_date", if (iso == null) JsonNull else JsonPrimitive(iso)) }, version)
    suspend fun setCorrespondent(id: String, corrId: String?, version: Int?) = update(id, buildJsonObject { put("correspondent_id", if (corrId == null) JsonNull else JsonPrimitive(corrId)) }, version)
    suspend fun setType(id: String, typeId: String?, version: Int?) = update(id, buildJsonObject { put("document_type_id", if (typeId == null) JsonNull else JsonPrimitive(typeId)) }, version)
    suspend fun setTags(id: String, tagIds: List<String>, version: Int?) = update(id, buildJsonObject { put("tag_ids", JsonArray(tagIds.map { JsonPrimitive(it) })) }, version)
    suspend fun markReviewed(id: String, reviewed: Boolean = true) = update(id, buildJsonObject { put("inbox", !reviewed) })

    suspend fun trash(id: String) = api.delete("/documents/$id")
    suspend fun restore(id: String) {
        api.send("POST", "/documents/$id/restore")
    }
    suspend fun deleteForever(id: String) = api.delete("/documents/$id", mapOf("permanent" to "true"))
    suspend fun emptyTrash() = api.delete("/trash")

    suspend fun reprocess(id: String) {
        api.send("POST", "/documents/$id/reprocess")
    }

    suspend fun unlock(id: String, password: String): Document =
        api.post("/documents/$id/unlock", buildJsonObject { put("password", password) }.toString())

    suspend fun shares(docId: String): List<Share> = api.get<Items<Share>>("/shares", mapOf("document_id" to docId)).items
    suspend fun createShare(docId: String, days: Int?, password: String?, allowDownload: Boolean): ShareCreated {
        val body = buildJsonObject {
            put("document_id", docId)
            if (days != null) put("expires_in_days", days)
            if (!password.isNullOrBlank()) put("password", password)
            put("allow_download", allowDownload)
        }.toString()
        return api.post("/shares", body)
    }

    suspend fun revokeShare(id: String) = api.delete("/shares/$id")

    /* ------------------------------------------------------------ files */

    fun thumbnailUrl(d: Document): String = api.url("/documents/${d.id}/thumbnail", mapOf("v" to fileSignature(d))).toString()

    /**
     * Changes when the file itself changes (new version, pages edited, searchable PDF made), but
     * not when only the title or tags do: the document's version counts those too, and a big PDF
     * shouldn't be downloaded again because a tag was added.
     */
    fun fileSignature(d: Document): String = "${d.sizeBytes}-${d.pageCount ?: 0}-${if (d.hasArchive) 1 else 0}${if (d.hasDerived) 1 else 0}${if (d.hasThumbnail) 1 else 0}"

    /** The cache file [viewableFile] uses for this document as it is now. */
    fun viewableName(d: Document): String {
        val kind = when {
            d.hasDerived -> "derived"
            d.hasArchive -> "archive"
            else -> "original"
        }
        val ext = when {
            d.hasDerived || d.hasArchive || d.isPdf -> if (d.hasDerived && d.isImage) "jpg" else "pdf"
            d.isImage -> d.mimeType.substringAfter('/').substringBefore('+').ifBlank { "jpg" }
            else -> d.originalFilename.substringAfterLast('.', "bin")
        }
        return "${d.id}-${fileSignature(d)}-$kind.$ext"
    }

    /** The best viewable copy of a document in the cache (downloaded once per file change). */
    suspend fun viewableFile(d: Document, onProgress: (Float) -> Unit = {}): File {
        val name = viewableName(d)
        val kind = name.substringAfterLast('-').substringBefore('.')
        val f = File(cacheDir, "docs/$name")
        if (!f.exists() || f.length() == 0L) {
            api.download("/documents/${d.id}/file", mapOf("kind" to kind), f, onProgress)
            // Older copies of this document aren't needed any more.
            f.parentFile?.listFiles { x -> x.name.startsWith("${d.id}-") && x.name != name }?.forEach { it.delete() }
            trimCache(File(cacheDir, "docs"), VIEW_CACHE_BYTES, keep = f)
        } else f.setLastModified(System.currentTimeMillis()) // opened again: the last to go when space is made
        return f
    }

    /** The original file under its own name, ready to open in another app or share. */
    suspend fun originalFile(d: Document, onProgress: (Float) -> Unit = {}): File {
        val name = d.originalFilename.ifBlank { d.title }.replace(Regex("[\\\\/:*?\"<>|]"), "_")
        val f = File(cacheDir, "shared/${d.id}-v${d.version}/$name")
        if (!f.exists() || f.length() == 0L) {
            api.download("/documents/${d.id}/file", mapOf("kind" to "original", "download" to "1"), f, onProgress)
            trimCache(File(cacheDir, "shared"), SHARE_CACHE_BYTES, keep = f)
        }
        return f
    }

    /* ------------------------------------------------------------ Ask */

    /** A page of conversations, newest first; [before] is the last one's updated_at, [q] searches titles. */
    suspend fun conversations(q: String = "", before: String? = null, limit: Int = 40): List<Conversation> =
        api.get<Items<Conversation>>("/ai/conversations", mapOf("q" to q.ifBlank { null }, "before" to before, "limit" to limit.toString())).items
    suspend fun deleteAllConversations() = api.delete("/ai/conversations")
    suspend fun conversation(id: String): List<ConversationMessage> = api.get<Items<ConversationMessage>>("/ai/conversations/$id/messages").items
    suspend fun deleteConversation(id: String) = api.delete("/ai/conversations/$id")

    suspend fun renameConversation(id: String, title: String) {
        api.patch<Conversation>("/ai/conversations/$id", buildJsonObject { put("title", title) }.toString())
    }

    fun ask(question: String, conversationId: String?, spaceId: String?, documentId: String? = null, regenerate: Boolean = false): Flow<AskEvent> = callbackFlow {
        val body = buildJsonObject {
            put("question", question)
            if (regenerate) put("regenerate", true)
            if (conversationId != null) put("conversation_id", conversationId)
            if (spaceId != null) putJsonArray("space_ids") { add(JsonPrimitive(spaceId)) }
            if (documentId != null) putJsonArray("document_ids") { add(JsonPrimitive(documentId)) }
        }.toString()
        val req = Request.Builder().url(api.url("/ai/ask")).header("Accept", "text/event-stream").post(body.toRequestBody(ApiClient.JSON)).build()
        // The server sends a keep-alive every 15 s, so two minutes of silence means the connection is gone.
        val client = api.imageClient.newBuilder().readTimeout(120, java.util.concurrent.TimeUnit.SECONDS).build()
        val call = client.newCall(req)
        // Read in a child coroutine so that cancelling the flow (Stop) reaches awaitClose and
        // cancels the HTTP call, which unblocks the read.
        launch {
            try {
                val res = call.await()
                res.use {
                    if (!it.isSuccessful) throw ApiClient.problem(it)
                    val src = it.body!!.source()
                    var event = ""
                    while (!src.exhausted()) {
                        val line = src.readUtf8Line() ?: break
                        when {
                            line.startsWith("event:") -> event = line.removePrefix("event:").trim()
                            line.startsWith("data:") -> {
                                val data = line.removePrefix("data:").trim()
                                val obj = { AppJson.parseToJsonElement(data) as? JsonObject }
                                when (event) {
                                    "status" -> trySend(AskEvent.Status((obj()?.get("stage") as? JsonPrimitive)?.content.orEmpty(), (obj()?.get("conversation_id") as? JsonPrimitive)?.content.orEmpty()))
                                    "citations" -> trySend(AskEvent.Citations(AppJson.decodeFromString(ListSerializer(Citation.serializer()), data)))
                                    "delta" -> trySend(AskEvent.Delta(AppJson.parseToJsonElement(data).let { e -> (e as? JsonPrimitive)?.content.orEmpty() }))
                                    "error" -> trySend(AskEvent.Failed((obj()?.get("message") as? JsonPrimitive)?.content ?: "Something went wrong"))
                                    "done" -> trySend(AskEvent.Done((obj()?.get("conversation_id") as? JsonPrimitive)?.content.orEmpty()))
                                }
                            }
                        }
                    }
                }
            } catch (e: Exception) {
                if (e !is kotlinx.coroutines.CancellationException && !isClosedForSend) {
                    trySend(AskEvent.Failed((e as? ApiException)?.message ?: "The connection to the server was lost. Try again.", gone = (e as? ApiException)?.status == 404))
                }
            } finally {
                close()
            }
        }
        awaitClose { call.cancel() }
    }.flowOn(Dispatchers.IO)
}


/** A short-lived browser-style session (cookie), for the few account changes a token may not make. */
class WebSession(private val base: String, private val api: ApiClient, deviceName: String) {
    private val agent = "DocvetaAndroid/0.1 ($deviceName)"
    private val http: OkHttpClient = ApiClient.baseHttp().newBuilder().cookieJar(MemoryCookies()).addInterceptor { chain ->
        chain.proceed(chain.request().newBuilder().header("User-Agent", agent).header("Origin", ApiClient.origin(base)).build())
    }.build()

    suspend fun send(method: String, path: String, body: String? = null): String {
        val rb = body?.toRequestBody(ApiClient.JSON) ?: if (method == "POST" || method == "PUT" || method == "PATCH") "{}".toRequestBody(ApiClient.JSON) else null
        val req = Request.Builder().url("$base/api/v1$path").header("Accept", "application/json").method(method, rb).build()
        return api.execute(req, http) { it.body?.string().orEmpty() }
    }

    suspend fun postRaw(path: String, body: String = "{}"): String = send("POST", path, body)

    suspend inline fun <reified T> post(path: String, body: String = "{}"): T = AppJson.decodeFromString(postRaw(path, body))

    suspend fun close() {
        runCatching { postRaw("/auth/logout") }
    }
}

/** PKCE S256: base64url(SHA-256(verifier)) without padding. */
fun pkceChallenge(verifier: String): String {
    val digest = java.security.MessageDigest.getInstance("SHA-256").digest(verifier.toByteArray(Charsets.US_ASCII))
    return java.util.Base64.getUrlEncoder().withoutPadding().encodeToString(digest)
}
