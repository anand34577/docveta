package app.docveta.android

import app.docveta.android.scan.DocumentDetector
import app.docveta.android.scan.PageFinder
import app.docveta.android.scan.PageFilters
import app.docveta.android.scan.PdfPageImage
import app.docveta.android.scan.PdfWord
import app.docveta.android.scan.PdfWriter
import app.docveta.android.scan.Pt
import app.docveta.android.scan.Quad
import app.docveta.android.scan.QuadTracker
import app.docveta.android.scan.Raster
import app.docveta.android.scan.Rectifier
import java.util.Random
import kotlin.math.abs
import kotlin.math.hypot
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/** A "document": white paper with rows of dark text-like marks and (unless told otherwise) a border box. */
private fun paper(w: Int, h: Int, border: Boolean = true): Raster {
    val px = IntArray(w * h) { -1 }
    fun dark(x: Int, y: Int) { px[y * w + x] = 0xFF202020.toInt() }
    // Text lines: short dashes every 14 px.
    var y = 40
    while (y < h - 40) {
        var x = 30
        while (x < w - 60) {
            val len = 8 + (x * 7 + y * 3) % 30
            for (xx in x until minOf(w - 30, x + len)) for (dy in 0..3) dark(xx, y + dy)
            x += len + 8
        }
        y += 18
    }
    if (border) {
        for (x in 8 until w - 8) { dark(x, 8); dark(x, h - 9) }
        for (yy in 8 until h - 8) { dark(8, yy); dark(w - 9, yy) }
    }
    return Raster(w, h, px)
}

/** Photographs [doc] onto a [bg]-coloured table: the page lands on [quad] in a w x h picture, with noise. */
private fun photo(doc: Raster, quad: Quad, w: Int, h: Int, bg: Int, noise: Int = 6, seed: Long = 1, surface: ((Int, Int) -> Int)? = null): Raster {
    val hm = Rectifier.homography(
        doubleArrayOf(quad.tl.x.toDouble(), quad.tl.y.toDouble(), quad.tr.x.toDouble(), quad.tr.y.toDouble(), quad.br.x.toDouble(), quad.br.y.toDouble(), quad.bl.x.toDouble(), quad.bl.y.toDouble()),
        doubleArrayOf(0.0, 0.0, doc.w.toDouble(), 0.0, doc.w.toDouble(), doc.h.toDouble(), 0.0, doc.h.toDouble()),
    )
    val rnd = Random(seed)
    val px = IntArray(w * h)
    for (y in 0 until h) for (x in 0 until w) {
        val d = hm[6] * x + hm[7] * y + 1.0
        val u = (hm[0] * x + hm[1] * y + hm[2]) / d
        val v = (hm[3] * x + hm[4] * y + hm[5]) / d
        var c = surface?.invoke(x, y) ?: bg
        if (u >= 0 && v >= 0 && u < doc.w && v < doc.h) c = doc.px[v.toInt() * doc.w + u.toInt()]
        val n = if (noise > 0) rnd.nextInt(noise * 2 + 1) - noise else 0
        fun ch(shift: Int) = (((c shr shift) and 0xFF) + n).coerceIn(0, 255)
        px[y * w + x] = (0xFF shl 24) or (ch(16) shl 16) or (ch(8) shl 8) or ch(0)
    }
    return Raster(w, h, px)
}

private fun cornersClose(a: Quad, b: Quad, tolerance: Float) = a.points.zip(b.points).all { (p, q) -> p.dist(q) <= tolerance }

class DetectorTest {
    private val doc = paper(420, 594)

    @Test
    fun findsATiltedPageOnADarkTable() {
        val truth = Quad(Pt(150f, 90f), Pt(560f, 130f), Pt(590f, 520f), Pt(120f, 470f))
        val img = photo(doc, truth, 720, 600, 0xFF2A2A2A.toInt())
        val found = DocumentDetector.detect(img.luma(), img.w, img.h)
        assertNotNull("page not found", found)
        assertTrue("corners off: $found vs $truth", cornersClose(found!!, truth, 0.03f * hypot(720f, 600f)))
    }

    @Test
    fun findsADarkPageOnABrightTable() {
        val dark = Raster(doc.w, doc.h, IntArray(doc.px.size) { if (doc.px[it] == -1) 0xFF303030.toInt() else 0xFF909090.toInt() })
        val truth = Quad(Pt(100f, 60f), Pt(600f, 80f), Pt(620f, 540f), Pt(90f, 520f))
        val img = photo(dark, truth, 720, 600, 0xFFE8E8E8.toInt())
        val found = DocumentDetector.detect(img.luma(), img.w, img.h)
        assertNotNull(found)
        assertTrue(cornersClose(found!!, truth, 0.03f * hypot(720f, 600f)))
    }

    @Test
    fun findsAPageThatFillsMostOfTheFrame() {
        val truth = Quad(Pt(20f, 15f), Pt(700f, 25f), Pt(705f, 585f), Pt(15f, 580f))
        val img = photo(doc, truth, 720, 600, 0xFF222222.toInt())
        val found = DocumentDetector.detect(img.luma(), img.w, img.h)
        assertNotNull(found)
        assertTrue(cornersClose(found!!, truth, 0.03f * hypot(720f, 600f)))
    }

    @Test
    fun findsAWhitePageOnANearlyWhiteDesk() {
        // Too little brightness difference for the brightness pass: only the outline gives it away.
        val truth = Quad(Pt(130f, 70f), Pt(590f, 105f), Pt(570f, 560f), Pt(110f, 520f))
        val img = photo(paper(420, 594, border = false), truth, 720, 600, 0xFFEEEEEE.toInt(), noise = 4)
        val found = DocumentDetector.detect(img.luma(), img.w, img.h) // the live camera's size
        assertNotNull("page not found", found)
        assertTrue("corners off: $found vs $truth", cornersClose(found!!, truth, 0.015f * hypot(720f, 600f)))
    }

    @Test
    fun findsAWhitePageWithABorderPrintedCloseToItsEdge() {
        // The ruled border is a stronger line than the paper's own edge; the corners must still go to the paper.
        val truth = Quad(Pt(130f, 70f), Pt(590f, 105f), Pt(570f, 560f), Pt(110f, 520f))
        val img = photo(doc, truth, 720, 600, 0xFFEEEEEE.toInt(), noise = 4)
        val found = DocumentDetector.detect(img.luma(), img.w, img.h, DocumentDetector.PHOTO_EDGE_SIDE)
        assertNotNull("page not found", found)
        assertTrue("corners off: $found vs $truth", cornersClose(found!!, truth, 0.01f * hypot(720f, 600f)))
    }

    @Test
    fun findsAPageOnBusyFabric() {
        // Blotchy cloth as bright as the paper in places, so no clean light/dark split exists.
        val rnd = Random(7)
        val cells = IntArray(120 * 100) { 90 + rnd.nextInt(166) }
        val cloth = { x: Int, y: Int -> val v = cells[(y / 6) * 120 + x / 6]; (0xFF shl 24) or (v shl 16) or (v shl 8) or v }
        val truth = Quad(Pt(160f, 80f), Pt(560f, 120f), Pt(575f, 540f), Pt(135f, 505f))
        val img = photo(doc, truth, 720, 600, 0, surface = cloth)
        val found = DocumentDetector.detect(img.luma(), img.w, img.h, DocumentDetector.PHOTO_EDGE_SIDE)
        assertNotNull("page not found", found)
        assertTrue("corners off: $found vs $truth", cornersClose(found!!, truth, 0.015f * hypot(720f, 600f)))
    }

    @Test
    fun putsTheCornersWithinAFewPixels() {
        val truth = Quad(Pt(150.3f, 90.6f), Pt(560.8f, 130.2f), Pt(590.1f, 520.4f), Pt(120.7f, 470.9f))
        val img = photo(doc, truth, 720, 600, 0xFF2A2A2A.toInt())
        val found = DocumentDetector.detect(img.luma(), img.w, img.h, DocumentDetector.PHOTO_EDGE_SIDE)!!
        val worst = found.points.zip(truth.points).maxOf { (p, q) -> p.dist(q) }
        assertTrue("worst corner $worst px off: $found", worst < 4f)
    }

    @Test
    fun givesUpOnAFeaturelessPicture() {
        val rnd = Random(3)
        val luma = ByteArray(640 * 480) { (120 + rnd.nextInt(9)).toByte() }
        assertNull(DocumentDetector.detect(luma, 640, 480))
    }

    @Test
    fun givesUpWhenTheWholeFrameIsPaper() {
        val img = photo(doc, Quad.full(720f, 600f), 720, 600, 0xFFFFFFFF.toInt())
        assertNull(DocumentDetector.detect(img.luma(), img.w, img.h))
    }

    /** Where the corners of a w x h sheet land when a pinhole camera (focal length f, looking at the picture centre) sees it tilted. */
    private fun projected(w: Double, h: Double, ax: Double, ay: Double, z: Double, f: Double, cx: Double, cy: Double): Quad {
        fun pt(x: Double, y: Double): Pt {
            val y1 = y * Math.cos(ax)
            val z1 = y * Math.sin(ax)
            val x2 = x * Math.cos(ay) + z1 * Math.sin(ay)
            val z2 = -x * Math.sin(ay) + z1 * Math.cos(ay)
            val zz = z + z2
            return Pt((cx + f * x2 / zz).toFloat(), (cy + f * y1 / zz).toFloat())
        }
        return Quad(pt(-w / 2, -h / 2), pt(w / 2, -h / 2), pt(w / 2, h / 2), pt(-w / 2, h / 2))
    }

    @Test
    fun recoversTheTrueProportionsOfATiltedPage() {
        val q = projected(0.70, 1.0, Math.toRadians(-50.0), Math.toRadians(8.0), 1.6, 800.0, 360.0, 300.0)
        // The quad itself looks nothing like 0.7:1 ...
        val edges = ((q.tl.dist(q.tr) + q.bl.dist(q.br)) / 2) / ((q.tl.dist(q.bl) + q.tr.dist(q.br)) / 2)
        assertTrue("edges ratio $edges", abs(edges - 0.7f) > 0.08f)
        // ... but the page's real shape comes back.
        val a = Rectifier.estimateAspect(q, 360.0, 300.0)
        assertNotNull(a)
        assertEquals(0.70, a!!, 0.02)
    }

    @Test
    fun flattensAPerspectivePhotoBackToThePage() {
        val truth = projected(0.70, 1.0, Math.toRadians(-20.0), Math.toRadians(15.0), 1.7, 800.0, 360.0, 300.0)
        val img = photo(doc, truth, 720, 600, 0xFF2A2A2A.toInt(), noise = 0)
        val found = DocumentDetector.detect(img.luma(), img.w, img.h)!!
        val flat = Rectifier.warp(img, found, maxSide = 700)
        // Right proportions (the paper is 420 x 594).
        assertEquals(420f / 594f, flat.w.toFloat() / flat.h, 0.05f)
        // And the right content: compare the middle of the flattened page with the original at the same size.
        val rw = flat.w
        val rh = flat.h
        val scaled = Rectifier.warp(doc, Quad.full(doc.w.toFloat(), doc.h.toFloat()), maxSide = maxOf(rw, rh))
        // Compare coarse brightness (12 px blocks), which tolerates a pixel or two of misalignment.
        val block = 12
        var diff = 0.0
        var n = 0
        for (by in rh / 8 / block until rh * 7 / 8 / block) for (bx in rw / 8 / block until rw * 7 / 8 / block) {
            fun mean(r: Raster, x0: Int, y0: Int, bw: Int, bh: Int): Double {
                var t = 0L
                var c = 0
                for (y in y0 until minOf(r.h, y0 + bh)) for (x in x0 until minOf(r.w, x0 + bw)) { t += r.px[y * r.w + x] and 0xFF; c++ }
                return t.toDouble() / maxOf(1, c)
            }
            val a = mean(flat, bx * block, by * block, block, block)
            val b = mean(scaled, bx * block * scaled.w / rw, by * block * scaled.h / rh, block * scaled.w / rw, block * scaled.h / rh)
            diff += abs(a - b)
            n++
        }
        val mean = diff / n
        assertTrue("flattened page differs too much (mean $mean)", mean < 14.0)
    }
}

/** The DocAligner model (assets/scan) on ONNX Runtime for the desktop, through the same path as the app. */
class PageFinderTest {
    private val finder = PageFinder(java.io.File("src/main/assets/scan/docaligner.onnx").readBytes())

    private fun find(img: Raster) = PageFinder.page(finder, img)

    /** A real camera frame, as a gzipped binary PPM (P6) in the test resources. */
    private fun frame(name: String): Raster {
        val bytes = java.util.zip.GZIPInputStream(javaClass.getResourceAsStream("/$name.ppm.gz")!!).readBytes()
        val header = String(bytes, 0, 32, Charsets.US_ASCII).split(Regex("\\s+"))
        val w = header[1].toInt()
        val h = header[2].toInt()
        val start = bytes.size - 3 * w * h
        fun at(i: Int) = bytes[start + i].toInt() and 0xFF
        return Raster(w, h, IntArray(w * h) { (0xFF shl 24) or (at(3 * it) shl 16) or (at(3 * it + 1) shl 8) or at(3 * it + 2) })
    }

    /** Random tilted pages on light, wooden and busy surfaces: worst-corner error, as a share of the picture's diagonal. */
    private fun scenes(n: Int, seed: Long): List<Pair<Raster, Quad>> {
        val rnd = Random(seed)
        val plain = paper(420, 594, border = false)
        return List(n) { i ->
            fun j() = rnd.nextFloat() * 110f - 55f
            val truth = Quad(Pt(150f + j(), 70f + j()), Pt(570f + j(), 80f + j()), Pt(590f + j(), 540f + j()), Pt(130f + j(), 530f + j()))
            val img = when (i % 3) {
                0 -> photo(plain, truth, 720, 600, 0xFFE0E0E0.toInt(), noise = 4, seed = i.toLong())
                1 -> photo(doc, truth, 720, 600, 0, seed = i.toLong()) { x, _ -> val v = (150 + 30 * kotlin.math.sin(x / 6.0)).toInt(); (0xFF shl 24) or (v shl 16) or ((v * 3 / 4) shl 8) or (v / 2) }
                else -> { val cells = IntArray(130 * 110) { 60 + rnd.nextInt(196) }; photo(plain, truth, 720, 600, 0, seed = i.toLong()) { x, y -> val v = cells[(y / 6) * 130 + x / 6]; (0xFF shl 24) or (v shl 16) or (v shl 8) or v } }
            }
            img to truth
        }
    }

    private fun worst(found: Quad?, truth: Quad) = found?.points?.zip(truth.points)?.maxOf { (p, q) -> p.dist(q) } ?: Float.MAX_VALUE

    private val doc = paper(420, 594)

    @Test
    fun findsPagesTheEdgesAloneMiss() {
        // Edges and brightness alone miss about a third of these (20 of 60 when this was written).
        val e = scenes(60, 11).map { (img, t) -> worst(find(img), t) }.sorted()
        assertTrue("missed ${e.count { it > 15f }} of 60: $e", e.count { it > 15f } <= 2)
        assertTrue("median worst corner ${e[30]} px", e[30] < 3f)
    }

    @Test
    fun trustsTheModelWhenItSeesNoPage() {
        // A blurry surface: edges alone outline a "page" in it.
        val img = frame("blur-no-page")
        assertNotNull(DocumentDetector.detect(img.luma(), img.w, img.h))
        assertNull(find(img))
    }

    @Test
    fun givesUpWithoutAPage() {
        val rnd = Random(3)
        val flat = Raster(640, 480, IntArray(640 * 480) { val v = 120 + rnd.nextInt(9); (0xFF shl 24) or (v shl 16) or (v shl 8) or v })
        assertNull(find(flat))
    }

    @Test
    fun findsAPageThatFillsTheWholeFrame() {
        val q = find(photo(doc, Quad.full(720f, 600f), 720, 600, 0xFFFFFFFF.toInt()))!!
        assertTrue("$q", q.differenceTo(Quad.full(720f, 600f)) < 0.04f)
    }

    @Test
    fun resizeKeepsFlatColoursAndAveragesDetail() {
        val grey = PageFinder.resize(Raster(1000, 700, IntArray(1000 * 700) { 0xFF336699.toInt() }), 256, 256)
        assertTrue(grey.all { it == 0xFF336699.toInt() })
        // Single-pixel stripes shrunk 4x come out as their average.
        val stripes = PageFinder.resize(Raster(1024, 1024, IntArray(1024 * 1024) { if (it % 2 == 0) -1 else 0xFF000000.toInt() }), 256, 256)
        assertEquals(128.0, stripes.map { it and 0xFF }.average(), 2.0)
    }
}

class QuadTest {
    @Test
    fun ordersCornersWhateverTheInputOrder() {
        val q = Quad.ordered(listOf(Pt(90f, 80f), Pt(10f, 12f), Pt(95f, 10f), Pt(8f, 85f)))
        assertEquals(Pt(10f, 12f), q.tl)
        assertEquals(Pt(95f, 10f), q.tr)
        assertEquals(Pt(90f, 80f), q.br)
        assertEquals(Pt(8f, 85f), q.bl)
    }

    @Test
    fun spotsABowTie() {
        assertTrue(Quad(Pt(0f, 0f), Pt(10f, 0f), Pt(10f, 10f), Pt(0f, 10f)).isConvex())
        assertFalse(Quad(Pt(0f, 0f), Pt(10f, 10f), Pt(10f, 0f), Pt(0f, 10f)).isConvex())
    }

    @Test
    fun areaOfARectangle() {
        assertEquals(200f, Quad(Pt(0f, 0f), Pt(20f, 0f), Pt(20f, 10f), Pt(0f, 10f)).area(), 0.01f)
    }

    @Test
    fun trackerReportsSteadyOnlyAfterTheQuadHoldsStill() {
        val t = QuadTracker(stillFrames = 4)
        val q = Quad(Pt(10f, 10f), Pt(110f, 12f), Pt(112f, 150f), Pt(8f, 148f))
        repeat(3) { t.update(q) }
        assertFalse(t.isSteady)
        repeat(6) { t.update(q) }
        assertTrue(t.isSteady)
        // Moving the page far away resets it.
        t.update(Quad(Pt(60f, 60f), Pt(160f, 62f), Pt(162f, 200f), Pt(58f, 198f)))
        assertFalse(t.isSteady)
        // Losing the page for a while forgets it.
        repeat(5) { t.update(null) }
        assertNull(t.quad)
    }
}

class FilterTest {
    @Test
    fun enhanceEvensOutShadowsAndWhitensThePaper() {
        val w = 320
        val h = 400
        // Paper lit from the left: brightness falls from 250 to 120 across the page, with a dark text bar.
        val px = IntArray(w * h) { i ->
            val x = i % w
            val y = i / w
            val b = 250 - x * 130 / w
            val ink = y in 190..200 && x in 40..280
            val v = if (ink) b / 5 else b
            (0xFF shl 24) or (v shl 16) or (v shl 8) or v
        }
        val out = PageFilters.enhance(Raster(w, h, px))
        fun at(x: Int, y: Int) = out.px[y * w + x] and 0xFF
        // Paper is white at both ends, despite the shadow.
        assertTrue(at(10, 50) > 235)
        assertTrue(at(w - 10, 50) > 235)
        // Ink stays dark.
        assertTrue(at(160, 195) < 90)
    }

    @Test
    fun enhanceKeepsTheColourOfALargePrintedArea() {
        // A magazine cover: orange over most of it, a white margin and a black strip at the bottom.
        val w = 400
        val h = 520
        val orange = 0xFFDC8C28.toInt()
        val px = IntArray(w * h) { i ->
            val x = i % w
            val y = i / w
            when {
                y >= 460 -> 0xFF181818.toInt()
                x < 20 || x >= w - 20 || y < 20 -> 0xFFE6E6E6.toInt()
                else -> orange
            }
        }
        val out = PageFilters.enhance(Raster(w, h, px))
        fun ch(p: Int, s: Int) = (p shr s) and 0xFF
        val o = out.px[250 * w + 200]
        // Orange stays orange: not lifted towards white (this used to give about 255, 255, 255).
        for (s in listOf(16, 8, 0)) assertEquals("channel $s: ${Integer.toHexString(o)}", ch(orange, s).toFloat(), ch(o, s).toFloat(), 30f)
        assertTrue(ch(out.px[490 * w + 200], 16) < 30) // the strip stays black
        assertTrue(ch(out.px[10 * w + 200], 16) > 240) // the margin is white
    }

    @Test
    fun blackAndWhiteHasOnlyTwoColours() {
        val w = 200
        val h = 200
        val px = IntArray(w * h) { i -> val v = if ((i % w) in 50..60) 40 else 200; (0xFF shl 24) or (v shl 16) or (v shl 8) or v }
        val out = PageFilters.blackWhite(Raster(w, h, px))
        assertTrue(out.px.all { it == -1 || it == 0xFF000000.toInt() })
        assertEquals(0xFF000000.toInt(), out.px[100 * w + 55])
        assertEquals(-1, out.px[100 * w + 150])
    }

    @Test
    fun fourQuarterTurnsAreTheIdentity() {
        val r = Raster(3, 2, intArrayOf(1, 2, 3, 4, 5, 6))
        var t = r
        repeat(4) { t = PageFilters.rotate(t, 1) }
        assertEquals(r.px.toList(), t.px.toList())
        // One turn clockwise: the left column becomes the top row, read bottom to top.
        val once = PageFilters.rotate(r, 1)
        assertEquals(2, once.w)
        assertEquals(3, once.h)
        assertEquals(listOf(4, 1, 5, 2, 6, 3), once.px.toList())
    }
}

class PdfWriterTest {
    private val jpeg = byteArrayOf(0xFF.toByte(), 0xD8.toByte(), 1, 2, 3, 4, 0xFF.toByte(), 0xD9.toByte())

    @Test
    fun writesAWellFormedPdfWithOnePagePerImage() {
        val bytes = PdfWriter.toBytes(listOf(PdfPageImage(jpeg, 1654, 2339), PdfPageImage(jpeg, 800, 600, gray = true)), "Rent (June)")
        val text = String(bytes, Charsets.ISO_8859_1)
        assertTrue(text.startsWith("%PDF-1.4"))
        assertTrue(text.trimEnd().endsWith("%%EOF"))
        assertTrue(text.contains("/Count 2"))
        assertTrue(text.contains("/DeviceGray"))
        assertTrue(text.contains("/Title (Rent \\(June\\))"))
        // Every cross-reference entry points at its "N 0 obj".
        val startxref = Regex("startxref\\n(\\d+)").find(text)!!.groupValues[1].toInt()
        assertTrue(text.substring(startxref).startsWith("xref"))
        val entries = Regex("(\\d{10}) 00000 n").findAll(text.substring(startxref)).map { it.groupValues[1].toInt() }.toList()
        assertEquals(1 + 2 + 3 * 2, entries.size)
        entries.forEachIndexed { i, off -> assertTrue("object ${i + 1} at $off", text.substring(off).startsWith("${i + 1} 0 obj")) }
        // The JPEG bytes are stored untouched.
        val needle = String(jpeg, Charsets.ISO_8859_1)
        assertTrue(text.contains(needle))
        assertFalse(text.contains("/Font")) // no words read: no text layer
    }

    @Test
    fun storesLosslessPagesAsFlateAtTheSmallestDepth() {
        fun inflate(b: ByteArray) = java.util.zip.InflaterInputStream(b.inputStream()).readBytes()
        val k = 0xFF shl 24
        // Black and white: 1 bit per pixel, white = 1, rows padded to a byte.
        val bw = PdfWriter.lossless(Raster(9, 2, IntArray(18) { if (it % 2 == 0) -1 else k }))
        assertEquals(1, bw.bits)
        assertTrue(bw.gray && bw.flate)
        assertEquals(listOf(0xAA, 0x80, 0x55, 0x00), inflate(bw.data).map { it.toInt() and 0xFF })
        // Gray: 8 bits, each row "Up" (2) then the difference from the row above.
        val gray = PdfWriter.lossless(Raster(2, 2, intArrayOf(k or 0x101010, k or 0x202020, k or 0x151515, k or 0x202020)))
        assertEquals(8, gray.bits)
        assertTrue(gray.gray)
        assertEquals(listOf(2, 0x10, 0x20, 2, 0x05, 0x00), inflate(gray.data).map { it.toInt() and 0xFF })
        // Colour: RGB, and the PDF says how to undo the predictor.
        val rgb = PdfWriter.lossless(Raster(1, 1, intArrayOf(k or 0x102030)))
        assertFalse(rgb.gray)
        assertEquals(listOf(2, 0x10, 0x20, 0x30), inflate(rgb.data).map { it.toInt() and 0xFF })
        val text = String(PdfWriter.toBytes(listOf(rgb, bw)), Charsets.ISO_8859_1)
        assertTrue(text.contains("/DeviceRGB /BitsPerComponent 8 /Filter /FlateDecode /DecodeParms << /Predictor 15 /Colors 3 /BitsPerComponent 8 /Columns 1 >>"))
        assertTrue(text.contains("/DeviceGray /BitsPerComponent 1 /Filter /FlateDecode /Length"))
        // Kept for checking with other PDF readers: a colour gradient page and a black and white page.
        val grad = PdfWriter.lossless(Raster(64, 48, IntArray(64 * 48) { k or ((it % 64) * 4 shl 16) or ((it / 64) * 5 shl 8) or 0x80 }))
        val stripes = PdfWriter.lossless(Raster(50, 40, IntArray(50 * 40) { if ((it % 50) / 5 % 2 == 0) -1 else k }))
        java.io.File("build/test-pdf").mkdirs()
        java.io.File("build/test-pdf/lossless.pdf").writeBytes(PdfWriter.toBytes(listOf(grad, stripes)))
    }

    @Test
    fun putsWordsReadOnThePhoneUnderThePictureAsInvisibleText() {
        val words = listOf(
            PdfWord("Amount", 100f, 165f, 150f, 45f), PdfWord("due", 265f, 165f, 70f, 45f), PdfWord("1,842.50", 350f, 165f, 170f, 45f, last = true),
            PdfWord("परीक्षा", 100f, 300f, 200f, 60f), PdfWord("पुस्तिका", 320f, 300f, 220f, 60f, last = true),
        )
        val bytes = PdfWriter.toBytes(listOf(PdfPageImage(jpeg, 1240, 1754, words = words)), "Bill")
        val text = String(bytes, Charsets.ISO_8859_1)
        assertTrue(text.contains(" 3 Tr")) // invisible
        assertTrue(text.contains("/ToUnicode"))
        assertTrue(text.contains("<" + "परीक्षा ".map { String.format("%04X", it.code) }.joinToString("") + ">")) // Hindi as UTF-16, with the space after the word
        val startxref = Regex("startxref\\n(\\d+)").find(text)!!.groupValues[1].toInt()
        val entries = Regex("(\\d{10}) 00000 n").findAll(text.substring(startxref)).map { it.groupValues[1].toInt() }.toList()
        assertEquals(3 + 3 + 4, entries.size)
        entries.forEachIndexed { i, off -> assertTrue("object ${i + 1} at $off", text.substring(off).startsWith("${i + 1} 0 obj")) }
        // Kept for checking with other PDF readers (pdf.js, PDFium).
        java.io.File("build/test-pdf").mkdirs()
        java.io.File("build/test-pdf/text-layer.pdf").writeBytes(bytes)
    }
}
