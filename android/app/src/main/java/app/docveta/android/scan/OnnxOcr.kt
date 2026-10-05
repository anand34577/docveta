package app.docveta.android.scan

import ai.onnxruntime.OnnxTensor
import ai.onnxruntime.OrtEnvironment
import ai.onnxruntime.OrtSession
import java.nio.FloatBuffer
import kotlin.math.max

/**
 * PaddleOCR on ONNX Runtime, on the processor: the same models and steps as the server's engine
 * (workers/onnx/worker.py). No Android classes, so it also runs in the JVM tests.
 *
 * [model] loads a model file by name (det.onnx, rec_<script>.onnx), [dict] a script's
 * character list; [scripts] are the scripts whose readers can be loaded.
 */
class OnnxOcr(
    private val model: (String) -> ByteArray,
    private val dict: (String) -> String,
    val scripts: List<String>,
    threads: Int = (Runtime.getRuntime().availableProcessors() / 2).coerceIn(1, 4),
) : AutoCloseable {
    private val env = OrtEnvironment.getEnvironment()
    private val options = OrtSession.SessionOptions().apply {
        setIntraOpNumThreads(threads)
        setOptimizationLevel(OrtSession.SessionOptions.OptLevel.ALL_OPT)
    }
    private val det = env.createSession(model("det.onnx"), options)
    private val rec = HashMap<String, OrtSession>()
    private val charsets = HashMap<String, List<String>>()

    /**
     * The words on a page, in the page's pixels. [hinted] scripts are tried first (from the space's
     * language), but the page's script is worked out from the text itself.
     */
    fun readPage(page: Raster, hinted: List<String>): List<PdfWord> {
        // Detection on the page scaled to fit the detector in one pass; reading from the full page.
        val k = minOf(1f, DET_SIZE.toFloat() / max(page.w, page.h))
        val small = if (k < 1f) PpOcr.resize(page, (page.w * k).toInt().coerceAtLeast(1), (page.h * k).toInt().coerceAtLeast(1)) else page
        val boxes = PpOcr.detect(small, ::detInfer, DET_SIZE).map { it.scaled(1f / k) }
        val (lines, _) = PpOcr.readLines(boxes, { b, s -> readLine(page, b, s) }, hinted, scripts)
        val words = ArrayList<PdfWord>()
        for ((b, r) in lines) {
            val w = b.x1 - b.x0
            r.words.forEachIndexed { i, (text, f0, f1) ->
                words.add(PdfWord(text, b.x0 + f0 * w, b.y0, (f1 - f0) * w, b.y1 - b.y0, last = i == r.words.lastIndex))
            }
        }
        return words
    }

    /** PP-OCR's detector wants normalised BGR, channels first. */
    internal fun detInfer(img: Raster): PpOcr.ProbMap {
        val n = img.w * img.h
        val buf = FloatBuffer.allocate(3 * n)
        for (c in 0 until 3) {
            val shift = c * 8 // B, G, R
            for (i in 0 until n) buf.put((((img.px[i] shr shift) and 0xFF) / 255f - DET_MEAN[c]) / DET_STD[c])
        }
        buf.rewind()
        OnnxTensor.createTensor(env, buf, longArrayOf(1, 3, img.h.toLong(), img.w.toLong())).use { t ->
            det.run(mapOf(det.inputNames.first() to t)).use { out ->
                val o = out.get(0) as OnnxTensor
                val shape = o.info.shape
                val fb = o.floatBuffer
                val p = FloatArray(fb.remaining()).also { fb.get(it) }
                return PpOcr.ProbMap(shape[3].toInt(), shape[2].toInt(), p)
            }
        }
    }

    internal fun readLine(page: Raster, box: PpOcr.TextBox, script: String): PpOcr.RecResult? {
        val session = rec.getOrPut(script) { env.createSession(model("rec_$script.onnx"), options) }
        val charset = charsets.getOrPut(script) { PpOcr.charset(dict(script)) }
        val input = PpOcr.recInput(page, box)
        val s = input.strip
        val n = s.w * s.h
        val buf = FloatBuffer.allocate(3 * n)
        for (c in 0 until 3) {
            val shift = c * 8 // B, G, R; padding stays 0 (mid-grey), as in PaddleOCR
            for (i in 0 until n) {
                val x = i % s.w
                buf.put(if (x < input.content) (((s.px[i] shr shift) and 0xFF) / 255f - 0.5f) / 0.5f else 0f)
            }
        }
        buf.rewind()
        OnnxTensor.createTensor(env, buf, longArrayOf(1, 3, s.h.toLong(), s.w.toLong())).use { t ->
            session.run(mapOf(session.inputNames.first() to t)).use { out ->
                val o = out.get(0) as OnnxTensor
                val shape = o.info.shape // (1, T, C)
                if (shape.size != 3) return null
                val fb = o.floatBuffer
                val probs = FloatArray(fb.remaining()).also { fb.get(it) }
                val r = PpOcr.ctcDecode(probs, shape[1].toInt(), shape[2].toInt(), charset, input.contentFrac)
                return r.takeIf { it.text.isNotEmpty() }
            }
        }
    }

    override fun close() {
        rec.values.forEach { it.close() }
        det.close()
        options.close()
    }

    companion object {
        /** The detector's input: the server's size, so text is found the same way. */
        const val DET_SIZE = 1280
        private val DET_MEAN = floatArrayOf(0.485f, 0.456f, 0.406f)
        private val DET_STD = floatArrayOf(0.229f, 0.224f, 0.225f)
    }
}
