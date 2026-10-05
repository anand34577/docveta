package app.docveta.android.scan

import kotlin.math.ceil
import kotlin.math.floor
import kotlin.math.max
import kotlin.math.min
import kotlin.math.roundToInt

/**
 * PP-OCR text detection (DB) and recognition (CTC) pre/post-processing: the same steps as the
 * server's OCR engine (workers/sdk-python/docveta_worker/ppocr.py), so the phone reads a page the
 * way the server would. Plain Kotlin, unit-tested on the JVM; the model calls are passed in.
 */
object PpOcr {
    const val REC_HEIGHT = 48
    const val REC_MAX_WIDTH = 3200

    /** A text line on the page: corners tl, tr, br, bl in page pixels. */
    class TextBox(val quad: Quad, val score: Float) {
        val x0 get() = quad.points.minOf { it.x }
        val y0 get() = quad.points.minOf { it.y }
        val x1 get() = quad.points.maxOf { it.x }
        val y1 get() = quad.points.maxOf { it.y }
        fun scaled(k: Float) = TextBox(quad.scaled(k, k), score)
    }

    /** One read line. [words] are (word, start, end) as fractions of the box width, from the CTC timesteps. */
    class RecResult(val text: String, val confidence: Float, val words: List<Triple<String, Float, Float>>)

    /** A probability map: [w] x [h] floats, row by row. */
    class ProbMap(val w: Int, val h: Int, val p: FloatArray)

    // ------------------------------------------------------------ detection

    /**
     * Text boxes in a DB probability map, in map coordinates: regions above [thresh], kept when
     * their mean probability reaches [boxThresh], as rotated rectangles grown back by [unclipRatio]
     * (DB shrinks text regions in training).
     */
    fun dbPostprocess(map: ProbMap, thresh: Float = 0.3f, boxThresh: Float = 0.6f, unclipRatio: Float = 1.6f, minSize: Float = 3f, maxCandidates: Int = 1500): List<TextBox> {
        val w = map.w
        val h = map.h
        val on = BooleanArray(w * h) { map.p[it] > thresh }
        val seen = BooleanArray(w * h)
        val stack = IntArray(w * h)
        val out = ArrayList<TextBox>()
        var found = 0
        for (start in on.indices) {
            if (!on[start] || seen[start]) continue
            if (++found > maxCandidates) break
            // One region (8-connected), with each row's leftmost and rightmost pixel for its outline.
            var sp = 0
            stack[sp++] = start
            seen[start] = true
            var n = 0
            var sum = 0.0
            val rowMin = HashMap<Int, Int>()
            val rowMax = HashMap<Int, Int>()
            while (sp > 0) {
                val i = stack[--sp]
                val x = i % w
                val y = i / w
                n++
                sum += map.p[i]
                rowMin[y] = min(rowMin[y] ?: x, x)
                rowMax[y] = max(rowMax[y] ?: x, x)
                for (dy in -1..1) for (dx in -1..1) {
                    val nx = x + dx
                    val ny = y + dy
                    if (nx < 0 || ny < 0 || nx >= w || ny >= h) continue
                    val j = ny * w + nx
                    if (on[j] && !seen[j]) {
                        seen[j] = true
                        stack[sp++] = j
                    }
                }
            }
            if (n < 4) continue
            val outline = ArrayList<Pt>(rowMin.size * 2)
            for ((y, x) in rowMin) outline.add(Pt(x.toFloat(), y.toFloat()))
            for ((y, x) in rowMax) outline.add(Pt(x.toFloat(), y.toFloat()))
            val rect = minAreaRect(outline) ?: continue
            if (min(rect.bw, rect.bh) < minSize) continue
            val score = (sum / n).toFloat()
            if (score < boxThresh) continue
            val dist = rect.bw * rect.bh * unclipRatio / max(2 * (rect.bw + rect.bh), 1e-6f)
            val grown = rect.grown(dist)
            if (min(grown.bw, grown.bh) < minSize + 2) continue
            val q = Quad.ordered(grown.corners().map { Pt(it.x.coerceIn(0f, (w - 1).toFloat()), it.y.coerceIn(0f, (h - 1).toFloat())) })
            out.add(TextBox(q, score))
        }
        return out
    }

    /** A rotated rectangle: centre, unit axis [u] (and the perpendicular), extents along each. */
    internal class RotRect(val c: Pt, val u: Pt, val bw: Float, val bh: Float) {
        fun grown(d: Float) = RotRect(c, u, bw + 2 * d, bh + 2 * d)
        fun corners(): List<Pt> {
            val v = Pt(-u.y, u.x)
            val a = u * (bw / 2)
            val b = v * (bh / 2)
            return listOf(c - a - b, c + a - b, c + a + b, c - a + b)
        }
    }

    /** Smallest rectangle around the points, at any angle (rotating calipers over the convex hull). */
    internal fun minAreaRect(points: List<Pt>): RotRect? {
        val hull = convexHull(points.distinct())
        if (hull.isEmpty()) return null
        if (hull.size < 3) {
            val a = hull.first()
            val b = hull.last()
            val len = a.dist(b)
            val u = if (len > 0) Pt((b.x - a.x) / len, (b.y - a.y) / len) else Pt(1f, 0f)
            return RotRect(Pt((a.x + b.x) / 2, (a.y + b.y) / 2), u, len, 0f)
        }
        var best: RotRect? = null
        var bestArea = Float.MAX_VALUE
        for (i in hull.indices) {
            val a = hull[i]
            val b = hull[(i + 1) % hull.size]
            val len = a.dist(b)
            if (len == 0f) continue
            val u = Pt((b.x - a.x) / len, (b.y - a.y) / len)
            val v = Pt(-u.y, u.x)
            var minU = Float.MAX_VALUE
            var maxU = -Float.MAX_VALUE
            var minV = Float.MAX_VALUE
            var maxV = -Float.MAX_VALUE
            for (p in hull) {
                val pu = p.x * u.x + p.y * u.y
                val pv = p.x * v.x + p.y * v.y
                minU = min(minU, pu); maxU = max(maxU, pu)
                minV = min(minV, pv); maxV = max(maxV, pv)
            }
            val area = (maxU - minU) * (maxV - minV)
            if (area < bestArea) {
                bestArea = area
                val cu = (minU + maxU) / 2
                val cv = (minV + maxV) / 2
                best = RotRect(Pt(u.x * cu + v.x * cv, u.y * cu + v.y * cv), u, maxU - minU, maxV - minV)
            }
        }
        return best
    }

    /**
     * Text lines on a page. A page that fits the detector's [size] in one go is detected whole;
     * a bigger one (A4 at 300 DPI) in overlapping tiles, joining fragments of the same line.
     * [infer] gets an RGB picture (its sides multiples of 32) and returns its probability map.
     */
    fun detect(img: Raster, infer: (Raster) -> ProbMap, size: Int): List<TextBox> {
        if (max(img.w, img.h) <= size * 1.4f) {
            val k = min(size.toFloat() / img.w, size.toFloat() / img.h)
            val nw = max(1, (img.w * k).roundToInt())
            val nh = max(1, (img.h * k).roundToInt())
            val input = pad(resize(img, nw, nh), ceil32(nw), ceil32(nh))
            return sortBoxes(dbPostprocess(infer(input)).map { it.scaled(1f / k) })
        }
        // Work where the long side is ~2.2 tiles.
        val workScale = min(1f, size * 2.2f / max(img.w, img.h))
        val work = if (workScale < 1f) resize(img, (img.w * workScale).toInt(), (img.h * workScale).toInt()) else img
        val overlap = size / 6
        val found = ArrayList<TextBox>()
        for ((x, y) in tiles(work.w, work.h, size, overlap)) {
            val tile = pad(crop(work, x, y, size, size), size, size)
            for (b in dbPostprocess(infer(tile))) {
                // Boxes touching an inner tile edge are left to the neighbouring tile, which sees them whole.
                val touches = (b.x0 <= 1 && x > 0) || (b.y0 <= 1 && y > 0) || (b.x1 >= size - 2 && x + size < work.w) || (b.y1 >= size - 2 && y + size < work.h)
                if (touches && b.x1 - b.x0 < size * 0.9f) continue
                found.add(TextBox(Quad(b.quad.tl + Pt(x.toFloat(), y.toFloat()), b.quad.tr + Pt(x.toFloat(), y.toFloat()), b.quad.br + Pt(x.toFloat(), y.toFloat()), b.quad.bl + Pt(x.toFloat(), y.toFloat())), b.score))
            }
        }
        return sortBoxes(mergeBoxes(found.map { it.scaled(1f / workScale) }))
    }

    /** Top-left corners of overlapping size x size tiles covering a w x h picture. */
    internal fun tiles(w: Int, h: Int, size: Int, overlap: Int): List<Pair<Int, Int>> {
        fun axis(n: Int): List<Int> {
            if (n <= size) return listOf(0)
            val pos = (0 until n - size step (size - overlap)).toMutableList()
            pos.add(n - size)
            return pos.distinct().sorted()
        }
        val ys = axis(h)
        return ys.flatMap { y -> axis(w).map { x -> x to y } }
    }

    /** Removes duplicates from overlapping tiles and joins split fragments of a line. */
    fun mergeBoxes(boxes: List<TextBox>): List<TextBox> {
        val kept = ArrayList<TextBox>()
        for (b in boxes.sortedByDescending { (it.x1 - it.x0) * (it.y1 - it.y0) }) {
            var merged = false
            for (i in kept.indices) {
                val k = kept[i]
                val ih = min(b.y1, k.y1) - max(b.y0, k.y0)
                if (ih <= 0) continue
                val vertical = ih / max(1f, min(b.y1 - b.y0, k.y1 - k.y0))
                val gap = max(b.x0, k.x0) - min(b.x1, k.x1)
                val height = max(b.y1 - b.y0, k.y1 - k.y0)
                if (vertical > 0.6f && gap < height * 0.6f) {
                    val x0 = min(b.x0, k.x0)
                    val y0 = min(b.y0, k.y0)
                    val x1 = max(b.x1, k.x1)
                    val y1 = max(b.y1, k.y1)
                    kept[i] = TextBox(Quad(Pt(x0, y0), Pt(x1, y0), Pt(x1, y1), Pt(x0, y1)), max(b.score, k.score))
                    merged = true
                    break
                }
            }
            if (!merged) kept.add(b)
        }
        return kept
    }

    /** Reading order: top to bottom, then left to right within a visual line. */
    fun sortBoxes(boxes: List<TextBox>): List<TextBox> {
        val lines = ArrayList<MutableList<TextBox>>()
        for (b in boxes.sortedWith(compareBy({ it.y0 }, { it.x0 }))) {
            val last = lines.lastOrNull()?.last()
            if (last != null && min(b.y1, last.y1) - max(b.y0, last.y0) > 0.5f * min(b.y1 - b.y0, last.y1 - last.y0)) {
                lines.last().add(b)
            } else {
                lines.add(mutableListOf(b))
            }
        }
        return lines.flatMap { l -> l.sortedBy { it.x0 } }
    }

    // ------------------------------------------------------------ recognition

    /** A recognition input: [w] x [REC_HEIGHT] RGB, the line in the first [content] columns. */
    class RecInput(val strip: Raster, val content: Int) {
        val contentFrac get() = content.toFloat() / strip.w
    }

    /**
     * Straightens a text box out of the page into a strip [REC_HEIGHT] tall, padded to a multiple
     * of 160 wide (few distinct shapes keep the model fast). Tall boxes are vertical text: they
     * are turned to read left to right.
     */
    fun recInput(page: Raster, box: TextBox): RecInput {
        var q = box.quad
        var bw = max(q.tl.dist(q.tr), q.bl.dist(q.br)).toInt().coerceAtLeast(1)
        var bh = max(q.tl.dist(q.bl), q.tr.dist(q.br)).toInt().coerceAtLeast(1)
        if (bh > bw * 1.5f) {
            q = Quad(q.tr, q.br, q.bl, q.tl) // a quarter turn anticlockwise
            val t = bw; bw = bh; bh = t
        }
        val tw = min(REC_MAX_WIDTH, max(1, ceil(REC_HEIGHT * bw / bh.toFloat()).toInt()))
        val width = max(160, ceil(tw / 160f).toInt() * 160)
        val hm = Rectifier.homography(
            doubleArrayOf(0.0, 0.0, tw.toDouble(), 0.0, tw.toDouble(), REC_HEIGHT.toDouble(), 0.0, REC_HEIGHT.toDouble()),
            doubleArrayOf(q.tl.x.toDouble(), q.tl.y.toDouble(), q.tr.x.toDouble(), q.tr.y.toDouble(), q.br.x.toDouble(), q.br.y.toDouble(), q.bl.x.toDouble(), q.bl.y.toDouble()),
        )
        // Big lines shrink a lot: average several samples per output pixel so thin strokes survive.
        val n = ceil(bh / REC_HEIGHT.toFloat()).toInt().coerceIn(1, 4)
        val px = IntArray(width * REC_HEIGHT)
        for (y in 0 until REC_HEIGHT) for (x in 0 until tw) {
            var r = 0
            var g = 0
            var b = 0
            for (sy in 0 until n) for (sx in 0 until n) {
                val dx = x + (sx + 0.5) / n
                val dy = y + (sy + 0.5) / n
                val d = hm[6] * dx + hm[7] * dy + 1.0
                val c = bilinear(page, ((hm[0] * dx + hm[1] * dy + hm[2]) / d - 0.5).toFloat(), ((hm[3] * dx + hm[4] * dy + hm[5]) / d - 0.5).toFloat())
                r += (c shr 16) and 0xFF; g += (c shr 8) and 0xFF; b += c and 0xFF
            }
            val nn = n * n
            px[y * width + x] = (0xFF shl 24) or ((r / nn) shl 16) or ((g / nn) shl 8) or (b / nn)
        }
        return RecInput(Raster(width, REC_HEIGHT, px), tw)
    }

    /**
     * Greedy CTC decoding of [t] x [c] class probabilities (class 0 is the blank), with where each
     * word sits along the line. [contentFrac] is the share of the input width that held the line.
     */
    fun ctcDecode(probs: FloatArray, t: Int, c: Int, charset: List<String>, contentFrac: Float = 1f): RecResult {
        val chars = ArrayList<Triple<String, Int, Float>>()
        var prev = 0
        for (s in 0 until t) {
            var k = 0
            var best = -1f
            for (j in 0 until c) {
                val v = probs[s * c + j]
                if (v > best) { best = v; k = j }
            }
            if (k != 0 && k != prev && k <= charset.size) chars.add(Triple(charset[k - 1], s, best))
            prev = k
        }
        val text = chars.joinToString("") { it.first }
        val confidence = if (chars.isEmpty()) 0f else chars.map { it.third }.average().toFloat()
        val words = ArrayList<Triple<String, Float, Float>>()
        val frac = max(contentFrac, 1e-6f)
        var cur = StringBuilder()
        var start = -1
        var end = -1
        for ((ch, s, _) in chars + Triple(" ", t, 0f)) {
            if (ch.isBlank()) {
                if (cur.isNotEmpty()) words.add(Triple(cur.toString(), min(1f, start.toFloat() / t / frac), min(1f, (end + 1).toFloat() / t / frac)))
                cur = StringBuilder()
                start = -1
                continue
            }
            if (start < 0) start = s
            cur.append(ch)
            end = s
        }
        return RecResult(text.trim(), confidence, words)
    }

    /** PaddleOCR dictionaries list one character per line; a space is added at the end (use_space_char). */
    fun charset(dict: String): List<String> = dict.split('\n').map { it.trimEnd('\r') }.filter { it.isNotEmpty() } + " "

    // ------------------------------------------------------------ which script

    /** Script ids, as in the model file names (rec_<script>.onnx). */
    val SCRIPTS = listOf("en", "devanagari", "ta", "te", "ka")

    /** A document language (ISO 639-1) to the script its text is read with. */
    private val LANG_SCRIPT = mapOf(
        "en" to "en", "de" to "en", "fr" to "en", "es" to "en", "it" to "en", "pt" to "en", "nl" to "en", "id" to "en",
        "hi" to "devanagari", "mr" to "devanagari", "ne" to "devanagari", "sa" to "devanagari", "kok" to "devanagari",
        "mai" to "devanagari", "bho" to "devanagari", "doi" to "devanagari", "brx" to "devanagari", "new" to "devanagari",
        "ta" to "ta", "te" to "te", "kn" to "ka",
    )

    fun scriptOf(language: String): String? = LANG_SCRIPT[language.lowercase().substringBefore('-')]

    /** The available scripts, the [hinted] ones first, then English, then the rest. */
    fun scriptOrder(hinted: List<String>, available: List<String>): List<String> {
        val out = ArrayList<String>()
        for (s in hinted) if (s in available && s !in out) out.add(s)
        if (out.isEmpty() && "en" in available) out.add("en")
        for (s in available) if (s !in out) out.add(s)
        return out
    }

    /**
     * Reads every box and works out the page's script itself: the language is only a hint. The
     * widest lines are read with the hinted script first; if it isn't clearly right, with every
     * script, and the most confident reads the page. Lines it reads poorly are tried again with
     * the other plausible scripts (bilingual forms mix English and Hindi).
     * Returns the lines worth keeping, in box order, and the page's script.
     */
    fun readLines(
        boxes: List<TextBox>, read: (TextBox, String) -> RecResult?, hinted: List<String>, available: List<String>,
        minConf: Float = 0.5f, sample: Int = 8, sure: Float = 0.85f, retryBelow: Float = 0.8f, altConf: Float = 0.75f,
    ): Pair<List<Pair<TextBox, RecResult>>, String> {
        val order = scriptOrder(hinted, available)
        if (boxes.isEmpty() || order.isEmpty()) return emptyList<Pair<TextBox, RecResult>>() to (order.firstOrNull() ?: "en")
        val cache = HashMap<Pair<Int, String>, RecResult?>()
        fun get(i: Int, s: String) = cache.getOrPut(i to s) { read(boxes[i], s) }
        fun conf(r: RecResult?) = r?.confidence ?: 0f

        var primary = order[0]
        var others = order.drop(1)
        if (others.isNotEmpty()) {
            val widest = boxes.indices.sortedBy { boxes[it].x0 - boxes[it].x1 }.take(sample)
            val score = HashMap<String, Float>()
            score[primary] = widest.map { conf(get(it, primary)) }.average().toFloat()
            if (score.getValue(primary) < sure) {
                for (s in others) score[s] = widest.map { conf(get(it, s)) }.average().toFloat()
                primary = order.maxBy { score.getValue(it) } // ties keep the hinted script (maxBy keeps the first)
                val clear = order.filter { s -> widest.any { conf(get(it, s)) >= altConf } }.toSet()
                others = order.filter { it != primary && (it in hinted || it in clear || score.getValue(it) >= 0.5f) }
            }
        }
        // Lines the page's script reads poorly are tried with the others: the English reader turns a
        // Hindi line into confident-looking Latin letters (0.7-0.8), so a low bar would miss it.
        val out = ArrayList<Pair<TextBox, RecResult>>()
        for (i in boxes.indices) {
            var best = get(i, primary)
            if (conf(best) < retryBelow) for (s in others) {
                val r = get(i, s)
                // Another script must read the line clearly, or noise turns into foreign text.
                if (conf(r) > conf(best) && conf(r) >= altConf) best = r
            }
            if (best != null && best.text.isNotEmpty() && best.confidence >= minConf) out.add(boxes[i] to best)
        }
        return out to primary
    }

    // ------------------------------------------------------------ pictures

    private fun ceil32(n: Int) = max(32, (n + 31) / 32 * 32)

    /** [r] in the top-left corner of a black w x h picture. */
    internal fun pad(r: Raster, w: Int, h: Int): Raster {
        if (r.w == w && r.h == h) return r
        val px = IntArray(w * h) { 0xFF shl 24 }
        for (y in 0 until min(h, r.h)) System.arraycopy(r.px, y * r.w, px, y * w, min(w, r.w))
        return Raster(w, h, px)
    }

    internal fun crop(r: Raster, x: Int, y: Int, w: Int, h: Int): Raster {
        val cw = min(w, r.w - x)
        val ch = min(h, r.h - y)
        val px = IntArray(cw * ch)
        for (yy in 0 until ch) System.arraycopy(r.px, (y + yy) * r.w + x, px, yy * cw, cw)
        return Raster(cw, ch, px)
    }

    /** Resizes: averaging the covered pixels when shrinking (keeps thin strokes), bilinear when growing. */
    fun resize(r: Raster, nw: Int, nh: Int): Raster {
        if (nw == r.w && nh == r.h) return r
        val fx = r.w.toFloat() / nw
        val fy = r.h.toFloat() / nh
        val px = IntArray(nw * nh)
        if (fx <= 1f && fy <= 1f) {
            for (y in 0 until nh) for (x in 0 until nw) px[y * nw + x] = bilinear(r, (x + 0.5f) * fx - 0.5f, (y + 0.5f) * fy - 0.5f)
            return Raster(nw, nh, px)
        }
        for (y in 0 until nh) {
            val y0 = floor(y * fy).toInt()
            val y1 = min(r.h, max(y0 + 1, ceil((y + 1) * fy).toInt()))
            for (x in 0 until nw) {
                val x0 = floor(x * fx).toInt()
                val x1 = min(r.w, max(x0 + 1, ceil((x + 1) * fx).toInt()))
                var rr = 0
                var gg = 0
                var bb = 0
                for (yy in y0 until y1) for (xx in x0 until x1) {
                    val c = r.px[yy * r.w + xx]
                    rr += (c shr 16) and 0xFF; gg += (c shr 8) and 0xFF; bb += c and 0xFF
                }
                val n = (y1 - y0) * (x1 - x0)
                px[y * nw + x] = (0xFF shl 24) or ((rr / n) shl 16) or ((gg / n) shl 8) or (bb / n)
            }
        }
        return Raster(nw, nh, px)
    }

    private fun bilinear(r: Raster, x: Float, y: Float): Int {
        val xf = floor(x)
        val yf = floor(y)
        val ax = x - xf
        val ay = y - yf
        val x0 = xf.toInt().coerceIn(0, r.w - 1)
        val y0 = yf.toInt().coerceIn(0, r.h - 1)
        val x1 = (xf.toInt() + 1).coerceIn(0, r.w - 1)
        val y1 = (yf.toInt() + 1).coerceIn(0, r.h - 1)
        val p00 = r.px[y0 * r.w + x0]
        val p10 = r.px[y0 * r.w + x1]
        val p01 = r.px[y1 * r.w + x0]
        val p11 = r.px[y1 * r.w + x1]
        var out = 0xFF shl 24
        for (shift in intArrayOf(16, 8, 0)) {
            val top = ((p00 shr shift) and 0xFF) * (1 - ax) + ((p10 shr shift) and 0xFF) * ax
            val bottom = ((p01 shr shift) and 0xFF) * (1 - ax) + ((p11 shr shift) and 0xFF) * ax
            out = out or ((top * (1 - ay) + bottom * ay + 0.5f).toInt().coerceIn(0, 255) shl shift)
        }
        return out
    }
}
