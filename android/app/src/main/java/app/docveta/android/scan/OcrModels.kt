package app.docveta.android.scan

import android.content.Context
import androidx.work.Constraints
import androidx.work.CoroutineWorker
import androidx.work.ExistingWorkPolicy
import androidx.work.NetworkType
import androidx.work.OneTimeWorkRequestBuilder
import androidx.work.WorkInfo
import androidx.work.WorkManager
import androidx.work.WorkerParameters
import androidx.work.workDataOf
import app.docveta.android.BuildConfig
import java.io.File
import java.security.MessageDigest
import java.util.concurrent.TimeUnit
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.withContext
import okhttp3.OkHttpClient
import okhttp3.Request

/**
 * The text-reading models: the PaddleOCR models the server uses, as ONNX. The text finder and the
 * English and Devanagari readers come with the app (assets/ocr); Tamil, Telugu and Kannada are
 * fetched once from this version's GitHub release, when a scan needs them or when asked in
 * Settings, and checked against the SHA-256 recorded when the app was built.
 */
object OcrModels {
    private const val ASSETS = "ocr"

    /** Human names for the settings screen. */
    val NAMES = mapOf("en" to "English", "devanagari" to "Hindi, Marathi, Nepali (Devanagari)", "ta" to "Tamil", "te" to "Telugu", "ka" to "Kannada")

    private fun dir(ctx: Context) = File(ctx.filesDir, "ocr-models")

    private fun bundled(ctx: Context): Set<String> = runCatching { ctx.assets.list(ASSETS)?.toSet() }.getOrNull().orEmpty()

    /** Whether this build carries the models at all (a build made without them reads nothing; the server does it). */
    fun present(ctx: Context) = "det.onnx" in bundled(ctx)

    /** Scripts that can be read now, bundled or downloaded. */
    fun available(ctx: Context): List<String> {
        val assets = bundled(ctx)
        if ("det.onnx" !in assets) return emptyList()
        return PpOcr.SCRIPTS.filter { s -> "dict_$s.txt" in assets && ("rec_$s.onnx" in assets || File(dir(ctx), "rec_$s.onnx").isFile) }
    }

    /** Scripts that can be downloaded (known to this build, not on the phone yet). */
    fun downloadable(ctx: Context): List<String> {
        val have = available(ctx)
        return checksums(ctx).keys.map { it.removePrefix("rec_").removeSuffix(".onnx") }.filter { it in PpOcr.SCRIPTS && it !in have }
    }

    fun model(ctx: Context, name: String): ByteArray {
        val f = File(dir(ctx), name)
        return if (f.isFile) f.readBytes() else ctx.assets.open("$ASSETS/$name").use { it.readBytes() }
    }

    fun dict(ctx: Context, script: String): String = ctx.assets.open("$ASSETS/dict_$script.txt").use { it.readBytes().toString(Charsets.UTF_8) }

    /** "sha256  file" lines written by the build for every model, bundled or not. */
    private fun checksums(ctx: Context): Map<String, String> = runCatching {
        ctx.assets.open("$ASSETS/checksums.txt").bufferedReader().readLines()
            .mapNotNull { l -> l.trim().split(Regex("\\s+")).takeIf { it.size == 2 }?.let { it[1] to it[0].lowercase() } }.toMap()
    }.getOrDefault(emptyMap())

    /** Fetches [script]'s reader in the background. [anyNetwork] false waits for Wi-Fi (it's about 9 MB). */
    fun fetch(ctx: Context, script: String, anyNetwork: Boolean) {
        if (script !in downloadable(ctx)) return
        val req = OneTimeWorkRequestBuilder<OcrModelWorker>()
            .setInputData(workDataOf("script" to script))
            .setConstraints(Constraints.Builder().setRequiredNetworkType(if (anyNetwork) NetworkType.CONNECTED else NetworkType.UNMETERED).setRequiresStorageNotLow(true).build())
            .setBackoffCriteria(androidx.work.BackoffPolicy.EXPONENTIAL, 1, TimeUnit.MINUTES)
            .build()
        WorkManager.getInstance(ctx).enqueueUniqueWork(work(script), if (anyNetwork) ExistingWorkPolicy.REPLACE else ExistingWorkPolicy.KEEP, req)
    }

    /** Whether [script]'s download is queued or running. */
    fun fetching(ctx: Context, script: String): Flow<Boolean> =
        WorkManager.getInstance(ctx).getWorkInfosForUniqueWorkFlow(work(script)).map { l -> l.any { it.state == WorkInfo.State.ENQUEUED || it.state == WorkInfo.State.RUNNING || it.state == WorkInfo.State.BLOCKED } }

    fun remove(ctx: Context, script: String) {
        File(dir(ctx), "rec_$script.onnx").delete()
    }

    private fun work(script: String) = "ocr-model-$script"

    internal suspend fun download(ctx: Context, script: String) = withContext(Dispatchers.IO) {
        val name = "rec_$script.onnx"
        val want = checksums(ctx)[name] ?: error("no checksum for $name")
        val tmp = File(dir(ctx), "$name.part").also { it.parentFile?.mkdirs() }
        val client = OkHttpClient.Builder().readTimeout(60, TimeUnit.SECONDS).build()
        client.newCall(Request.Builder().url(BuildConfig.OCR_MODELS_URL + "docveta-ocr-$name").build()).execute().use { r ->
            if (!r.isSuccessful) error("HTTP ${r.code}")
            val md = MessageDigest.getInstance("SHA-256")
            r.body!!.byteStream().use { input ->
                tmp.outputStream().use { out ->
                    val buf = ByteArray(64 * 1024)
                    while (true) {
                        val n = input.read(buf)
                        if (n < 0) break
                        md.update(buf, 0, n)
                        out.write(buf, 0, n)
                    }
                }
            }
            val got = md.digest().joinToString("") { "%02x".format(it) }
            if (got != want) {
                tmp.delete()
                error("$name doesn't match this app's checksum")
            }
        }
        if (!tmp.renameTo(File(dir(ctx), name))) error("couldn't save $name")
    }
}

class OcrModelWorker(ctx: Context, params: WorkerParameters) : CoroutineWorker(ctx, params) {
    override suspend fun doWork(): Result {
        val script = inputData.getString("script") ?: return Result.failure()
        return try {
            OcrModels.download(applicationContext, script)
            Result.success()
        } catch (e: Exception) {
            if (runAttemptCount < 3) Result.retry() else Result.failure()
        }
    }
}
