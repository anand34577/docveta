package app.docveta.android.scan

import android.app.ActivityManager
import android.content.Context
import android.content.Intent
import android.content.IntentFilter
import android.graphics.BitmapFactory
import android.os.BatteryManager
import android.os.PowerManager
import java.io.File
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import kotlinx.serialization.Serializable

/**
 * Pages of a scan kept beside the queued PDF until the phone has read their text. [file] is the
 * JPEG that is read; a lossless scan keeps the page's Flate pixels ([PdfWriter.lossless]) in [raw].
 */
@Serializable
data class OcrPageFile(val file: String, val width: Int, val height: Int, val gray: Boolean = false, val raw: String? = null, val bits: Int = 8)

/**
 * Reading a scan's text on the phone, with the same PaddleOCR models as the server's OCR engine
 * ([OnnxOcr], [OcrModels]): offline, no Google services. The words go into the PDF as an invisible
 * text layer, so the file is searchable on its own and the server uses that text instead of
 * reading the pages again.
 *
 * It runs in the background after the scan is queued (it carries on if the app is closed), only
 * on phones that can spare the memory and battery, and never holds an upload back: if anything
 * fails, the scan goes as it is and the server reads it.
 */
object PhoneOcr {
    /**
     * The script a space's language suggests ("" or "auto": guess from the phone). Only a hint:
     * each page's script is worked out from its text.
     */
    fun script(language: String?): String {
        val lang = language?.takeIf { it.isNotBlank() && it.lowercase() != "auto" }
        if (lang != null) return PpOcr.scriptOf(lang) ?: "en"
        val l = java.util.Locale.getDefault()
        return PpOcr.scriptOf(l.language)?.takeIf { it != "en" } ?: if (l.country == "IN") "devanagari" else "en"
    }

    /** Scans queued by older versions say "latin" for English. */
    private fun normalised(script: String) = if (script == "latin") "en" else script

    /** "auto": read here when the server can't read text itself; "on": whenever the phone can; "off": never. */
    fun wanted(mode: String, serverReadsText: Boolean) = when (mode) {
        "on" -> true
        "off" -> false
        else -> !serverReadsText
    }

    /** Whether this phone can read text at all: a 64-bit phone (the app carries no reader for 32-bit ones) and a build with the models. */
    fun supported(ctx: Context) = android.os.Process.is64Bit() && OcrModels.present(ctx)

    /** Whether the phone can spare it now: a supported phone with the memory, not in battery saver, not nearly flat. */
    fun canRunNow(ctx: Context): Boolean {
        if (!supported(ctx)) return false
        val am = ctx.getSystemService(ActivityManager::class.java)
        val mem = ActivityManager.MemoryInfo().also { am.getMemoryInfo(it) }
        if (am.isLowRamDevice || mem.totalMem < 2_500_000_000L || mem.lowMemory) return false
        if (ctx.getSystemService(PowerManager::class.java).isPowerSaveMode) return false
        val battery = ctx.registerReceiver(null, IntentFilter(Intent.ACTION_BATTERY_CHANGED))
        val level = battery?.getIntExtra(BatteryManager.EXTRA_LEVEL, -1) ?: -1
        val scale = battery?.getIntExtra(BatteryManager.EXTRA_SCALE, 100) ?: 100
        val charging = (battery?.getIntExtra(BatteryManager.EXTRA_PLUGGED, 0) ?: 0) != 0
        return charging || level < 0 || level * 100 / scale >= 20
    }

    /** Gets [script]'s reader ready for the next scan: Tamil, Telugu and Kannada are fetched once (on Wi-Fi). */
    fun prepare(ctx: Context, script: String) {
        OcrModels.fetch(ctx, normalised(script), anyNetwork = false)
    }

    /**
     * Reads the pages in [dir] and writes [pdf] again with their text. Returns false when there was
     * nothing to add (no text found, or this build has no models).
     */
    suspend fun makeSearchable(ctx: Context, dir: File, pdf: File, title: String, script: String): Boolean = withContext(Dispatchers.Default) {
        val list = File(dir, "pages.json").takeIf { it.exists() } ?: return@withContext false
        val pages = app.docveta.android.data.AppJson.decodeFromString(kotlinx.serialization.builtins.ListSerializer(OcrPageFile.serializer()), list.readText())
        val scripts = OcrModels.available(ctx)
        if (scripts.isEmpty()) return@withContext false
        OnnxOcr({ OcrModels.model(ctx, it) }, { OcrModels.dict(ctx, it) }, scripts).use { ocr ->
            var anyText = false
            val out = pages.map { p ->
                val jpeg = File(dir, p.file).readBytes()
                val words = read(ocr, jpeg, p.width, listOf(normalised(script)))
                if (words.isNotEmpty()) anyText = true
                val raw = p.raw?.let { File(dir, it).readBytes() }
                PdfPageImage(raw ?: jpeg, p.width, p.height, p.gray, words, flate = raw != null, bits = p.bits)
            }
            if (!anyText) return@withContext false
            val tmp = File(pdf.parentFile, pdf.name + ".part")
            tmp.outputStream().use { PdfWriter.write(out, it, title) }
            if (!tmp.renameTo(pdf)) {
                tmp.copyTo(pdf, overwrite = true)
                tmp.delete()
            }
            true
        }
    }

    /** Words on one page, in the page's own pixels. Big pages are read at most 2000 px wide (enough for text, kind to memory). */
    private fun read(ocr: OnnxOcr, jpeg: ByteArray, width: Int, hinted: List<String>): List<PdfWord> {
        var sample = 1
        while (width / (sample * 2) >= 2000) sample *= 2
        val bmp = BitmapFactory.decodeByteArray(jpeg, 0, jpeg.size, BitmapFactory.Options().apply { inSampleSize = sample }) ?: return emptyList()
        val k = width.toFloat() / bmp.width
        val page = ImageIO.toRaster(bmp)
        bmp.recycle()
        return ocr.readPage(page, hinted).map { it.copy(x = it.x * k, y = it.y * k, w = it.w * k, h = it.h * k) }
    }
}
