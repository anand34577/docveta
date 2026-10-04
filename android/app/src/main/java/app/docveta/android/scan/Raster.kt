package app.docveta.android.scan

import kotlin.math.floor
import kotlin.math.max
import kotlin.math.min
import kotlin.math.roundToInt

/** Pixels as 0xAARRGGBB, row by row. Plain arrays, so every step is testable without a phone. */
class Raster(val w: Int, val h: Int, val px: IntArray) {
    init {
        require(px.size >= w * h) { "pixel array too small" }
    }

    fun luma(): ByteArray = ByteArray(w * h) { luminance(px[it]).toByte() }
}

internal fun luminance(argb: Int): Int {
    val r = (argb shr 16) and 0xFF
    val g = (argb shr 8) and 0xFF
    val b = argb and 0xFF
    return (r * 77 + g * 151 + b * 28) shr 8
}

private fun clamp255(v: Float): Int = if (v < 0f) 0 else if (v > 255f) 255 else v.toInt()

/** Turns a skewed photo of a page into a straight, flat page. */
object Rectifier {
    /**
     * The size the flattened page should have, never bigger than [maxSide]. A page photographed at an
     * angle looks squashed, so the true width:height is recovered from the quad itself (Zhang & He's
     * method, assuming the camera looks at the centre of the picture); for a straight-on photo, or when the
     * maths is degenerate, it falls back to averaging opposite edges.
     */
    fun outputSize(q: Quad, maxSide: Int, imageW: Int = 0, imageH: Int = 0): Pair<Int, Int> {
        val edgeW = (q.tl.dist(q.tr) + q.bl.dist(q.br)) / 2f
        val edgeH = (q.tl.dist(q.bl) + q.tr.dist(q.br)) / 2f
        var w = edgeW
        var h = edgeH
        if (imageW > 0 && imageH > 0) {
            val aspect = estimateAspect(q, imageW / 2.0, imageH / 2.0)
            if (aspect != null) {
                // Keep the page area the quad suggests, with the recovered proportions.
                val area = edgeW * edgeH
                h = kotlin.math.sqrt(area / aspect).toFloat()
                w = (h * aspect).toFloat()
            }
        }
        val k = min(1f, maxSide / max(w, h).coerceAtLeast(1f))
        return max(1, (w * k).roundToInt()) to max(1, (h * k).roundToInt())
    }

    /** width / height of the real page, or null when it can't be told (nearly straight-on or degenerate). */
    internal fun estimateAspect(q: Quad, cx: Double, cy: Double): Double? {
        fun v(p: Pt) = doubleArrayOf(p.x - cx, p.y - cy, 1.0)
        fun cross(a: DoubleArray, b: DoubleArray) = doubleArrayOf(a[1] * b[2] - a[2] * b[1], a[2] * b[0] - a[0] * b[2], a[0] * b[1] - a[1] * b[0])
        fun dot(a: DoubleArray, b: DoubleArray) = a[0] * b[0] + a[1] * b[1] + a[2] * b[2]
        val m1 = v(q.tl)
        val m2 = v(q.tr)
        val m3 = v(q.bl)
        val m4 = v(q.br)
        val d2 = dot(cross(m2, m4), m3)
        val d3 = dot(cross(m3, m4), m2)
        if (kotlin.math.abs(d2) < 1e-9 || kotlin.math.abs(d3) < 1e-9) return null
        val k2 = dot(cross(m1, m4), m3) / d2
        val k3 = dot(cross(m1, m4), m2) / d3
        val n2 = doubleArrayOf(k2 * m2[0] - m1[0], k2 * m2[1] - m1[1], k2 * m2[2] - m1[2])
        val n3 = doubleArrayOf(k3 * m3[0] - m1[0], k3 * m3[1] - m1[1], k3 * m3[2] - m1[2])
        // With a straight-on camera n2[2] and n3[2] vanish: no perspective, so the edge lengths are right.
        if (kotlin.math.abs(n2[2] * n3[2]) < 1e-6) return null
        val f2 = -(n2[0] * n3[0] + n2[1] * n3[1]) / (n2[2] * n3[2])
        if (!(f2 > 0.0) || f2.isInfinite()) return null
        val num = (n2[0] * n2[0] + n2[1] * n2[1]) / f2 + n2[2] * n2[2]
        val den = (n3[0] * n3[0] + n3[1] * n3[1]) / f2 + n3[2] * n3[2]
        if (!(den > 0.0) || !(num > 0.0)) return null
        val a = kotlin.math.sqrt(num / den)
        return if (a.isNaN() || a < 0.2 || a > 5.0) null else a
    }

    /**
     * Inverse-maps every output pixel through the homography (output rectangle to the quad in the
     * source) and samples bilinearly.
     */
    fun warp(src: Raster, q: Quad, maxSide: Int = 2339): Raster {
        val (ow, oh) = outputSize(q, maxSide, src.w, src.h)
        val hm = homography(
            doubleArrayOf(0.0, 0.0, ow.toDouble(), 0.0, ow.toDouble(), oh.toDouble(), 0.0, oh.toDouble()),
            doubleArrayOf(q.tl.x.toDouble(), q.tl.y.toDouble(), q.tr.x.toDouble(), q.tr.y.toDouble(), q.br.x.toDouble(), q.br.y.toDouble(), q.bl.x.toDouble(), q.bl.y.toDouble()),
        )
        val out = IntArray(ow * oh)
        val maxX = src.w - 1
        val maxY = src.h - 1
        for (y in 0 until oh) {
            for (x in 0 until ow) {
                val dx = x + 0.5
                val dy = y + 0.5
                val d = hm[6] * dx + hm[7] * dy + 1.0
                val sx = ((hm[0] * dx + hm[1] * dy + hm[2]) / d - 0.5).toFloat()
                val sy = ((hm[3] * dx + hm[4] * dy + hm[5]) / d - 0.5).toFloat()
                out[y * ow + x] = bilinear(src.px, src.w, maxX, maxY, sx, sy)
            }
        }
        return Raster(ow, oh, out)
    }

    private fun bilinear(px: IntArray, w: Int, maxX: Int, maxY: Int, x: Float, y: Float): Int {
        val xf = floor(x)
        val yf = floor(y)
        val fx = x - xf
        val fy = y - yf
        val x0 = xf.toInt().coerceIn(0, maxX)
        val y0 = yf.toInt().coerceIn(0, maxY)
        val x1 = (x0 + 1).coerceAtMost(maxX)
        val y1 = (y0 + 1).coerceAtMost(maxY)
        val p00 = px[y0 * w + x0]
        val p10 = px[y0 * w + x1]
        val p01 = px[y1 * w + x0]
        val p11 = px[y1 * w + x1]
        var out = 0xFF shl 24
        for (shift in intArrayOf(16, 8, 0)) {
            val a = (p00 shr shift) and 0xFF
            val b = (p10 shr shift) and 0xFF
            val c = (p01 shr shift) and 0xFF
            val d = (p11 shr shift) and 0xFF
            val top = a + (b - a) * fx
            val bottom = c + (d - c) * fx
            out = out or (clamp255(top + (bottom - top) * fy + 0.5f) shl shift)
        }
        return out
    }

    /** Solves for the 3x3 projective map (h33 = 1) taking the four [from] points to the four [to] points. */
    internal fun homography(from: DoubleArray, to: DoubleArray): DoubleArray {
        // Unknowns h0..h7; two equations per point pair.
        val a = Array(8) { DoubleArray(9) }
        for (i in 0 until 4) {
            val x = from[2 * i]
            val y = from[2 * i + 1]
            val u = to[2 * i]
            val v = to[2 * i + 1]
            a[2 * i] = doubleArrayOf(x, y, 1.0, 0.0, 0.0, 0.0, -u * x, -u * y, u)
            a[2 * i + 1] = doubleArrayOf(0.0, 0.0, 0.0, x, y, 1.0, -v * x, -v * y, v)
        }
        // Gaussian elimination with partial pivoting.
        for (col in 0 until 8) {
            var piv = col
            for (r in col + 1 until 8) if (kotlin.math.abs(a[r][col]) > kotlin.math.abs(a[piv][col])) piv = r
            val tmp = a[col]; a[col] = a[piv]; a[piv] = tmp
            val p = a[col][col]
            if (kotlin.math.abs(p) < 1e-12) return doubleArrayOf(1.0, 0.0, 0.0, 0.0, 1.0, 0.0, 0.0, 0.0)
            for (c in col until 9) a[col][c] /= p
            for (r in 0 until 8) {
                if (r == col) continue
                val f = a[r][col]
                if (f == 0.0) continue
                for (c in col until 9) a[r][c] -= f * a[col][c]
            }
        }
        return DoubleArray(8) { a[it][8] }
    }
}

/** Clean-up filters for a flattened page. */
object PageFilters {
    enum class Kind { ORIGINAL, ENHANCED, GRAY, BLACK_WHITE }

    fun apply(r: Raster, kind: Kind): Raster = when (kind) {
        Kind.ORIGINAL -> r
        Kind.ENHANCED -> enhance(r)
        Kind.GRAY -> gray(r)
        Kind.BLACK_WHITE -> blackWhite(r)
    }

    fun gray(r: Raster): Raster {
        val out = IntArray(r.w * r.h) { val l = luminance(r.px[it]); (0xFF shl 24) or (l shl 16) or (l shl 8) or l }
        return Raster(r.w, r.h, out)
    }

    /**
     * Evens out the lighting and whitens the paper: every pixel is divided by the paper colour
     * around it (estimated from a coarse grid of local brightest values), then contrast is stretched.
     * Shadows and a yellow cast disappear; ink and photos keep their colour.
     */
    fun enhance(r: Raster): Raster {
        val block = 8
        val gw = max(1, (r.w + block - 1) / block)
        val gh = max(1, (r.h + block - 1) / block)
        // Local "paper" colour: brightest pixel of each block, per channel (text strokes are thinner than a block).
        val bg = Array(3) { FloatArray(gw * gh) }
        for (gy in 0 until gh) for (gx in 0 until gw) {
            var mr = 0
            var mg = 0
            var mb = 0
            for (y in gy * block until min(r.h, (gy + 1) * block)) for (x in gx * block until min(r.w, (gx + 1) * block)) {
                val p = r.px[y * r.w + x]
                mr = max(mr, (p shr 16) and 0xFF)
                mg = max(mg, (p shr 8) and 0xFF)
                mb = max(mb, p and 0xFF)
            }
            bg[0][gy * gw + gx] = mr.toFloat()
            bg[1][gy * gw + gx] = mg.toFloat()
            bg[2][gy * gw + gx] = mb.toFloat()
        }
        for (c in 0 until 3) {
            repeat(2) { bg[c] = blurGrid(bg[c], gw, gh) }
        }
        val out = IntArray(r.w * r.h)
        val black = 0.28f
        val white = 0.93f
        for (y in 0 until r.h) {
            val gyf = ((y + 0.5f) / block - 0.5f).coerceIn(0f, (gh - 1).toFloat())
            for (x in 0 until r.w) {
                val gxf = ((x + 0.5f) / block - 0.5f).coerceIn(0f, (gw - 1).toFloat())
                val p = r.px[y * r.w + x]
                var o = 0xFF shl 24
                for (c in 0 until 3) {
                    val shift = 16 - 8 * c
                    val b = max(40f, sample(bg[c], gw, gh, gxf, gyf))
                    val v = ((p shr shift) and 0xFF) / b
                    val s = ((v - black) / (white - black)).coerceIn(0f, 1f)
                    o = o or (clamp255(s * 255f + 0.5f) shl shift)
                }
                out[y * r.w + x] = o
            }
        }
        return Raster(r.w, r.h, out)
    }

    /** Pure black ink on white paper (like a photocopy): each pixel is compared with the average around it. */
    fun blackWhite(r: Raster): Raster {
        val w = r.w
        val h = r.h
        val integral = LongArray((w + 1) * (h + 1))
        for (y in 0 until h) {
            var row = 0L
            for (x in 0 until w) {
                row += luminance(r.px[y * w + x])
                integral[(y + 1) * (w + 1) + x + 1] = integral[y * (w + 1) + x + 1] + row
            }
        }
        val half = max(8, max(w, h) / 32)
        val out = IntArray(w * h)
        for (y in 0 until h) {
            val y0 = max(0, y - half)
            val y1 = min(h, y + half + 1)
            for (x in 0 until w) {
                val x0 = max(0, x - half)
                val x1 = min(w, x + half + 1)
                val sum = integral[y1 * (w + 1) + x1] - integral[y0 * (w + 1) + x1] - integral[y1 * (w + 1) + x0] + integral[y0 * (w + 1) + x0]
                val mean = sum.toDouble() / ((x1 - x0) * (y1 - y0))
                val l = luminance(r.px[y * w + x])
                out[y * w + x] = if (l > mean * 0.88) -1 else (0xFF shl 24)
            }
        }
        return Raster(w, h, out)
    }

    /** Quarter turns clockwise. */
    fun rotate(r: Raster, quarterTurns: Int): Raster {
        return when (((quarterTurns % 4) + 4) % 4) {
            0 -> r
            1 -> {
                val out = IntArray(r.w * r.h)
                for (y in 0 until r.h) for (x in 0 until r.w) out[x * r.h + (r.h - 1 - y)] = r.px[y * r.w + x]
                Raster(r.h, r.w, out)
            }
            2 -> {
                val out = IntArray(r.w * r.h)
                for (i in out.indices) out[r.w * r.h - 1 - i] = r.px[i]
                Raster(r.w, r.h, out)
            }
            else -> {
                val out = IntArray(r.w * r.h)
                for (y in 0 until r.h) for (x in 0 until r.w) out[(r.w - 1 - x) * r.h + y] = r.px[y * r.w + x]
                Raster(r.h, r.w, out)
            }
        }
    }

    private fun blurGrid(g: FloatArray, w: Int, h: Int): FloatArray {
        val tmp = FloatArray(w * h)
        for (y in 0 until h) for (x in 0 until w) {
            var s = 0f
            for (dx in -2..2) s += g[y * w + (x + dx).coerceIn(0, w - 1)]
            tmp[y * w + x] = s / 5f
        }
        val out = FloatArray(w * h)
        for (y in 0 until h) for (x in 0 until w) {
            var s = 0f
            for (dy in -2..2) s += tmp[(y + dy).coerceIn(0, h - 1) * w + x]
            out[y * w + x] = s / 5f
        }
        return out
    }

    private fun sample(g: FloatArray, w: Int, h: Int, x: Float, y: Float): Float {
        val x0 = x.toInt()
        val y0 = y.toInt()
        val x1 = min(w - 1, x0 + 1)
        val y1 = min(h - 1, y0 + 1)
        val fx = x - x0
        val fy = y - y0
        val top = g[y0 * w + x0] * (1 - fx) + g[y0 * w + x1] * fx
        val bottom = g[y1 * w + x0] * (1 - fx) + g[y1 * w + x1] * fx
        return top * (1 - fy) + bottom * fy
    }
}
