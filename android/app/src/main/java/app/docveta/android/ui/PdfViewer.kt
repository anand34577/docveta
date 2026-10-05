package app.docveta.android.ui

import android.annotation.SuppressLint
import android.graphics.Bitmap
import android.os.Build
import android.os.ext.SdkExtensions
import android.graphics.Color as AColor
import android.graphics.pdf.PdfRenderer
import android.os.ParcelFileDescriptor
import android.util.LruCache
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.gestures.awaitEachGesture
import androidx.compose.foundation.gestures.awaitFirstDown
import androidx.compose.foundation.gestures.calculatePan
import androidx.compose.foundation.gestures.calculateZoom
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.derivedStateOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.produceState
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.composed
import androidx.compose.ui.draw.clipToBounds
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.input.pointer.PointerEventPass
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.input.pointer.positionChanged
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.unit.dp
import java.io.File
import kotlin.math.max
import kotlin.math.min
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ensureActive
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext

/** Renders PDF pages to bitmaps, one at a time (PdfRenderer isn't thread-safe), keeping a few in memory. */
class PdfPages(file: File) : AutoCloseable {
    private val fd = ParcelFileDescriptor.open(file, ParcelFileDescriptor.MODE_READ_ONLY)
    private val renderer = PdfRenderer(fd)
    private val lock = Mutex()
    private val cache = object : LruCache<String, Bitmap>(48 * 1024 * 1024) {
        override fun sizeOf(key: String, value: Bitmap) = value.byteCount
    }
    val count: Int get() = renderer.pageCount

    /** Width and height of a page in points (for sizing the placeholder before it's drawn). */
    suspend fun size(index: Int): Pair<Int, Int> = lock.withLock {
        withContext(Dispatchers.IO) { renderer.openPage(index).use { it.width to it.height } }
    }

    suspend fun render(index: Int, widthPx: Int): Bitmap {
        val key = "$index@$widthPx"
        cache.get(key)?.let { return it }
        return lock.withLock {
            cache.get(key) ?: withContext(Dispatchers.Default) {
                renderer.openPage(index).use { p ->
                    val h = (widthPx.toFloat() * p.height / p.width).toInt().coerceAtLeast(1)
                    val bmp = Bitmap.createBitmap(widthPx, h, Bitmap.Config.ARGB_8888)
                    bmp.eraseColor(AColor.WHITE)
                    p.render(bmp, null, null, PdfRenderer.Page.RENDER_MODE_FOR_DISPLAY)
                    cache.put(key, bmp)
                    bmp
                }
            }
        }
    }

    /** Every match of [query] with where it is on its page. Only where [canSearch]. */
    @SuppressLint("NewApi")
    suspend fun search(query: String): List<PdfMatch> = withContext(Dispatchers.Default) {
        val out = ArrayList<PdfMatch>()
        for (i in 0 until count) {
            coroutineContext.ensureActive()
            lock.withLock { renderer.openPage(i).use { p -> p.searchText(query).forEach { m -> out.add(PdfMatch(i, m.bounds)) } } }
        }
        out
    }

    override fun close() {
        runCatching { renderer.close() }
        runCatching { fd.close() }
    }

    companion object {
        /** Android can find text in a PDF itself from Android 15, or 12-14 with a recent system update. */
        val canSearch: Boolean by lazy {
            Build.VERSION.SDK_INT >= 35 || (Build.VERSION.SDK_INT >= 31 && runCatching { SdkExtensions.getExtensionVersion(Build.VERSION_CODES.S) >= 13 }.getOrDefault(false))
        }
    }
}

/**
 * Lets two fingers zoom and pan, double-tap toggle zoom, and leaves one finger free to scroll the list underneath
 * (it only takes over once zoomed in, and then only to pan).
 */
fun Modifier.pinchZoom(maxScale: Float = 5f, onScale: (Float) -> Unit = {}): Modifier = composed {
    var scale by remember { mutableFloatStateOf(1f) }
    var offset by remember { mutableStateOf(Offset.Zero) }
    fun clampOffset(o: Offset, size: androidx.compose.ui.unit.IntSize): Offset {
        val maxX = (size.width * (scale - 1f)) / 2f
        val maxY = (size.height * (scale - 1f)) / 2f
        return Offset(o.x.coerceIn(-maxX, maxX), o.y.coerceIn(-maxY, maxY))
    }
    this
        .clipToBounds()
        .pointerInput(Unit) {
            detectTapGestures(onDoubleTap = { tap ->
                if (scale > 1.05f) {
                    scale = 1f
                    offset = Offset.Zero
                } else {
                    scale = 2.5f
                    offset = clampOffset(Offset((size.width / 2f - tap.x) * (scale - 1f), (size.height / 2f - tap.y) * (scale - 1f)), size)
                }
                onScale(scale)
            })
        }
        .pointerInput(Unit) {
            awaitEachGesture {
                awaitFirstDown(requireUnconsumed = false, pass = PointerEventPass.Initial)
                do {
                    val event = awaitPointerEvent(PointerEventPass.Initial)
                    val multi = event.changes.count { it.pressed } > 1
                    if (multi || scale > 1.01f) {
                        val zoom = event.calculateZoom()
                        val pan = event.calculatePan()
                        val newScale = (scale * zoom).coerceIn(1f, maxScale)
                        scale = newScale
                        offset = if (newScale <= 1.01f) Offset.Zero else clampOffset(offset + pan, size)
                        onScale(scale)
                        event.changes.forEach { if (it.positionChanged()) it.consume() }
                    }
                } while (event.changes.any { it.pressed })
            }
        }
        .graphicsLayer {
            scaleX = scale
            scaleY = scale
            translationX = offset.x
            translationY = offset.y
        }
}

/** All pages of a PDF in a scrolling column, with pinch zoom. */
@Composable
fun PdfView(
    file: File,
    modifier: Modifier = Modifier,
    startPage: Int = 0,
    onPage: (Int, Int) -> Unit = { _, _ -> },
    find: PdfFindState? = null,
    pageTexts: (suspend () -> List<Pair<Int, String>>)? = null,
) {
    val pages = remember(file) { runCatching { PdfPages(file) }.getOrNull() }
    DisposableEffect(pages) { onDispose { pages?.close() } }
    if (pages == null) {
        Text("This PDF couldn't be shown. You can still open it in another app.", Modifier.padding(24.dp), style = MaterialTheme.typography.bodyMedium)
        return
    }
    val state = rememberLazyListState(initialFirstVisibleItemIndex = startPage.coerceIn(0, max(0, pages.count - 1)))
    // The page under the middle of the screen (the first visible one may be just its last few lines).
    val current by remember {
        derivedStateOf {
            val info = state.layoutInfo
            val mid = (info.viewportStartOffset + info.viewportEndOffset) / 2
            info.visibleItemsInfo.firstOrNull { it.offset <= mid && it.offset + it.size > mid }?.index ?: state.firstVisibleItemIndex
        }
    }
    LaunchedEffect(current) { onPage(current, pages.count) }
    // Find: Android's own search gives the places on the page; without it (or when the file has no
    // text layer yet) the server's text of each page still says which pages match.
    if (find != null) LaunchedEffect(find.open, find.query) {
        val q = find.query.trim()
        if (!find.open || q.isEmpty()) {
            find.matches = emptyList(); find.searched = false; find.searching = false
            return@LaunchedEffect
        }
        kotlinx.coroutines.delay(250)
        find.searching = true
        try {
            var found = if (PdfPages.canSearch) runCatching { pages.search(q) }.getOrDefault(emptyList()) else emptyList()
            var anyText = found.isNotEmpty()
            if (found.isEmpty() && pageTexts != null) {
                val texts = runCatching { pageTexts() }.getOrDefault(emptyList())
                anyText = texts.any { it.second.isNotBlank() }
                found = findInPageTexts(texts, q)
            } else if (found.isEmpty()) anyText = true
            find.show(found, anyText)
        } finally {
            find.searching = false
        }
    }
    val density = LocalDensity.current
    BoxWithConstraints(modifier.fillMaxSize().background(MaterialTheme.colorScheme.surfaceVariant)) {
        val widthPx = with(LocalDensity.current) { min(maxWidth.toPx() * 1.6f, 1800f).toInt() }
        // Bring the chosen match to the upper third of the screen.
        if (find != null) {
            val viewH = with(density) { maxHeight.toPx() }
            val itemW = with(density) { (maxWidth - 20.dp).toPx() }
            LaunchedEffect(find.seq) {
                val m = find.current ?: return@LaunchedEffect
                val top = m.rects.minOfOrNull { it.top }
                val offset = if (top == null) 0 else {
                    val (w, _) = pages.size(m.page)
                    (top / w * itemW - viewH / 3).toInt().coerceAtLeast(0)
                }
                state.animateScrollToItem(m.page, offset)
            }
        }
        LazyColumn(Modifier.fillMaxSize().pinchZoom(), state = state, contentPadding = PaddingValues(top = if (find?.open == true) 76.dp else 12.dp, bottom = 12.dp)) {
            itemsIndexed(List(pages.count) { it }) { i, _ ->
                val size by produceState(1f to 1.41f, pages, i) {
                    val (w, h) = pages.size(i)
                    value = w.toFloat() to h.toFloat()
                }
                val ratio = size.second / size.first
                val bmp by produceState<Bitmap?>(null, pages, i, widthPx) { value = runCatching { pages.render(i, widthPx) }.getOrNull() }
                Surface(Modifier.fillMaxWidth().padding(horizontal = 10.dp, vertical = 5.dp).aspectRatio(1f / ratio), shadowElevation = 2.dp, color = androidx.compose.ui.graphics.Color.White) {
                    bmp?.let { Image(it.asImageBitmap(), "Page ${i + 1}", Modifier.fillMaxSize(), contentScale = ContentScale.FillWidth) }
                    val hits = find?.matches?.withIndex()?.filter { it.value.page == i && it.value.rects.isNotEmpty() }.orEmpty()
                    if (hits.isNotEmpty()) Canvas(Modifier.fillMaxSize()) {
                        val k = this.size.width / size.first
                        for ((n, m) in hits) {
                            val active = n == find?.index
                            for (r in m.rects) {
                                drawRect(
                                    if (active) androidx.compose.ui.graphics.Color(0x99FF8A00) else androidx.compose.ui.graphics.Color(0x66FFD400),
                                    topLeft = Offset(r.left * k, r.top * k),
                                    size = androidx.compose.ui.geometry.Size(r.width() * k, r.height() * k),
                                )
                            }
                        }
                    }
                }
            }
        }
        // Where am I: "2 / 5", and a reminder that pages zoom.
        if (pages.count > 1) {
            Surface(
                Modifier.align(Alignment.BottomCenter).navigationBarsPadding().padding(bottom = 16.dp),
                shape = androidx.compose.foundation.shape.RoundedCornerShape(50),
                color = MaterialTheme.colorScheme.inverseSurface.copy(alpha = 0.82f),
                contentColor = MaterialTheme.colorScheme.inverseOnSurface,
            ) {
                Text("${current + 1} / ${pages.count}", Modifier.padding(horizontal = 14.dp, vertical = 6.dp), style = MaterialTheme.typography.labelLarge)
            }
        }
    }
}
