package app.docveta.android.upload

import app.docveta.android.data.ApiClient
import app.docveta.android.data.ApiException
import java.io.File
import java.io.RandomAccessFile
import java.util.Base64
import kotlinx.coroutines.delay
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.Request
import okhttp3.RequestBody
import okhttp3.RequestBody.Companion.toRequestBody
import okio.BufferedSink

/**
 * Resumable uploads over tus 1.0.0, so a big scan survives a lift ride through a tunnel: the next
 * attempt asks the server how much arrived and sends the rest.
 */
class TusUploader(private val api: ApiClient, private val chunkSize: Int = 2 * 1024 * 1024) {
    private val version = "1.0.0"

    /**
     * Uploads [file] and returns the new document's id.
     * [uploadUrl] is the address of an upload begun earlier (null to start one); [onUrl] reports a new one so the caller can keep it.
     */
    suspend fun upload(
        file: File,
        meta: Map<String, String>,
        uploadUrl: String? = null,
        onUrl: (String) -> Unit = {},
        onProgress: (sent: Long, total: Long) -> Unit = { _, _ -> },
    ): String {
        val total = file.length()
        var url = uploadUrl
        var offset = 0L
        if (url != null) {
            val o = headOffset(url)
            if (o == null) url = null else offset = o
        }
        if (url == null) {
            url = create(total, meta)
            onUrl(url)
            offset = 0
        }
        onProgress(offset, total)
        var docId: String? = null
        var failures = 0
        while (offset < total || docId == null) {
            val len = minOf(chunkSize.toLong(), total - offset)
            try {
                val (newOffset, id) = patch(url, file, offset, len) { sent -> onProgress(offset + sent, total) }
                failures = 0
                offset = newOffset
                if (id != null) docId = id
                onProgress(offset, total)
                if (offset >= total && docId == null) throw ApiException(0, "no_document", "The upload finished but the server didn't confirm it")
            } catch (e: ApiException) {
                if (e.status == 409 && e.code == "offset_mismatch") { // we and the server disagree about the offset: ask, then carry on
                    offset = headOffset(url) ?: throw ApiException(404, "gone", "The upload expired. Try again.")
                    continue
                }
                if (!e.isNetwork || ++failures > 3) throw e
                delay(1000L * (1 shl failures))
                offset = headOffset(url) ?: offset
            }
        }
        return docId
    }

    private fun b64(s: String) = Base64.getEncoder().encodeToString(s.toByteArray(Charsets.UTF_8))

    private suspend fun create(total: Long, meta: Map<String, String>): String {
        val md = meta.filterValues { it.isNotEmpty() }.entries.joinToString(",") { "${it.key} ${b64(it.value)}" }
        val req = Request.Builder().url(api.url("/uploads")).header("Tus-Resumable", version).header("Upload-Length", total.toString()).header("Upload-Metadata", md)
            .post(ByteArray(0).toRequestBody()).build()
        val loc = api.execute(req) { it.header("Location") } ?: throw ApiException(0, "tus", "The server didn't start the upload")
        return if (loc.startsWith("http")) loc else api.baseUrl + loc
    }

    /** The server's byte count for an upload begun earlier, or null if it has been forgotten. */
    private suspend fun headOffset(url: String): Long? {
        val req = Request.Builder().url(url).head().header("Tus-Resumable", version).build()
        return try {
            api.execute(req) { it.header("Upload-Offset")?.toLongOrNull() ?: 0L }
        } catch (e: ApiException) {
            if (e.status == 404 || e.status == 410) null else throw e
        }
    }

    private suspend fun patch(url: String, file: File, offset: Long, length: Long, onSent: (Long) -> Unit): Pair<Long, String?> {
        val body = object : RequestBody() {
            override fun contentType() = "application/offset+octet-stream".toMediaType()
            override fun contentLength() = length
            override fun writeTo(sink: BufferedSink) {
                RandomAccessFile(file, "r").use { raf ->
                    raf.seek(offset)
                    val buf = ByteArray(32 * 1024)
                    var left = length
                    var sent = 0L
                    while (left > 0) {
                        val n = raf.read(buf, 0, minOf(buf.size.toLong(), left).toInt())
                        if (n < 0) break
                        sink.write(buf, 0, n)
                        left -= n
                        sent += n
                        onSent(sent)
                    }
                }
            }
        }
        val req = Request.Builder().url(url).header("Tus-Resumable", version).header("Upload-Offset", offset.toString()).patch(body).build()
        return api.execute(req) { (it.header("Upload-Offset")?.toLongOrNull() ?: (offset + length)) to it.header("Docveta-Document-Id") }
    }
}
