package app.docveta.android.upload

import android.content.Context
import android.net.Uri
import android.provider.OpenableColumns
import androidx.work.BackoffPolicy
import androidx.work.Constraints
import androidx.work.CoroutineWorker
import androidx.work.ExistingWorkPolicy
import androidx.work.NetworkType
import androidx.work.OneTimeWorkRequestBuilder
import androidx.work.WorkManager
import androidx.work.WorkerParameters
import app.docveta.android.DocvetaApp
import app.docveta.android.data.ApiClient
import app.docveta.android.data.ApiException
import app.docveta.android.data.AppJson
import app.docveta.android.data.SessionStore
import java.io.File
import java.util.UUID
import java.util.concurrent.TimeUnit
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.withContext
import kotlinx.serialization.Serializable
import kotlinx.serialization.builtins.ListSerializer

@Serializable
data class UploadItem(
    val id: String,
    val path: String,
    val name: String,
    val mime: String,
    val size: Long,
    val spaceId: String? = null,
    val title: String? = null,
    val source: String = "web",
    val status: String = "queued", // queued | uploading | done | duplicate | error
    val progress: Float = 0f,
    val tusUrl: String? = null,
    val docId: String? = null,
    val error: String? = null,
    val duplicateOfId: String? = null,
    val duplicateTitle: String? = null,
    val allowDuplicate: Boolean = false,
) {
    val active get() = status == "queued" || status == "uploading"
}

/**
 * The upload queue. Files are copied into the app's own storage first, so they survive the picker
 * going away, and the list is saved to disk so a restart (or a dead battery) loses nothing.
 */
class UploadManager(private val context: Context, private val api: ApiClient, private val session: SessionStore) {
    private val dir = File(context.filesDir, "uploads").apply { mkdirs() }
    private val stateFile = File(dir, "queue.json")
    private val _items = MutableStateFlow(load())
    val items: StateFlow<List<UploadItem>> = _items.asStateFlow()

    private fun load(): List<UploadItem> = runCatching {
        AppJson.decodeFromString(ListSerializer(UploadItem.serializer()), stateFile.readText())
            .map { if (it.status == "uploading") it.copy(status = "queued") else it } // interrupted: try again
            .filter { File(it.path).exists() || !it.active }
    }.getOrDefault(emptyList())

    private fun save() {
        runCatching { stateFile.writeText(AppJson.encodeToString(ListSerializer(UploadItem.serializer()), _items.value)) }
    }

    private fun change(id: String, f: (UploadItem) -> UploadItem) {
        _items.update { l -> l.map { if (it.id == id) f(it) else it } }
        save()
    }

    /** Copies content from a picker or share sheet into the queue. */
    suspend fun enqueue(uri: Uri, spaceId: String?, source: String, title: String? = null): UploadItem = withContext(Dispatchers.IO) {
        var name = "document"
        context.contentResolver.query(uri, null, null, null, null)?.use { c ->
            if (c.moveToFirst()) {
                val n = c.getColumnIndex(OpenableColumns.DISPLAY_NAME)
                if (n >= 0) name = c.getString(n) ?: name
            }
        }
        val mime = context.contentResolver.getType(uri) ?: "application/octet-stream"
        if (!name.contains('.')) name += "." + (android.webkit.MimeTypeMap.getSingleton().getExtensionFromMimeType(mime) ?: "bin")
        val id = UUID.randomUUID().toString()
        val dest = File(dir, "$id-${name.replace(Regex("[^A-Za-z0-9._-]"), "_")}")
        context.contentResolver.openInputStream(uri)?.use { i -> dest.outputStream().use { o -> i.copyTo(o) } } ?: throw ApiException(0, "read", "Couldn't read that file")
        add(dest, name, mime, spaceId, source, title, id)
    }

    /** Queues a file the app made itself (a scan). It is moved, not copied. */
    suspend fun enqueueFile(file: File, name: String, mime: String, spaceId: String?, source: String, title: String?): UploadItem = withContext(Dispatchers.IO) {
        val id = UUID.randomUUID().toString()
        val dest = File(dir, "$id-${name.replace(Regex("[^A-Za-z0-9._-]"), "_")}")
        if (!file.renameTo(dest)) {
            file.copyTo(dest, overwrite = true)
            file.delete()
        }
        add(dest, name, mime, spaceId, source, title, id)
    }

    private fun add(file: File, name: String, mime: String, spaceId: String?, source: String, title: String?, id: String): UploadItem {
        val item = UploadItem(id = id, path = file.path, name = name, mime = mime, size = file.length(), spaceId = spaceId, source = source, title = title)
        _items.update { it + item }
        save()
        schedule()
        return item
    }

    fun retry(id: String, allowDuplicate: Boolean = false) {
        change(id) { it.copy(status = "queued", error = null, allowDuplicate = allowDuplicate || it.allowDuplicate, progress = 0f, tusUrl = if (allowDuplicate) null else it.tusUrl) }
        schedule()
    }

    fun remove(id: String) {
        _items.value.firstOrNull { it.id == id }?.let { File(it.path).delete() }
        _items.update { l -> l.filterNot { it.id == id } }
        save()
    }

    fun clearFinished() {
        _items.value.filter { !it.active && it.status != "error" }.forEach { File(it.path).delete() }
        _items.update { l -> l.filter { it.active || it.status == "error" } }
        save()
    }

    fun schedule() {
        val net = if (session.wifiOnlyUploads) NetworkType.UNMETERED else NetworkType.CONNECTED
        val req = OneTimeWorkRequestBuilder<UploadWorker>()
            .setConstraints(Constraints.Builder().setRequiredNetworkType(net).build())
            .setBackoffCriteria(BackoffPolicy.EXPONENTIAL, 15, TimeUnit.SECONDS)
            .build()
        WorkManager.getInstance(context).enqueueUniqueWork("uploads", ExistingWorkPolicy.APPEND_OR_REPLACE, req)
    }

    /** Sends everything that's queued. Returns true when something should be tried again later (no network, say). */
    suspend fun process(): Boolean {
        val uploader = TusUploader(api)
        var retryLater = false
        while (true) {
            val item = _items.value.firstOrNull { it.status == "queued" } ?: break
            val file = File(item.path)
            if (!file.exists()) {
                change(item.id) { it.copy(status = "error", error = "The file is gone") }
                continue
            }
            change(item.id) { it.copy(status = "uploading", error = null) }
            try {
                val meta = buildMap {
                    put("filename", item.name)
                    put("filetype", item.mime)
                    item.spaceId?.let { put("space_id", it) }
                    item.title?.let { put("title", it) }
                    if (item.source == "share" || item.source == "scan") put("source", item.source)
                    if (item.allowDuplicate) put("allow_duplicate", "true")
                }
                var last = 0L
                val docId = uploader.upload(file, meta, item.tusUrl, onUrl = { u -> change(item.id) { it.copy(tusUrl = u) } }) { sent, total ->
                    val now = System.currentTimeMillis()
                    if (now - last > 250 || sent >= total) {
                        last = now
                        change(item.id) { it.copy(progress = if (total > 0) sent.toFloat() / total else 1f) }
                    }
                }
                file.delete()
                change(item.id) { it.copy(status = "done", progress = 1f, docId = docId, tusUrl = null) }
            } catch (e: ApiException) {
                when {
                    e.code == "duplicate_document" -> change(item.id) { it.copy(status = "duplicate", error = e.message, duplicateOfId = e.extra["document_id"], duplicateTitle = e.extra["title"], tusUrl = null) }
                    e.isNetwork -> {
                        change(item.id) { it.copy(status = "queued", error = "Waiting for a connection") }
                        retryLater = true
                        break
                    }
                    e.isUnauthorized -> {
                        change(item.id) { it.copy(status = "error", error = "Please sign in again") }
                        break
                    }
                    else -> change(item.id) { it.copy(status = "error", error = e.message, tusUrl = if (e.status in 500..599) it.tusUrl else null) }
                }
            }
        }
        return retryLater
    }
}

class UploadWorker(ctx: Context, params: WorkerParameters) : CoroutineWorker(ctx, params) {
    override suspend fun doWork(): Result {
        val app = applicationContext as DocvetaApp
        if (!app.container.session.signedIn) return Result.success()
        return if (app.container.uploads.process()) Result.retry() else Result.success()
    }
}
