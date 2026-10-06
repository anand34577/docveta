package app.docveta.android.scan

import ai.onnxruntime.OnnxTensor
import ai.onnxruntime.OrtEnvironment
import ai.onnxruntime.OrtSession
import android.content.Context
import java.nio.FloatBuffer
import kotlin.math.abs
import kotlin.math.max
import kotlin.math.min

/**
 * Finds the page's four corners with DocAligner (DocsaidLab, Apache-2.0; the FastViT-SA24 heatmap
 * model in assets/scan, about 0.2 s a frame on a mid-range phone's CPU): a network that marks each
 * corner with a blob on a heatmap. Its smaller models (LCNet-100, FastViT-T8) cut off the dark
 * bottom strip of a magazine cover on real photos, so they aren't used. It
 * knows what a page looks like, so it copes where edges and brightness alone fall short: a white
 * page on a light desk, busy surfaces, shadows. Where it runs, its answer is final, "no page"
 * included: [DocumentDetector]'s edges find "pages" in blur and clutter, and override it badly on
 * real photos. That one is for phones where the model can't run.
 *
 * The same steps as DocAligner's own Python code, on ONNX Runtime; no Android classes, so it also
 * runs in the JVM tests.
 */
class PageFinder(
    model: ByteArray,
    // CPU only: on a mid-range phone NNAPI (GPU and NPU) was no faster, and XNNPACK slower.
    threads: Int = (Runtime.getRuntime().availableProcessors() / 2).coerceIn(1, 4),
) : AutoCloseable {
    private val env = OrtEnvironment.getEnvironment()
    private val options = OrtSession.SessionOptions().apply {
        setIntraOpNumThreads(threads)
        setOptimizationLevel(OrtSession.SessionOptions.OptLevel.ALL_OPT)
    }
    private val session = env.createSession(model, options)

    /** The page's corners in the picture's pixels, or null when there's no page in view. */
    fun find(img: Raster): Quad? {
        val small = resize(img, SIZE, SIZE)
        val n = SIZE * SIZE
        val buf = FloatBuffer.allocate(3 * n)
        for (c in 0 until 3) {
            val shift = c * 8 // B, G, R, from 0 to 1
            for (i in 0 until n) buf.put(((small[i] shr shift) and 0xFF) / 255f)
        }
        buf.rewind()
        OnnxTensor.createTensor(env, buf, longArrayOf(1, 3, SIZE.toLong(), SIZE.toLong())).use { t ->
            session.run(mapOf(session.inputNames.first() to t)).use { out ->
                val o = out.get(0) as OnnxTensor
                val shape = o.info.shape // (1, 4, h, w): top-left, top-right, bottom-right, bottom-left
                val fb = o.floatBuffer
                val maps = FloatArray(fb.remaining()).also { fb.get(it) }
                return corners(maps, shape[3].toInt(), shape[2].toInt(), img.w, img.h)
            }
        }
    }

    override fun close() {
        session.close()
        options.close()
    }

    companion object {
        const val SIZE = 256
        private const val THRESHOLD = 0.3f // DocAligner's own

        /**
         * Each heatmap's biggest blob above the threshold, at its weighted centre, in a [imgW] x [imgH]
         * picture. Null when a corner has no blob or the corners don't make a plausible page.
         */
        internal fun corners(maps: FloatArray, w: Int, h: Int, imgW: Int, imgH: Int): Quad? {
            val seen = BooleanArray(w * h)
            val stack = IntArray(w * h)
            val pts = (0 until 4).map { k ->
                val base = k * w * h
                seen.fill(false)
                var best: FloatArray? = null // count, weight, x sum, y sum
                for (start in 0 until w * h) {
                    if (seen[start] || maps[base + start] < THRESHOLD) continue
                    val b = FloatArray(4)
                    var sp = 0
                    stack[sp++] = start
                    seen[start] = true
                    while (sp > 0) {
                        val i = stack[--sp]
                        val v = maps[base + i]
                        val x = i % w
                        val y = i / w
                        b[0] += 1f; b[1] += v; b[2] += v * (x + 0.5f); b[3] += v * (y + 0.5f)
                        fun push(j: Int) { if (!seen[j] && maps[base + j] >= THRESHOLD) { seen[j] = true; stack[sp++] = j } }
                        if (x > 0) push(i - 1)
                        if (x < w - 1) push(i + 1)
                        if (y > 0) push(i - w)
                        if (y < h - 1) push(i + w)
                    }
                    if (best == null || b[0] > best[0]) best = b
                }
                val b = best ?: return null
                Pt(b[2] / b[1] * imgW / w, b[3] / b[1] * imgH / h)
            }
            val q = Quad.ordered(pts).clampedTo(imgW.toFloat(), imgH.toFloat())
            return q.takeIf { it.isConvex() && it.area() >= 0.05f * imgW * imgH }
        }

        /**
         * Shrinks (or grows) to nw x nh with a tent filter as wide as the scale, like Pillow's
         * bilinear resize: the model finds corners most reliably on this (fewer misses than plain
         * bilinear or box averaging, as OpenCV does them, in our tests).
         */
        internal fun resize(img: Raster, nw: Int, nh: Int): IntArray {
            val (xFrom, xW) = taps(img.w, nw)
            val (yFrom, yW) = taps(img.h, nh)
            // Rows first, into float channels, then columns.
            val tmp = Array(3) { FloatArray(nw * img.h) }
            for (y in 0 until img.h) for (x in 0 until nw) {
                var r = 0f; var g = 0f; var b = 0f
                val ws = xW[x]
                for (k in ws.indices) {
                    val c = img.px[y * img.w + xFrom[x] + k]
                    r += ws[k] * ((c shr 16) and 0xFF); g += ws[k] * ((c shr 8) and 0xFF); b += ws[k] * (c and 0xFF)
                }
                tmp[0][y * nw + x] = r; tmp[1][y * nw + x] = g; tmp[2][y * nw + x] = b
            }
            val out = IntArray(nw * nh)
            for (y in 0 until nh) for (x in 0 until nw) {
                val ws = yW[y]
                val ch = IntArray(3) { c ->
                    var s = 0f
                    for (k in ws.indices) s += ws[k] * tmp[c][(yFrom[y] + k) * nw + x]
                    (s + 0.5f).toInt().coerceIn(0, 255)
                }
                out[y * nw + x] = (0xFF shl 24) or (ch[0] shl 16) or (ch[1] shl 8) or ch[2]
            }
            return out
        }

        /** For each output pixel: the first source pixel it reads and the normalised weights from there on. */
        private fun taps(src: Int, dst: Int): Pair<IntArray, Array<FloatArray>> {
            val scale = src.toFloat() / dst
            val support = max(1f, scale)
            val from = IntArray(dst)
            val weights = Array(dst) { i ->
                val centre = (i + 0.5f) * scale
                val a = max(0, (centre - support + 0.5f).toInt())
                val b = min(src, (centre + support + 0.5f).toInt())
                from[i] = a
                val ws = FloatArray(max(1, b - a)) { k -> max(0f, 1f - abs((a + k + 0.5f - centre) / support)) }
                val sum = ws.sum()
                if (sum > 0f) {
                    for (k in ws.indices) ws[k] /= sum
                } else {
                    ws[0] = 1f
                }
                ws
            }
            return from to weights
        }

        /** The page in [img]: by the model where it runs (null: no page), else by edges ([DocumentDetector]). */
        fun page(finder: PageFinder?, img: Raster, edgeSide: Int = DocumentDetector.LIVE_EDGE_SIDE): Quad? =
            if (finder != null) finder.find(img) else DocumentDetector.detect(img.luma(), img.w, img.h, edgeSide)

        @Volatile private var shared: PageFinder? = null
        @Volatile private var unavailable = false

        /**
         * The app's one finder, loaded on first use. Null where the model can't run: 32-bit phones
         * ship without ONNX Runtime (see build.gradle.kts), and there the edges alone find the page.
         */
        fun get(ctx: Context): PageFinder? {
            shared?.let { return it }
            if (unavailable) return null
            synchronized(this) {
                shared?.let { return it }
                return try {
                    PageFinder(ctx.assets.open("scan/docaligner.onnx").use { it.readBytes() }).also { shared = it }
                } catch (e: Throwable) { // UnsatisfiedLinkError without the native library
                    unavailable = true
                    android.util.Log.w("PageFinder", "DocAligner can't run here; finding pages by edges only", e)
                    null
                }
            }
        }
    }
}
