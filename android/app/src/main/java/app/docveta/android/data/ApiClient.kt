package app.docveta.android.data

import java.io.File
import java.io.IOException
import java.util.concurrent.TimeUnit
import kotlin.coroutines.resume
import kotlin.coroutines.resumeWithException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.suspendCancellableCoroutine
import kotlinx.coroutines.withContext
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.contentOrNull
import okhttp3.Call
import okhttp3.Callback
import okhttp3.HttpUrl
import okhttp3.HttpUrl.Companion.toHttpUrl
import okhttp3.Interceptor
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody
import okhttp3.RequestBody.Companion.asRequestBody
import okhttp3.RequestBody.Companion.toRequestBody
import okhttp3.Response

/** A problem answered by the server (RFC 9457) or a network failure (status 0). */
class ApiException(val status: Int, val code: String, override val message: String, val fields: List<FieldProblem> = emptyList(), val extra: Map<String, String> = emptyMap()) :
    IOException(message) {
    val isNetwork get() = status == 0
    val isUnauthorized get() = status == 401
    fun fieldMessage(field: String) = fields.firstOrNull { it.field == field }?.message
}

val AppJson = Json {
    ignoreUnknownKeys = true
    coerceInputValues = true
    explicitNulls = false
}

/** Turns what a person typed ("docs.example.com", "192.168.1.20:8080") into a base URL, or null if it can't be one. */
fun normalizeServerUrl(input: String): String? {
    var s = input.trim().trimEnd('/')
    if (s.isEmpty()) return null
    if (!s.contains("://")) {
        val host = s.substringBefore('/').substringBefore(':')
        val local = host == "localhost" || host.endsWith(".local") || Regex("^(10|127|192\\.168|172\\.(1[6-9]|2\\d|3[01]))\\.").containsMatchIn(host) || host.matches(Regex("^\\d+\\.\\d+\\.\\d+\\.\\d+$"))
        s = (if (local) "http://" else "https://") + s
    }
    s = s.removeSuffix("/api/v1").removeSuffix("/api")
    return runCatching { s.toHttpUrl() }.getOrNull()?.let { it.toString().trimEnd('/') }
}

/**
 * The HTTP layer: bearer-token auth, the Origin header the server's CSRF check wants, JSON in and
 * out, and problems turned into [ApiException]. No retrofit; the API is small.
 */
class ApiClient(
    private val session: SessionStore,
    val http: OkHttpClient = baseHttp(),
    private val onUnauthorized: () -> Unit = {},
) {
    private val authed: OkHttpClient = http.newBuilder().addInterceptor(authInterceptor()).build()

    /** The client used for images and downloads (Coil shares it so thumbnails carry the token). */
    val imageClient: OkHttpClient get() = authed

    val baseUrl: String get() = session.serverUrl.orEmpty()

    private fun authInterceptor() = Interceptor { chain ->
        val b = chain.request().newBuilder()
        val token = session.token
        val base = session.serverUrl
        // Only talk credentials to our own server (images may come from nowhere else, but be strict).
        val ours = base != null && chain.request().url.toString().startsWith(base)
        if (ours) {
            if (token != null) b.header("Authorization", "Bearer $token")
            b.header("Origin", origin(base!!))
        }
        b.header("User-Agent", "DocvetaAndroid/0.1")
        val res = chain.proceed(b.build())
        if (res.code == 401 && token != null && ours) onUnauthorized()
        res
    }

    fun url(path: String, query: Map<String, String?> = emptyMap(), multi: List<Pair<String, String>> = emptyList()): HttpUrl {
        val b = (baseUrl + "/api/v1" + path).toHttpUrl().newBuilder()
        for ((k, v) in query) if (v != null && v != "") b.addQueryParameter(k, v)
        for ((k, v) in multi) b.addQueryParameter(k, v)
        return b.build()
    }

    suspend fun send(method: String, path: String, query: Map<String, String?> = emptyMap(), multi: List<Pair<String, String>> = emptyList(), body: String? = null, headers: Map<String, String> = emptyMap()): String {
        val rb: RequestBody? = when {
            body != null -> body.toRequestBody(JSON)
            method == "POST" || method == "PUT" || method == "PATCH" -> "".toRequestBody(JSON)
            else -> null
        }
        val req = Request.Builder().url(url(path, query, multi)).method(method, rb).header("Accept", "application/json").apply { headers.forEach { (k, v) -> header(k, v) } }.build()
        return execute(req) { it.body?.string().orEmpty() }
    }

    suspend inline fun <reified T> get(path: String, query: Map<String, String?> = emptyMap(), multi: List<Pair<String, String>> = emptyList()): T =
        AppJson.decodeFromString(send("GET", path, query, multi))

    suspend inline fun <reified T> post(path: String, body: String? = null, query: Map<String, String?> = emptyMap()): T =
        AppJson.decodeFromString(send("POST", path, query, body = body))

    suspend inline fun <reified T> patch(path: String, body: String, headers: Map<String, String> = emptyMap()): T =
        AppJson.decodeFromString(send("PATCH", path, body = body, headers = headers))

    suspend inline fun <reified T> put(path: String, body: String): T =
        AppJson.decodeFromString(send("PUT", path, body = body))

    /** Sends a file as multipart/form-data (field "file"), with extra text fields first. */
    suspend fun sendFile(path: String, file: File, mime: String, name: String, fields: Map<String, String> = emptyMap()): String {
        val body = okhttp3.MultipartBody.Builder().setType(okhttp3.MultipartBody.FORM).apply {
            fields.forEach { (k, v) -> addFormDataPart(k, v) }
            addFormDataPart("file", name, file.asRequestBody(mime.toMediaType()))
        }.build()
        val req = Request.Builder().url(url(path)).post(body).header("Accept", "application/json").build()
        val client = authed.newBuilder().writeTimeout(10, TimeUnit.MINUTES).readTimeout(5, TimeUnit.MINUTES).build()
        return execute(req, client) { it.body?.string().orEmpty() }
    }

    suspend fun delete(path: String, query: Map<String, String?> = emptyMap()) {
        send("DELETE", path, query)
    }

    /** Runs a request and maps non-2xx answers to [ApiException]. [read] runs on the response before it is closed. */
    suspend fun <T> execute(req: Request, client: OkHttpClient = authed, read: (Response) -> T): T = withContext(Dispatchers.IO) {
        val call = client.newCall(req)
        val res = try {
            call.await()
        } catch (e: IOException) {
            throw ApiException(0, "network", "Can't reach the server. Check your connection.")
        }
        res.use {
            if (!it.isSuccessful) throw problem(it)
            read(it)
        }
    }

    /** Saves a file from the server, reporting progress as 0..1 (or -1 when the size isn't known). */
    suspend fun download(path: String, query: Map<String, String?>, dest: File, onProgress: (Float) -> Unit = {}) {
        val req = Request.Builder().url(url(path, query)).build()
        execute(req) { res ->
            val body = res.body ?: throw ApiException(0, "empty", "The server sent nothing")
            val total = body.contentLength()
            dest.parentFile?.mkdirs()
            val tmp = File(dest.path + ".part")
            tmp.outputStream().use { out ->
                body.byteStream().use { input ->
                    val buf = ByteArray(64 * 1024)
                    var done = 0L
                    while (true) {
                        val n = input.read(buf)
                        if (n < 0) break
                        out.write(buf, 0, n)
                        done += n
                        onProgress(if (total > 0) done.toFloat() / total else -1f)
                    }
                }
            }
            if (!tmp.renameTo(dest)) throw IOException("Couldn't save the file")
        }
    }

    companion object {
        val JSON = "application/json".toMediaType()

        fun origin(base: String): String {
            val u = base.toHttpUrl()
            val default = (u.scheme == "https" && u.port == 443) || (u.scheme == "http" && u.port == 80)
            return u.scheme + "://" + u.host + if (default) "" else ":" + u.port
        }

        fun baseHttp(): OkHttpClient = OkHttpClient.Builder()
            .connectTimeout(15, TimeUnit.SECONDS)
            .readTimeout(60, TimeUnit.SECONDS)
            .writeTimeout(60, TimeUnit.SECONDS)
            .build()

        fun problem(res: Response): ApiException {
            val text = runCatching { res.body?.string() }.getOrNull().orEmpty()
            val p = runCatching { AppJson.decodeFromString<ApiProblem>(text) }.getOrNull()
            val msg = p?.title?.takeIf { it.isNotBlank() } ?: when {
                res.code == 413 -> "This file is too large"
                res.code >= 500 -> "The server had a problem. Please try again."
                res.code == 401 -> "Please sign in again"
                else -> "Request failed (${res.code})"
            }
            val extra = p?.extra?.mapValues { (_, v) -> (v as? JsonPrimitive)?.contentOrNull ?: v.toString() }.orEmpty()
            return ApiException(res.code, p?.code ?: "error", msg, p?.errors.orEmpty(), extra)
        }
    }
}

suspend fun Call.await(): Response = suspendCancellableCoroutine { cont ->
    enqueue(object : Callback {
        override fun onFailure(call: Call, e: IOException) {
            if (!cont.isCancelled) cont.resumeWithException(e)
        }

        override fun onResponse(call: Call, response: Response) {
            cont.resume(response)
        }
    })
    cont.invokeOnCancellation { runCatching { cancel() } }
}
