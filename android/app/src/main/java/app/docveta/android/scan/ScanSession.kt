package app.docveta.android.scan

import android.graphics.Bitmap
import android.net.Uri
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import app.docveta.android.AppContainer
import app.docveta.android.data.AppJson
import java.io.File
import java.text.SimpleDateFormat
import java.util.Date
import java.util.Locale
import java.util.UUID
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import kotlinx.serialization.builtins.ListSerializer

/** One scanned page: the upright photo on disk, where its corners are, and how it should look when done. */
data class ScanPage(
    val id: String,
    val file: File,
    val width: Int,
    val height: Int,
    val quad: Quad,
    val detected: Boolean,
    val filter: PageFilters.Kind = PageFilters.Kind.ENHANCED,
    val turns: Int = 0,
)

/**
 * A scan in progress: pages from the camera or gallery, each with its own crop, filter and
 * turn, and finally one PDF (or several pictures) handed to the upload queue.
 */
class ScanSession(private val c: AppContainer) : ViewModel() {
    var pages by mutableStateOf<List<ScanPage>>(emptyList())
        private set
    var title by mutableStateOf(defaultTitle())
    var spaceId by mutableStateOf<String?>(null)
    var asPdf by mutableStateOf(true)
    var filterForNew by mutableStateOf(initialFilter())
    var lossless by mutableStateOf(c.session.scanLossless)
        private set
    var working by mutableStateOf(false)
        private set
    var error by mutableStateOf<String?>(null)

    private val dir = File(cacheDirOf(c), "scans").apply { mkdirs() }

    private fun initialFilter() = runCatching { PageFilters.Kind.valueOf(c.session.scanFilter ?: "ENHANCED") }.getOrDefault(PageFilters.Kind.ENHANCED)

    fun chooseFilterForNew(k: PageFilters.Kind) {
        filterForNew = k
        c.session.scanFilter = k.name
    }

    fun chooseLossless(v: Boolean) {
        lossless = v
        c.session.scanLossless = v
    }

    /**
     * Adds a captured or imported photo: made upright, saved at a sensible size, page found. When
     * the edges can't be found, a camera shot starts from a slightly inset frame (it surely shows
     * some table), an imported picture from the whole picture (it is often a scan already).
     */
    fun add(open: () -> java.io.InputStream, orientation: Int, fromGallery: Boolean = false, onAdded: (ScanPage) -> Unit = {}) {
        viewModelScope.launch {
            working = true
            try {
                val page = withContext(Dispatchers.Default) {
                    val bmp = ImageIO.decodeUpright(open, orientation, MAX_SOURCE)
                    val id = UUID.randomUUID().toString()
                    val f = File(dir, "$id.jpg")
                    ImageIO.saveJpeg(bmp, f, 100) // near-lossless: the final page is encoded again
                    val found = findPage(bmp)
                    val fallback = if (fromGallery) Quad.full(bmp.width.toFloat(), bmp.height.toFloat()) else Quad.inset(bmp.width.toFloat(), bmp.height.toFloat())
                    val p = ScanPage(id, f, bmp.width, bmp.height, found ?: fallback, found != null, filterForNew)
                    bmp.recycle()
                    p
                }
                pages = pages + page
                onAdded(page)
            } catch (e: Exception) {
                error = "Couldn't use that picture"
            } finally {
                working = false
            }
        }
    }

    fun addFile(file: File, onAdded: (ScanPage) -> Unit = {}) {
        val orientation = ImageIO.orientationOf { file.inputStream() }
        add({ file.inputStream() }, orientation) { onAdded(it); file.delete() }
    }

    fun addUri(uri: Uri, resolver: android.content.ContentResolver, onAdded: (ScanPage) -> Unit = {}) {
        val orientation = ImageIO.orientationOf { resolver.openInputStream(uri)!! }
        add({ resolver.openInputStream(uri)!! }, orientation, fromGallery = true, onAdded = onAdded)
    }

    fun update(id: String, f: (ScanPage) -> ScanPage) {
        pages = pages.map { if (it.id == id) f(it) else it }
    }

    fun remove(id: String) {
        pages.firstOrNull { it.id == id }?.file?.delete()
        pages = pages.filterNot { it.id == id }
    }

    fun move(id: String, delta: Int) {
        val i = pages.indexOfFirst { it.id == id }
        val j = i + delta
        if (i < 0 || j !in pages.indices) return
        pages = pages.toMutableList().also { val t = it[i]; it[i] = it[j]; it[j] = t }
    }

    /** Finds the page again (after the person moved the corners and wants a fresh guess). */
    suspend fun redetect(id: String): Quad? {
        val p = pages.firstOrNull { it.id == id } ?: return null
        return withContext(Dispatchers.Default) {
            val bmp = ImageIO.decodeUpright(p.file, MAX_SOURCE)
            val found = findPage(bmp)
            bmp.recycle()
            found
        }
    }

    private fun findPage(bmp: Bitmap): Quad? {
        return PageFinder.page(PageFinder.get(c.context), ImageIO.toRaster(bmp), DocumentDetector.PHOTO_EDGE_SIDE)
    }

    /** The page as it will come out, at preview size. */
    suspend fun preview(p: ScanPage, maxSide: Int = 900): Bitmap = withContext(Dispatchers.Default) {
        val bmp = ImageIO.decodeUpright(p.file, maxSide)
        val k = bmp.width.toFloat() / p.width
        val raster = ImageIO.toRaster(bmp)
        bmp.recycle()
        ImageIO.toBitmap(finish(raster, p.quad.scaled(k, k), p, maxSide))
    }

    private fun finish(src: Raster, quad: Quad, p: ScanPage, maxSide: Int): Raster {
        val flat = Rectifier.warp(src, quad, maxSide)
        return PageFilters.rotate(PageFilters.apply(flat, p.filter), p.turns)
    }

    /** A file to upload, with its pages kept aside when the phone will read its text (see [PhoneOcr]). */
    class Rendered(val file: File, val name: String, val ocrPages: File? = null)

    /** Builds the file(s) to upload from every page. Returns the PDF, or one JPEG per page. */
    suspend fun render(onProgress: (Int, Int) -> Unit = { _, _ -> }): List<Rendered> = withContext(Dispatchers.Default) {
        val out = ArrayList<Rendered>()
        val images = ArrayList<PdfPageImage>()
        val pdf = asPdf || pages.size == 1
        val base = title.trim().ifBlank { defaultTitle() }
        val ocrPages = if (pdf && PhoneOcr.wanted(c.session.phoneOcr, c.session.serverReadsText)) File(dir, "ocr-${UUID.randomUUID()}").apply { mkdirs() } else null
        val ocrList = ArrayList<OcrPageFile>()
        pages.forEachIndexed { i, p ->
            onProgress(i, pages.size)
            val bmp = ImageIO.decodeUpright(p.file, MAX_SOURCE)
            val raster = ImageIO.toRaster(bmp)
            bmp.recycle()
            val done = finish(raster, p.quad, p, 2339)
            val finalBmp = ImageIO.toBitmap(done)
            if (pdf) {
                // ponytail: every page is held in memory until the PDF is written; lossless colour
                // pages are a few MB each, so stream them to files first if long scans run out of memory.
                val img = if (lossless) PdfWriter.lossless(done) else PdfPageImage(ImageIO.jpeg(finalBmp, 85), done.w, done.h)
                images.add(img)
                if (ocrPages != null) {
                    File(ocrPages, "p$i.jpg").writeBytes(if (lossless) ImageIO.jpeg(finalBmp, 90) else img.data)
                    val raw = if (lossless) "p$i.bin".also { File(ocrPages, it).writeBytes(img.data) } else null
                    ocrList.add(OcrPageFile("p$i.jpg", done.w, done.h, img.gray, raw, img.bits))
                }
            } else {
                val ext = if (lossless) "png" else "jpg"
                val f = File(dir, "out-${UUID.randomUUID()}.$ext").also { it.writeBytes(if (lossless) ImageIO.png(finalBmp) else ImageIO.jpeg(finalBmp, 85)) }
                out.add(Rendered(f, "$base ${i + 1}.$ext"))
            }
            finalBmp.recycle()
        }
        if (pdf) {
            val f = File(dir, "out-${UUID.randomUUID()}.pdf")
            f.outputStream().use { PdfWriter.write(images, it, base) }
            ocrPages?.let { File(it, "pages.json").writeText(AppJson.encodeToString(ListSerializer(OcrPageFile.serializer()), ocrList)) }
            out.add(Rendered(f, "$base.pdf", ocrPages))
        }
        onProgress(pages.size, pages.size)
        out
    }

    /** Renders and puts the result in the upload queue. Calls [onDone] when queued. */
    fun submit(onDone: () -> Unit) {
        if (pages.isEmpty() || working) return
        viewModelScope.launch {
            working = true
            try {
                val files = render()
                val script = PhoneOcr.script(c.repo.cachedMe()?.spaces?.firstOrNull { it.id == spaceId }?.defaultLanguage)
                for (r in files) {
                    val mime = when (r.name.substringAfterLast('.')) { "pdf" -> "application/pdf"; "png" -> "image/png"; else -> "image/jpeg" }
                    c.uploads.enqueueFile(r.file, r.name, mime, spaceId, "scan", r.name.substringBeforeLast('.'), r.ocrPages, script)
                }
                if (files.any { it.ocrPages != null }) PhoneOcr.prepare(c.context, script)
                pages.forEach { it.file.delete() }
                pages = emptyList()
                title = defaultTitle()
                onDone()
            } catch (e: Exception) {
                error = "Couldn't build the document: ${e.message}"
            } finally {
                working = false
            }
        }
    }

    fun discard() {
        pages.forEach { it.file.delete() }
        pages = emptyList()
    }

    override fun onCleared() {
        // Leftovers of an abandoned scan are only cache; the system clears them, but don't wait.
        pages.forEach { it.file.delete() }
    }

    companion object {
        const val MAX_SOURCE = 3200
        fun defaultTitle(): String = "Scan " + SimpleDateFormat("yyyy-MM-dd HH.mm", Locale.US).format(Date())
        private fun cacheDirOf(c: AppContainer): File = c.cacheDir
    }
}
