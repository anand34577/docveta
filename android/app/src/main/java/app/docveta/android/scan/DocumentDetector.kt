package app.docveta.android.scan

import kotlin.math.max
import kotlin.math.min

/**
 * Finds the page in a photo: a paper rectangle against a darker (or lighter) surface.
 *
 * Works on a small greyscale copy: blur, split light from dark with Otsu's threshold, take the
 * biggest connected blob, and fit the four-sided shape that best covers its convex hull. No native
 * code and no Google services, so it behaves the same on every phone and is unit-tested on the JVM.
 *
 * ponytail: brightness-based only. White paper on a white desk, or a page lying on busy fabric,
 * returns null and the crop screen starts from an adjustable default instead. A gradient-based
 * second pass (Canny + Hough lines) is the upgrade if that proves common.
 */
object DocumentDetector {
    private const val WORK_SIDE = 240

    /** [luma] is row-major 8-bit brightness, [w] x [h]. The result is in that picture's coordinates. */
    fun detect(luma: ByteArray, w: Int, h: Int): Quad? {
        if (w < 32 || h < 32 || luma.size < w * h) return null
        val small = downscale(luma, w, h, WORK_SIDE)
        val sw = small.w
        val sh = small.h
        val blurred = boxBlur(boxBlur(small.px, sw, sh), sw, sh)
        val t = otsu(blurred)
        var best: Candidate? = null
        for (bright in booleanArrayOf(true, false)) {
            val mask = BooleanArray(sw * sh) { (blurred[it].toInt() and 0xFF > t) == bright }
            val cleaned = dilate(erode(mask, sw, sh), sw, sh)
            val c = candidate(cleaned, blurred, sw, sh) ?: continue
            if (best == null || c.score > best.score) best = c
        }
        val q = best?.quad ?: return null
        return q.scaled(w.toFloat() / sw, h.toFloat() / sh)
    }

    private class Small(val w: Int, val h: Int, val px: ByteArray)

    private fun downscale(src: ByteArray, w: Int, h: Int, side: Int): Small {
        val f = max(w, h).toFloat() / side
        if (f <= 1f) return Small(w, h, src.copyOf(w * h))
        val sw = max(1, (w / f).toInt())
        val sh = max(1, (h / f).toInt())
        val out = ByteArray(sw * sh)
        for (y in 0 until sh) {
            val y0 = (y * f).toInt()
            val y1 = min(h, max(y0 + 1, ((y + 1) * f).toInt()))
            for (x in 0 until sw) {
                val x0 = (x * f).toInt()
                val x1 = min(w, max(x0 + 1, ((x + 1) * f).toInt()))
                var sum = 0
                for (yy in y0 until y1) for (xx in x0 until x1) sum += src[yy * w + xx].toInt() and 0xFF
                out[y * sw + x] = (sum / ((y1 - y0) * (x1 - x0))).toByte()
            }
        }
        return Small(sw, sh, out)
    }

    private fun boxBlur(src: ByteArray, w: Int, h: Int): ByteArray {
        val tmp = IntArray(w * h)
        for (y in 0 until h) for (x in 0 until w) {
            var s = 0
            for (dx in -1..1) s += src[y * w + (x + dx).coerceIn(0, w - 1)].toInt() and 0xFF
            tmp[y * w + x] = s
        }
        val out = ByteArray(w * h)
        for (y in 0 until h) for (x in 0 until w) {
            var s = 0
            for (dy in -1..1) s += tmp[(y + dy).coerceIn(0, h - 1) * w + x]
            out[y * w + x] = (s / 9).toByte()
        }
        return out
    }

    /** Otsu's method: the brightness that best separates two groups of pixels. */
    internal fun otsu(px: ByteArray): Int {
        val hist = IntArray(256)
        for (b in px) hist[b.toInt() and 0xFF]++
        val total = px.size
        var sum = 0L
        for (i in 0..255) sum += i.toLong() * hist[i]
        var sumB = 0L
        var wB = 0
        var maxVar = -1.0
        var threshold = 127
        for (i in 0..255) {
            wB += hist[i]
            if (wB == 0) continue
            val wF = total - wB
            if (wF == 0) break
            sumB += i.toLong() * hist[i]
            val mB = sumB.toDouble() / wB
            val mF = (sum - sumB).toDouble() / wF
            val v = wB.toDouble() * wF * (mB - mF) * (mB - mF)
            if (v > maxVar) {
                maxVar = v
                threshold = i
            }
        }
        return threshold
    }

    private fun erode(m: BooleanArray, w: Int, h: Int) = morph(m, w, h, true)
    private fun dilate(m: BooleanArray, w: Int, h: Int) = morph(m, w, h, false)

    /** 3x3 erosion or dilation. Pixels outside the picture count as "same as the edge" so a page touching the frame stays whole. */
    private fun morph(m: BooleanArray, w: Int, h: Int, erode: Boolean): BooleanArray {
        val out = BooleanArray(w * h)
        for (y in 0 until h) for (x in 0 until w) {
            var all = true
            var any = false
            for (dy in -1..1) for (dx in -1..1) {
                val v = m[(y + dy).coerceIn(0, h - 1) * w + (x + dx).coerceIn(0, w - 1)]
                if (v) any = true else all = false
            }
            out[y * w + x] = if (erode) all else any
        }
        return out
    }

    private class Candidate(val quad: Quad, val score: Float)

    private fun candidate(mask: BooleanArray, luma: ByteArray, w: Int, h: Int): Candidate? {
        val comp = largestComponent(mask, w, h) ?: return null
        val frac = comp.count.toFloat() / (w * h)
        if (frac < 0.10f || frac > 0.96f) return null

        // Boundary pixels of the blob.
        val inComp = BooleanArray(w * h)
        for (i in comp.pixels) inComp[i] = true
        val boundary = ArrayList<Pt>()
        for (i in comp.pixels) {
            val x = i % w
            val y = i / w
            val edge = x == 0 || y == 0 || x == w - 1 || y == h - 1 || !inComp[i - 1] || !inComp[i + 1] || !inComp[i - w] || !inComp[i + w]
            if (edge) boundary.add(Pt(x + 0.5f, y + 0.5f))
        }
        val hull = convexHull(boundary)
        if (hull.size < 4) return null
        val hullQuadArea = polygonArea(hull)
        // A page is a solid blob. A thin ring (the table around the page) has a hull as big as the frame
        // but is mostly empty inside, so it is rejected here.
        if (comp.count / max(1f, hullQuadArea) < 0.55f) return null
        // The blob must differ from its surroundings, or this is just noise split in two.
        var inSum = 0L
        var outSum = 0L
        for (i in luma.indices) if (inComp[i]) inSum += luma[i].toInt() and 0xFF else outSum += luma[i].toInt() and 0xFF
        val outN = luma.size - comp.count
        if (outN == 0 || kotlin.math.abs(inSum.toFloat() / comp.count - outSum.toFloat() / outN) < 18f) return null
        var eps = 0.012f * perimeter(hull)
        var simple = simplifyClosed(hull, eps)
        while (simple.size > 24) {
            eps *= 1.5f
            simple = simplifyClosed(hull, eps)
        }
        val quad = largestInscribedQuad(simple) ?: return null
        if (!quad.isConvex()) return null
        val qa = quad.area()
        val ratio = qa / max(1f, hullQuadArea)
        val minSide = minOf3(quad.tl.dist(quad.tr), quad.tr.dist(quad.br), min(quad.br.dist(quad.bl), quad.bl.dist(quad.tl)))
        // A "page" as big as the whole frame is the frame, not a page.
        if (ratio < 0.80f || qa / (w * h) < 0.08f || qa / (w * h) > 0.97f || minSide < 0.10f * min(w, h)) return null
        return Candidate(quad, (qa / (w * h)) * ratio * ratio * ratio)
    }

    private class Component(val pixels: IntArray, val count: Int)

    private fun largestComponent(mask: BooleanArray, w: Int, h: Int): Component? {
        val seen = BooleanArray(w * h)
        val stack = IntArray(w * h)
        var best: IntArray? = null
        var bestN = 0
        val cur = IntArray(w * h)
        for (start in mask.indices) {
            if (!mask[start] || seen[start]) continue
            var sp = 0
            var n = 0
            stack[sp++] = start
            seen[start] = true
            while (sp > 0) {
                val i = stack[--sp]
                cur[n++] = i
                val x = i % w
                val y = i / w
                if (x > 0 && mask[i - 1] && !seen[i - 1]) { seen[i - 1] = true; stack[sp++] = i - 1 }
                if (x < w - 1 && mask[i + 1] && !seen[i + 1]) { seen[i + 1] = true; stack[sp++] = i + 1 }
                if (y > 0 && mask[i - w] && !seen[i - w]) { seen[i - w] = true; stack[sp++] = i - w }
                if (y < h - 1 && mask[i + w] && !seen[i + w]) { seen[i + w] = true; stack[sp++] = i + w }
            }
            if (n > bestN) {
                bestN = n
                best = cur.copyOf(n)
            }
        }
        return best?.let { Component(it, bestN) }
    }

    private fun polygonArea(p: List<Pt>): Float {
        var s = 0f
        for (i in p.indices) {
            val a = p[i]
            val b = p[(i + 1) % p.size]
            s += a.x * b.y - b.x * a.y
        }
        return kotlin.math.abs(s) / 2f
    }

    private fun perimeter(p: List<Pt>): Float {
        var s = 0f
        for (i in p.indices) s += p[i].dist(p[(i + 1) % p.size])
        return s
    }
}

/**
 * Smooths detections across camera frames and tells when the page has held still, so the camera
 * can capture by itself.
 */
class QuadTracker(private val stillFrames: Int = 8) {
    private var current: Quad? = null
    private var steady = 0
    private var missed = 0

    val quad: Quad? get() = current

    /** True once the page has stayed in place for [stillFrames] detections in a row. */
    val isSteady: Boolean get() = current != null && steady >= stillFrames

    fun update(detected: Quad?): Quad? {
        if (detected == null) {
            if (++missed > 3) {
                current = null
                steady = 0
            }
            return current
        }
        missed = 0
        val prev = current
        if (prev == null) {
            current = detected
            steady = 0
        } else {
            val diff = prev.differenceTo(detected)
            if (diff > 0.12f) {
                current = detected // the page moved: jump to it
                steady = 0
            } else {
                // Ease towards the new reading to remove jitter.
                fun mix(a: Pt, b: Pt) = Pt(a.x * 0.6f + b.x * 0.4f, a.y * 0.6f + b.y * 0.4f)
                current = Quad(mix(prev.tl, detected.tl), mix(prev.tr, detected.tr), mix(prev.br, detected.br), mix(prev.bl, detected.bl))
                steady = if (diff < 0.02f) steady + 1 else max(0, steady - 1)
            }
        }
        return current
    }

    fun reset() {
        current = null
        steady = 0
        missed = 0
    }
}
