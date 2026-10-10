package app.docveta.android.data

import java.io.File
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put

// Everything beyond reading and filing documents: document tools, spaces, the account and
// administration. Same endpoints and bodies as the web app.

private fun strings(l: List<String>) = JsonArray(l.map { JsonPrimitive(it) })
private fun nullable(s: String?): JsonElement = if (s == null) JsonNull else JsonPrimitive(s)

/* ------------------------------------------------------------ document tools */

suspend fun Repository.history(id: String): List<HistoryEntry> = api.get<Items<HistoryEntry>>("/documents/$id/history").items
suspend fun Repository.pages(id: String): List<PageText> = api.get<Items<PageText>>("/documents/$id/pages").items
suspend fun Repository.similar(id: String): List<SimilarDoc> = api.get<Items<SimilarDoc>>("/documents/$id/similar").items
suspend fun Repository.customFields(spaceId: String?): List<CustomField> = api.get<Items<CustomField>>("/custom-fields", mapOf("space_id" to spaceId)).items

suspend fun Repository.setCustomField(id: String, fieldId: String, value: JsonElement, version: Int?) =
    update(id, buildJsonObject { put("custom_fields", buildJsonObject { put(fieldId, value) }) }, version)

suspend fun Repository.setSpace(id: String, spaceId: String, version: Int?) = update(id, buildJsonObject { put("space_id", spaceId) }, version)
suspend fun Repository.setLanguage(id: String, lang: String, version: Int?) = update(id, buildJsonObject { put("language", lang) }, version)
suspend fun Repository.setLocation(id: String, where: String, version: Int?) = update(id, buildJsonObject { put("physical_location", where) }, version)
suspend fun Repository.assignAsn(id: String): Document = api.post("/documents/$id/asn")

suspend fun Repository.reprocess(id: String, forceOcr: Boolean) {
    api.send("POST", "/documents/$id/reprocess", query = mapOf("force_ocr" to if (forceOcr) "true" else null))
}

/** Asks the AI for tag, sender and type suggestions again. */
suspend fun Repository.runAi(id: String) {
    api.send("POST", "/documents/$id/ai")
}

/** [pages]: original page numbers (1-based) in the new order, each with extra clockwise turn. */
suspend fun Repository.editPages(id: String, pages: List<Pair<Int, Int>>): Document {
    val body = buildJsonObject { put("pages", JsonArray(pages.map { (from, rot) -> buildJsonObject { put("from", from); put("rotate", rot) } })) }
    return api.post("/documents/$id/pages/edit", body.toString())
}

/** Copies pages (like "1,3-4") into new documents. */
suspend fun Repository.split(id: String, ranges: List<String>, trashOriginal: Boolean = false): List<Document> {
    val body = buildJsonObject { put("ranges", strings(ranges)); put("trash_original", trashOriginal) }
    return api.post<Items<Document>>("/documents/$id/split", body.toString()).items
}

suspend fun Repository.merge(ids: List<String>, title: String, trashOriginals: Boolean): Document {
    val body = buildJsonObject { put("ids", strings(ids)); put("title", title); put("trash_originals", trashOriginals) }
    return api.post("/documents/merge", body.toString())
}

suspend fun Repository.uploadVersion(id: String, file: File, mime: String, name: String): Document =
    AppJson.decodeFromString(api.sendFile("/documents/$id/versions", file, mime, name, mapOf("note" to "Replaced file")))

suspend fun Repository.restoreVersion(id: String, no: Int): Document = api.post("/documents/$id/versions/$no/restore")

/** A file of this document (a version, or the searchable PDF), saved in the cache to open or share. */
/** Several documents as one ZIP file in the cache, ready to share or save. The server takes up to 500. */
suspend fun Repository.zip(ids: List<String>): File {
    val f = File(cacheDir, "shared/zip-${System.currentTimeMillis()}/docveta-documents-${java.time.LocalDate.now()}.zip")
    api.download("/documents/archive", emptyMap(), f, body = buildJsonObject { put("ids", JsonArray(ids.map { JsonPrimitive(it) })) }.toString())
    trimCache(File(cacheDir, "shared"), SHARE_CACHE_BYTES, keep = f)
    return f
}

suspend fun Repository.downloadFile(d: Document, kind: String, version: Int? = null, onProgress: (Float) -> Unit = {}): File {
    val base = d.originalFilename.ifBlank { d.title }.replace(Regex("[\\\\/:*?\"<>|]"), "_")
    val name = when {
        kind == "archive" -> base.substringBeforeLast('.') + " (searchable).pdf"
        version != null -> base.substringBeforeLast('.') + " (version $version)" + base.substringAfterLast('.', "").let { if (it.isEmpty()) "" else ".$it" }
        else -> base
    }
    val f = File(cacheDir, "shared/${d.id}-v${d.version}-$kind-${version ?: 0}/$name")
    val q = mapOf("kind" to kind, "download" to "1", "version" to version?.toString())
    if (!f.exists() || f.length() == 0L) api.download("/documents/${d.id}/file", q, f, onProgress)
    return f
}

/** Runs one action on many documents: update | trash | restore | purge | reprocess. [select] = "inbox" picks the whole Inbox on the server. */
suspend fun Repository.bulk(ids: List<String>, action: String, update: JsonObject = JsonObject(emptyMap()), select: String? = null): BulkResult {
    val body = buildJsonObject {
        put("ids", strings(ids))
        if (select != null) put("select", select)
        put("action", action)
        put("update", update)
    }
    return api.post("/documents/bulk", body.toString())
}

suspend fun Repository.directory(): List<DirectoryEntry> = api.get<Items<DirectoryEntry>>("/users/directory").items

/* ------------------------------------------------------------ saved views */

suspend fun Repository.savedViews(): List<SavedView> = api.get<Items<SavedView>>("/saved-views").items.sortedWith(compareBy({ it.sortOrder }, { it.name.lowercase() }))

suspend fun Repository.createView(name: String, query: DocQuery, shareInSpace: String?): SavedView {
    val body = buildJsonObject { put("name", name); put("query", query.toJson()); put("space_id", nullable(shareInSpace)); put("pinned", true) }
    return api.post("/saved-views", body.toString())
}

suspend fun Repository.updateView(id: String, patch: JsonObject): SavedView = api.patch("/saved-views/$id", patch.toString())
suspend fun Repository.deleteView(id: String) = api.delete("/saved-views/$id")

/* ------------------------------------------------------------ spaces */

suspend fun Repository.createSpace(name: String, description: String, color: String): Space =
    api.post("/spaces", buildJsonObject { put("name", name); put("description", description); put("color", color) }.toString())

suspend fun Repository.updateSpace(id: String, patch: JsonObject): Space = api.patch("/spaces/$id", patch.toString())
suspend fun Repository.deleteSpace(id: String) = api.delete("/spaces/$id")
suspend fun Repository.members(spaceId: String): List<Member> = api.get<Items<Member>>("/spaces/$spaceId/members").items

suspend fun Repository.setMember(spaceId: String, userId: String, role: String) {
    api.send("PUT", "/spaces/$spaceId/members/$userId", body = buildJsonObject { put("role", role) }.toString())
}

suspend fun Repository.removeMember(spaceId: String, userId: String) = api.delete("/spaces/$spaceId/members/$userId")
suspend fun Repository.aiStats(spaceId: String): AiStats = api.get("/spaces/$spaceId/ai-stats")

/** Tags, correspondents and document types with their automatic-matching rule. */
suspend fun Repository.saveTaxonomy(kind: String, spaceId: String, existing: Taxonomy?, name: String, color: String?, algorithm: String, pattern: String, caseSensitive: Boolean): Taxonomy {
    val body = buildJsonObject {
        put("name", name.trim())
        if (kind == "tags" && color != null) put("color", color)
        put("match_algorithm", algorithm)
        put("match_pattern", pattern)
        put("case_sensitive", caseSensitive)
        if (existing == null) put("space_id", spaceId)
    }.toString()
    return if (existing == null) api.post("/$kind", body) else api.patch("/$kind/${existing.id}", body)
}

suspend fun Repository.deleteTaxonomy(kind: String, id: String) = api.delete("/$kind/$id")

suspend fun Repository.mergeTaxonomy(kind: String, target: String, sources: List<String>): Taxonomy =
    api.post("/$kind/$target/merge", buildJsonObject { put("source_ids", strings(sources)) }.toString())

suspend fun Repository.saveField(existing: CustomField?, spaceId: String, name: String, type: String, choices: List<String>, currency: String?): CustomField {
    val options = buildJsonObject {
        if (type == "select" || type == "multiselect") put("choices", strings(choices))
        if (type == "monetary" && currency != null) put("currency", currency.trim().uppercase())
    }
    return if (existing == null) api.post("/custom-fields", buildJsonObject { put("space_id", spaceId); put("name", name.trim()); put("data_type", type); put("options", options) }.toString())
    else api.patch("/custom-fields/${existing.id}", buildJsonObject { put("name", name.trim()); put("options", options) }.toString())
}

suspend fun Repository.deleteField(id: String) = api.delete("/custom-fields/$id")

suspend fun Repository.workflows(spaceId: String): List<Workflow> = api.get<Items<Workflow>>("/workflows", mapOf("space_id" to spaceId)).items.filter { it.spaceId == spaceId }
suspend fun Repository.saveWorkflow(id: String?, body: JsonObject): Workflow = if (id == null) api.post("/workflows", body.toString()) else api.patch("/workflows/$id", body.toString())
suspend fun Repository.setWorkflowEnabled(id: String, on: Boolean): Workflow = api.patch("/workflows/$id", buildJsonObject { put("enabled", on) }.toString())
suspend fun Repository.deleteWorkflow(id: String) = api.delete("/workflows/$id")
suspend fun Repository.workflowRuns(id: String): List<WorkflowRun> = api.get<Items<WorkflowRun>>("/workflows/$id/runs").items

/** A printable sheet (separator, or an archive-number label) saved as a picture. */
suspend fun Repository.barcodeSheet(asn: Long?): File {
    val f = File(cacheDir, "shared/barcodes/" + (if (asn == null) "separator.png" else "asn-$asn.png"))
    if (!f.exists() || f.length() == 0L) api.download(if (asn == null) "/barcodes/separator.png" else "/barcodes/asn.png", mapOf("n" to asn?.toString()), f)
    return f
}

/* ------------------------------------------------------------ account */

suspend fun Repository.updateProfile(patch: JsonObject): Me = api.patch("/me", patch.toString())
suspend fun Repository.sessions(): List<WebSessionInfo> = api.get<Items<WebSessionInfo>>("/me/sessions").items
suspend fun Repository.revokeSession(id: String) = api.delete("/me/sessions/$id")
suspend fun Repository.tokens(): List<ApiToken> = api.get<Items<ApiToken>>("/me/tokens").items
suspend fun Repository.revokeToken(id: String) = api.delete("/me/tokens/$id")
suspend fun Repository.identities(): List<Identity> = api.get<Items<Identity>>("/me/identities").items
suspend fun Repository.unlinkIdentity(id: String) = api.delete("/me/identities/$id")
suspend fun Repository.twoFactor(): TwoFactorStatus = api.get("/me/2fa")
suspend fun Repository.notificationPrefs(): NotificationPrefs = api.get("/me/notification-prefs")

suspend fun Repository.saveNotificationPrefs(p: NotificationPrefs): NotificationPrefs =
    api.put("/me/notification-prefs", buildJsonObject { put("quiet_enabled", p.quietEnabled); put("quiet_start", p.quietStart); put("quiet_end", p.quietEnd) }.toString())

suspend fun Repository.channels(): ChannelList = api.get("/notification-channels")

suspend fun Repository.saveChannel(existing: Channel?, name: String, type: String, config: Map<String, String>, events: List<String>, system: Boolean): Channel {
    val cfg = buildJsonObject { config.forEach { (k, v) -> if (v.isNotBlank()) put(k, v.trim()) } }
    return if (existing == null) api.post("/notification-channels", buildJsonObject { put("name", name); put("type", type); put("config", cfg); put("events", strings(events)); put("system", system) }.toString())
    else api.patch("/notification-channels/${existing.id}", buildJsonObject { put("name", name); put("config", cfg); put("events", strings(events)) }.toString())
}

suspend fun Repository.setChannelEnabled(id: String, on: Boolean): Channel = api.patch("/notification-channels/$id", buildJsonObject { put("enabled", on) }.toString())
suspend fun Repository.deleteChannel(id: String) = api.delete("/notification-channels/$id")
suspend fun Repository.testChannel(id: String) { api.send("POST", "/notification-channels/$id/test") }

suspend fun Repository.changePassword(current: String, new: String, code: String?) =
    withPassword(current, code) { it.postRaw("/me/password", buildJsonObject { put("current_password", current); put("new_password", new) }.toString()) }

suspend fun Repository.createToken(password: String, code: String?, name: String, scopes: List<String>, days: Int?): String = withPassword(password, code) { s ->
    s.post<NewToken>("/me/tokens", buildJsonObject { put("name", name); put("scopes", strings(scopes)); put("expires_in_days", if (days == null) JsonNull else JsonPrimitive(days)) }.toString()).secret
}

suspend fun Repository.disableTwoFactor(password: String, code: String) = withPassword(password, code) { s ->
    s.postRaw("/me/2fa/disable", buildJsonObject { put("password", password); put("code", code.replace(" ", "")) }.toString())
}

suspend fun Repository.newRecoveryCodes(password: String, code: String): List<String> = withPassword(password, code) { s ->
    s.post<RecoveryCodes>("/me/2fa/recovery-codes", buildJsonObject { put("code", code.replace(" ", "")) }.toString()).codes
}

/* ------------------------------------------------------------ administration */

suspend fun Repository.adminUsers(): List<AdminUser> = api.get<Items<AdminUser>>("/admin/users").items

suspend fun Repository.createUser(email: String, name: String, password: String?, admin: Boolean): AdminUser =
    api.post("/admin/users", buildJsonObject { put("email", email.trim()); put("display_name", name.trim()); put("password", nullable(password?.ifBlank { null })); put("is_admin", admin) }.toString())

suspend fun Repository.updateUser(id: String, patch: JsonObject): AdminUser = api.patch("/admin/users/$id", patch.toString())
suspend fun Repository.deleteUser(id: String) = api.delete("/admin/users/$id")
suspend fun Repository.resetUserTwoFactor(id: String) = api.delete("/admin/users/$id/2fa")

suspend fun Repository.invites(): List<Invite> = api.get<Items<Invite>>("/admin/invites").items

suspend fun Repository.createInvite(name: String, email: String, admin: Boolean, note: String, days: Int, sendEmail: Boolean, spaces: Map<String, String>): InviteCreated {
    val body = buildJsonObject {
        put("display_name", name.trim())
        if (email.isNotBlank()) put("email", email.trim())
        put("is_admin", admin); put("note", note); put("expires_in_days", days); put("send_email", sendEmail && email.isNotBlank())
        put("spaces", JsonArray(spaces.map { (id, role) -> buildJsonObject { put("space_id", id); put("role", role) } }))
    }
    return api.post("/admin/invites", body.toString())
}

suspend fun Repository.revokeInvite(id: String) = api.delete("/admin/invites/$id")

suspend fun Repository.workers(): List<Worker> = api.get<Items<Worker>>("/admin/workers").items
suspend fun Repository.createWorker(name: String): String = api.post<WorkerToken>("/admin/workers", buildJsonObject { put("name", name) }.toString()).token
suspend fun Repository.setWorkerEnabled(id: String, on: Boolean) { api.send("PATCH", "/admin/workers/$id", body = buildJsonObject { put("enabled", on) }.toString()) }
suspend fun Repository.rotateWorkerToken(id: String): String = api.post<WorkerToken>("/admin/workers/$id/rotate-token").token
suspend fun Repository.deleteWorker(id: String) = api.delete("/admin/workers/$id")
suspend fun Repository.tasks(status: String?): TaskList = api.get("/admin/tasks", mapOf("status" to status, "limit" to "100"))
suspend fun Repository.retryTask(id: String) { api.send("POST", "/admin/tasks/$id/retry") }

suspend fun Repository.processingSettings(): ProcessingSettings = api.get("/admin/settings/processing")
suspend fun Repository.saveProcessingSettings(s: ProcessingSettings): ProcessingSettings = api.put("/admin/settings/processing", AppJson.encodeToString(ProcessingSettings.serializer(), s))

suspend fun Repository.oidcConfig(): OidcConfig = api.get("/admin/settings/oidc")

suspend fun Repository.saveOidc(c: OidcConfig, secret: String): OidcConfig {
    val o = AppJson.encodeToJsonElement(OidcConfig.serializer(), c) as JsonObject
    val body = JsonObject(o.filterKeys { it != "redirect_uri" && it != "has_client_secret" } + (if (secret.isNotBlank()) mapOf("client_secret" to JsonPrimitive(secret)) else emptyMap()))
    return api.put("/admin/settings/oidc", body.toString())
}

suspend fun Repository.smtpConfig(): SmtpConfig = api.get("/admin/settings/smtp")

suspend fun Repository.saveSmtp(c: SmtpConfig, password: String): SmtpConfig {
    val o = AppJson.encodeToJsonElement(SmtpConfig.serializer(), c) as JsonObject
    val body = JsonObject(o.filterKeys { it != "has_password" } + (if (password.isNotBlank()) mapOf("password" to JsonPrimitive(password)) else emptyMap()))
    return api.put("/admin/settings/smtp", body.toString())
}

suspend fun Repository.testSmtp() { api.send("POST", "/admin/settings/smtp/test") }
suspend fun Repository.serverSettings(): ServerSettings = api.get("/admin/settings/server")

suspend fun Repository.saveServerSettings(publicUrl: String, allowLocal: Boolean): ServerSettings =
    api.put("/admin/settings/server", buildJsonObject { put("public_url", publicUrl.trim()); put("allow_local_targets", allowLocal) }.toString())

suspend fun Repository.officeInfo(): OfficeInfo = api.get("/admin/settings/office")
suspend fun Repository.saveOffice(url: String) { api.send("PUT", "/admin/settings/office", body = buildJsonObject { put("url", url.trim()) }.toString()) }
suspend fun Repository.testOffice(url: String) { api.send("POST", "/admin/settings/office/test", body = buildJsonObject { if (url.isNotBlank()) put("url", url.trim()) }.toString()) }

suspend fun Repository.systemInfo(): SystemInfo = api.get("/admin/system")
suspend fun Repository.audit(action: String?): List<AuditEntry> = api.get<Items<AuditEntry>>("/admin/audit", mapOf("action" to action, "limit" to "200")).items

suspend fun Repository.aiProviders(): List<AiProvider> = api.get<Items<AiProvider>>("/admin/ai/providers").items

suspend fun Repository.saveProvider(id: String?, body: JsonObject): AiProvider = if (id == null) api.post("/admin/ai/providers", body.toString()) else api.patch("/admin/ai/providers/$id", body.toString())
suspend fun Repository.setProviderEnabled(id: String, on: Boolean): AiProvider = api.patch("/admin/ai/providers/$id", buildJsonObject { put("enabled", on) }.toString())
suspend fun Repository.deleteProvider(id: String) = api.delete("/admin/ai/providers/$id")
suspend fun Repository.testProvider(id: String): AiTestResult = api.post("/admin/ai/providers/$id/test")
suspend fun Repository.reindex(): Int = api.post<Queued>("/admin/ai/reindex").queued
suspend fun Repository.aiTuning(): AiTuning = api.get("/admin/ai/settings")
suspend fun Repository.saveAiTuning(body: JsonObject): AiTuning = api.put("/admin/ai/settings", body.toString())
suspend fun Repository.resetAiTuning(): AiTuning { api.delete("/admin/ai/settings"); return aiTuning() }

suspend fun Repository.folders(): FolderList = api.get("/admin/folders")
suspend fun Repository.saveFolder(id: String?, body: JsonObject): WatchedFolder = if (id == null) api.post("/admin/folders", body.toString()) else api.patch("/admin/folders/$id", body.toString())
suspend fun Repository.setFolderEnabled(id: String, on: Boolean): WatchedFolder = api.patch("/admin/folders/$id", buildJsonObject { put("enabled", on) }.toString())
suspend fun Repository.deleteFolder(id: String) = api.delete("/admin/folders/$id")
suspend fun Repository.scanFolder(id: String): ScanResult = api.post("/admin/folders/$id/scan")
