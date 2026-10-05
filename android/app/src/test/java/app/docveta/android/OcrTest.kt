package app.docveta.android

import app.docveta.android.scan.OnnxOcr
import app.docveta.android.scan.PpOcr
import app.docveta.android.scan.Pt
import app.docveta.android.scan.Quad
import app.docveta.android.scan.Raster
import java.io.File
import java.util.zip.GZIPInputStream
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Assume.assumeTrue
import org.junit.Test

/** The phone's port of the server's PP-OCR steps: the same cases as workers/sdk-python/tests/test_ppocr.py. */
class PpOcrTest {
    private fun map(w: Int, h: Int, vararg regions: Pair<IntArray, Float>): PpOcr.ProbMap {
        val p = FloatArray(w * h)
        for ((r, v) in regions) for (y in r[1] until r[3]) for (x in r[0] until r[2]) p[y * w + x] = v
        return PpOcr.ProbMap(w, h, p)
    }

    @Test
    fun dbPostprocessFindsRectangles() {
        val m = map(400, 200, intArrayOf(40, 50, 300, 70) to 0.95f, intArrayOf(60, 120, 160, 135) to 0.9f)
        val boxes = PpOcr.sortBoxes(PpOcr.dbPostprocess(m))
        assertEquals(2, boxes.size)
        val b = boxes[0]
        // Unclipping grows the shrunk DB region a little in every direction.
        assertTrue(b.x0 < 40 && b.x1 > 299 && b.y0 < 50 && b.y1 > 69)
        assertTrue(boxes[0].y0 < boxes[1].y0)
    }

    @Test
    fun lowScoreRegionsAreDropped() {
        assertTrue(PpOcr.dbPostprocess(map(100, 100, intArrayOf(10, 10, 80, 30) to 0.4f)).isEmpty())
    }

    @Test
    fun aTiltedLineGetsATiltedBox() {
        val w = 300
        val h = 200
        val p = FloatArray(w * h)
        // A 200 x 16 band rotated by ~10°.
        for (y in 0 until h) for (x in 0 until w) {
            val u = (x - 150) * 0.985f + (y - 100) * 0.174f
            val v = -(x - 150) * 0.174f + (y - 100) * 0.985f
            if (kotlin.math.abs(u) < 100 && kotlin.math.abs(v) < 8) p[y * w + x] = 0.9f
        }
        val b = PpOcr.dbPostprocess(PpOcr.ProbMap(w, h, p)).single()
        val slope = (b.quad.tr.y - b.quad.tl.y) / (b.quad.tr.x - b.quad.tl.x)
        assertEquals(0.176f, slope, 0.03f)
    }

    @Test
    fun tiledDetectionMapsBackToPageCoordinates() {
        val size = 320
        val page = Raster(900, 1200, IntArray(900 * 1200) { -1 })
        val calls = ArrayList<Pair<Int, Int>>()
        val boxes = PpOcr.detect(page, { img ->
            calls.add(img.w to img.h)
            // Every tile has one line at the same place in the tile.
            map(size, size, intArrayOf(40, 100, 200, 120) to 0.95f)
        }, size)
        assertTrue(calls.size > 1)
        assertTrue(calls.all { it == size to size })
        assertTrue(boxes.isNotEmpty())
        for (b in boxes) assertTrue(b.x0 >= 0 && b.x1 <= 900 && b.y1 <= 1200)
    }

    @Test
    fun aSmallPageIsDetectedInOnePassWithSidesInMultiplesOf32() {
        val page = Raster(600, 400, IntArray(600 * 400) { -1 })
        val calls = ArrayList<Pair<Int, Int>>()
        PpOcr.detect(page, { img -> calls.add(img.w to img.h); PpOcr.ProbMap(img.w, img.h, FloatArray(img.w * img.h)) }, 640)
        assertEquals(listOf(640 to 448), calls) // 600 x 400 scaled to 640 x 427, padded to 448
    }

    @Test
    fun mergeJoinsFragmentsOfOneLine() {
        fun box(x0: Float, y0: Float, x1: Float, y1: Float) = PpOcr.TextBox(Quad(Pt(x0, y0), Pt(x1, y0), Pt(x1, y1), Pt(x0, y1)), 0.9f)
        assertEquals(2, PpOcr.mergeBoxes(listOf(box(0f, 0f, 100f, 20f), box(105f, 1f, 200f, 21f), box(0f, 100f, 50f, 120f))).size)
    }

    @Test
    fun recInputIsPaddedToMultiplesOf160() {
        val page = Raster(400, 100, IntArray(400 * 100) { -1 })
        val box = PpOcr.TextBox(Quad(Pt(0f, 0f), Pt(300f, 0f), Pt(300f, 30f), Pt(0f, 30f)), 0.9f)
        val r = PpOcr.recInput(page, box)
        assertEquals(48, r.strip.h)
        assertEquals(480, r.content) // 300 x 30 at height 48
        assertEquals(480, r.strip.w)
    }

    @Test
    fun verticalTextIsTurnedToReadAcross() {
        val page = Raster(200, 200, IntArray(200 * 200) { -1 })
        val r = PpOcr.recInput(page, PpOcr.TextBox(Quad(Pt(10f, 10f), Pt(30f, 10f), Pt(30f, 150f), Pt(10f, 150f)), 0.9f))
        assertTrue(r.content > 48 * 3)
    }

    @Test
    fun ctcDecodeWithWordPositions() {
        val charset = listOf("a", "b", "c", " ")
        val seq = intArrayOf(1, 1, 0, 2, 0, 4, 0, 3, 3, 0) // "ab c"
        val probs = FloatArray(seq.size * 5) { 0.01f }
        seq.forEachIndexed { t, k -> probs[t * 5 + k] = 0.9f }
        val r = PpOcr.ctcDecode(probs, seq.size, 5, charset)
        assertEquals("ab c", r.text)
        assertEquals(0.9f, r.confidence, 1e-4f)
        assertEquals(listOf("ab", "c"), r.words.map { it.first })
        assertTrue(r.words[0].third <= r.words[1].second + 1e-6f)
    }

    @Test
    fun charsetAddsTheSpace() {
        assertEquals(listOf("a", "b", " "), PpOcr.charset("a\r\nb\n"))
    }

    private fun box(i: Int, width: Int = 300) = (10f + i * 40).let { y -> PpOcr.TextBox(Quad(Pt(10f, y), Pt(10f + width, y), Pt(10f + width, y + 30), Pt(10f, y + 30)), 0.9f) }

    /** truth[i] = (script the line is written in, its text): the right script reads it at 0.95, any other guesses at 0.3. */
    private fun reader(truth: Map<Int, Pair<String, String>>, calls: MutableList<Pair<Int, String>>) = { b: PpOcr.TextBox, script: String ->
        val i = ((b.y0 - 10) / 40).toInt()
        calls.add(i to script)
        val (real, text) = truth.getValue(i)
        PpOcr.RecResult(if (script == real) text else "x?", if (script == real) 0.95f else 0.3f, emptyList())
    }

    @Test
    fun aHindiPageIsReadAsHindiEvenWithAnEnglishHint() {
        val truth = (0 until 5).associateWith { "devanagari" to "पंक्ति $it" }
        val (found, script) = PpOcr.readLines((0 until 5).map { box(it) }, reader(truth, ArrayList()), listOf("en"), listOf("devanagari", "en", "ta"))
        assertEquals("devanagari", script)
        assertEquals(truth.values.map { it.second }, found.map { it.second.text })
    }

    @Test
    fun anEnglishPageNeverRunsTheOtherModels() {
        val calls = ArrayList<Pair<Int, String>>()
        val truth = (0 until 20).associateWith { "en" to "line $it" }
        val (found, script) = PpOcr.readLines((0 until 20).map { box(it) }, reader(truth, calls), listOf("en"), listOf("devanagari", "en", "ta"))
        assertEquals("en", script)
        assertEquals(20, found.size)
        assertEquals(setOf("en"), calls.map { it.second }.toSet())
    }

    @Test
    fun aMixedPageReadsEachLineInItsScript() {
        val truth = (0 until 10).associateWith { "en" to "line $it" }.toMutableMap()
        truth[3] = "devanagari" to "नाम"
        truth[7] = "devanagari" to "पता"
        val (found, script) = PpOcr.readLines((0 until 10).map { box(it) }, reader(truth, ArrayList()), listOf("en"), listOf("devanagari", "en"))
        assertEquals("en", script)
        assertEquals(truth.values.map { it.second }, found.map { it.second.text })
    }

    @Test
    fun aHindiLineTheEnglishReaderGarblesConfidentlyIsReadAgain() {
        // The real English model reads "भारत सरकार" as "HRd HRQR" at 0.76: still worth a second look.
        val read = { b: PpOcr.TextBox, s: String ->
            val i = ((b.y0 - 10) / 40).toInt()
            if (i == 4) PpOcr.RecResult(if (s == "en") "HRd HRQR" else "भारत सरकार", if (s == "en") 0.76f else 0.99f, emptyList())
            else PpOcr.RecResult("line $i", if (s == "en") 0.99f else 0.9f, emptyList())
        }
        val (found, script) = PpOcr.readLines((0 until 5).map { box(it) }, read, listOf("en"), listOf("en", "devanagari"))
        assertEquals("en", script)
        assertEquals("भारत सरकार", found[4].second.text)
    }

    @Test
    fun noiseIsNotTurnedIntoAnotherScript() {
        val (found, _) = PpOcr.readLines(listOf(box(0)), { _, s -> PpOcr.RecResult("~~", if (s == "en") 0.55f else 0.6f, emptyList()) }, listOf("en"), listOf("devanagari", "en"))
        assertEquals(listOf("~~"), found.map { it.second.text })
    }

    @Test
    fun aMissingHintedScriptFallsBackToWhatTheresThere() {
        assertEquals(listOf("en", "devanagari"), PpOcr.scriptOrder(listOf("ta"), listOf("en", "devanagari")))
        assertEquals("ka", PpOcr.scriptOf("kn-IN"))
    }
}

/**
 * The real models on ONNX Runtime: reads a rendered page (test resource ocr-page.pgm.gz: three
 * English lines and "भारत सरकार"). Needs the models (workers/onnx/models, or DOCVETA_OCR_MODELS);
 * skipped without them.
 */
class OnnxOcrTest {
    private val dir = File(System.getenv("DOCVETA_OCR_MODELS") ?: "../../workers/onnx/models")

    /** A binary greyscale PGM (P5), gzipped. */
    private fun page(): Raster {
        val bytes = GZIPInputStream(javaClass.getResourceAsStream("/ocr-page.pgm.gz")!!).readBytes()
        val header = String(bytes, 0, 32, Charsets.US_ASCII).split(Regex("\\s+"))
        val w = header[1].toInt()
        val h = header[2].toInt()
        val start = bytes.size - w * h
        return Raster(w, h, IntArray(w * h) { val v = bytes[start + it].toInt() and 0xFF; (0xFF shl 24) or (v shl 16) or (v shl 8) or v })
    }

    @Test
    fun readsAPrintedPage() {
        assumeTrue("no OCR models in $dir", File(dir, "det.onnx").isFile && File(dir, "rec_en.onnx").isFile && File(dir, "rec_devanagari.onnx").isFile)
        OnnxOcr({ File(dir, it).readBytes() }, { File(dir, "dict_$it.txt").readText() }, listOf("en", "devanagari")).use { ocr ->
            val p = page()
            val t0 = System.nanoTime()
            val words = ocr.readPage(p, listOf("en"))
            val text = words.joinToString(" ") { it.text }
            println("read ${words.size} words in ${(System.nanoTime() - t0) / 1_000_000} ms: $text")
            for (w in listOf("Invoice", "number", "4821", "Amount", "1,842.50", "Please", "March", "2026")) assertTrue("'$w' not in: $text", w in text)
            // The Hindi line is found although the hint said English.
            assertTrue("Hindi not read: $text", "भारत" in text && "सरकार" in text)
            // Words sit where they were drawn: "Invoice" from x=150 on the first line (y 120-170).
            val inv = words.first { it.text.startsWith("Invoice") }
            assertEquals(150f, inv.x, 20f)
            assertTrue("y ${inv.y}", inv.y in 100f..140f)
            assertTrue(inv.w in 120f..220f)
        }
    }
}
