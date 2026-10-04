package app.docveta.android.scan

import kotlin.math.abs
import kotlin.math.hypot
import kotlin.math.max
import kotlin.math.min

data class Pt(val x: Float, val y: Float) {
    operator fun plus(o: Pt) = Pt(x + o.x, y + o.y)
    operator fun minus(o: Pt) = Pt(x - o.x, y - o.y)
    operator fun times(k: Float) = Pt(x * k, y * k)
    fun dist(o: Pt) = hypot(x - o.x, y - o.y)
}

/** Four corners of a page, in the order top-left, top-right, bottom-right, bottom-left (as seen on the picture). */
data class Quad(val tl: Pt, val tr: Pt, val br: Pt, val bl: Pt) {
    val points get() = listOf(tl, tr, br, bl)

    fun scaled(sx: Float, sy: Float) = Quad(Pt(tl.x * sx, tl.y * sy), Pt(tr.x * sx, tr.y * sy), Pt(br.x * sx, br.y * sy), Pt(bl.x * sx, bl.y * sy))

    /** Area by the shoelace formula. */
    fun area(): Float {
        val p = points
        var s = 0f
        for (i in p.indices) {
            val a = p[i]
            val b = p[(i + 1) % 4]
            s += a.x * b.y - b.x * a.y
        }
        return abs(s) / 2f
    }

    /** True when the corners form a simple convex quadrilateral (no bow-tie, no dent). */
    fun isConvex(): Boolean {
        val p = points
        var sign = 0
        for (i in 0 until 4) {
            val a = p[i]
            val b = p[(i + 1) % 4]
            val c = p[(i + 2) % 4]
            val cross = (b.x - a.x) * (c.y - b.y) - (b.y - a.y) * (c.x - b.x)
            val s = if (cross > 0) 1 else if (cross < 0) -1 else 0
            if (s == 0) continue
            if (sign == 0) sign = s else if (s != sign) return false
        }
        return sign != 0
    }

    fun moved(index: Int, to: Pt) = when (index) {
        0 -> copy(tl = to)
        1 -> copy(tr = to)
        2 -> copy(br = to)
        else -> copy(bl = to)
    }

    /** Largest distance between matching corners, relative to the quad's diagonal. */
    fun differenceTo(o: Quad): Float {
        val d = max(max(tl.dist(o.tl), tr.dist(o.tr)), max(br.dist(o.br), bl.dist(o.bl)))
        return d / max(1f, tl.dist(br))
    }

    fun clampedTo(w: Float, h: Float) = Quad(
        Pt(tl.x.coerceIn(0f, w), tl.y.coerceIn(0f, h)), Pt(tr.x.coerceIn(0f, w), tr.y.coerceIn(0f, h)),
        Pt(br.x.coerceIn(0f, w), br.y.coerceIn(0f, h)), Pt(bl.x.coerceIn(0f, w), bl.y.coerceIn(0f, h)),
    )

    /** Rotates the corner labels a quarter turn clockwise (used when the page is turned). */
    fun rotatedLabels(quarterTurns: Int): Quad {
        var q = this
        repeat(((quarterTurns % 4) + 4) % 4) { q = Quad(q.bl, q.tl, q.tr, q.br) }
        return q
    }

    companion object {
        /** A rectangle inset from the picture's edges: the starting point when nothing was detected. */
        fun inset(w: Float, h: Float, fraction: Float = 0.06f): Quad {
            val dx = w * fraction
            val dy = h * fraction
            return Quad(Pt(dx, dy), Pt(w - dx, dy), Pt(w - dx, h - dy), Pt(dx, h - dy))
        }

        fun full(w: Float, h: Float) = Quad(Pt(0f, 0f), Pt(w, 0f), Pt(w, h), Pt(0f, h))

        /** Puts four unordered points into tl, tr, br, bl order. */
        fun ordered(p: List<Pt>): Quad {
            require(p.size == 4)
            val tl = p.minBy { it.x + it.y }
            val br = p.maxBy { it.x + it.y }
            val tr = p.maxBy { it.x - it.y }
            val bl = p.minBy { it.x - it.y }
            // Ties (a near-45° page) can pick the same point twice; fall back to a sort by angle around the centre.
            if (setOf(tl, tr, br, bl).size < 4) {
                val c = Pt(p.map { it.x }.average().toFloat(), p.map { it.y }.average().toFloat())
                val s = p.sortedBy { kotlin.math.atan2((it.y - c.y).toDouble(), (it.x - c.x).toDouble()) }
                val start = s.indices.minBy { s[it].x + s[it].y }
                val r = List(4) { s[(start + it) % 4] }
                return Quad(r[0], r[1], r[2], r[3])
            }
            return Quad(tl, tr, br, bl)
        }
    }
}

/** Convex hull (Andrew's monotone chain). Returns vertices in counter-clockwise order on a y-down screen. */
internal fun convexHull(points: List<Pt>): List<Pt> {
    if (points.size < 3) return points
    val pts = points.sortedWith(compareBy({ it.x }, { it.y }))
    fun cross(o: Pt, a: Pt, b: Pt) = (a.x - o.x) * (b.y - o.y) - (a.y - o.y) * (b.x - o.x)
    val lower = ArrayList<Pt>()
    for (p in pts) {
        while (lower.size >= 2 && cross(lower[lower.size - 2], lower[lower.size - 1], p) <= 0) lower.removeAt(lower.size - 1)
        lower.add(p)
    }
    val upper = ArrayList<Pt>()
    for (p in pts.asReversed()) {
        while (upper.size >= 2 && cross(upper[upper.size - 2], upper[upper.size - 1], p) <= 0) upper.removeAt(upper.size - 1)
        upper.add(p)
    }
    lower.removeAt(lower.size - 1)
    upper.removeAt(upper.size - 1)
    return lower + upper
}

/** Douglas–Peucker simplification of a closed polygon: keeps the vertices that matter. */
internal fun simplifyClosed(poly: List<Pt>, epsilon: Float): List<Pt> {
    if (poly.size <= 4) return poly
    // Split the ring at the two points farthest apart so both halves are open polylines.
    var a = 0
    var b = 0
    var best = -1f
    for (i in poly.indices) for (j in i + 1 until poly.size) {
        val d = poly[i].dist(poly[j])
        if (d > best) {
            best = d
            a = i
            b = j
        }
    }
    fun dp(pts: List<Pt>): List<Pt> {
        if (pts.size < 3) return pts
        val s = pts.first()
        val e = pts.last()
        var idx = -1
        var maxD = 0f
        for (i in 1 until pts.size - 1) {
            val d = distToSegment(pts[i], s, e)
            if (d > maxD) {
                maxD = d
                idx = i
            }
        }
        return if (maxD > epsilon) dp(pts.subList(0, idx + 1)).dropLast(1) + dp(pts.subList(idx, pts.size)) else listOf(s, e)
    }
    val first = poly.slice(a..b)
    val second = poly.slice(b until poly.size) + poly.slice(0..a)
    return dp(first).dropLast(1) + dp(second).dropLast(1)
}

internal fun distToSegment(p: Pt, a: Pt, b: Pt): Float {
    val dx = b.x - a.x
    val dy = b.y - a.y
    val len2 = dx * dx + dy * dy
    if (len2 == 0f) return p.dist(a)
    val t = (((p.x - a.x) * dx + (p.y - a.y) * dy) / len2).coerceIn(0f, 1f)
    return hypot(p.x - (a.x + t * dx), p.y - (a.y + t * dy))
}

/** The four vertices of a convex polygon that enclose the most area (brute force over at most ~24 vertices). */
internal fun largestInscribedQuad(hull: List<Pt>): Quad? {
    val n = hull.size
    if (n < 4) return null
    var best: List<Pt>? = null
    var bestArea = -1f
    fun triArea(a: Pt, b: Pt, c: Pt) = abs((b.x - a.x) * (c.y - a.y) - (c.x - a.x) * (b.y - a.y)) / 2f
    for (i in 0 until n - 3) for (j in i + 1 until n - 2) for (k in j + 1 until n - 1) for (l in k + 1 until n) {
        // Vertices of a convex polygon taken in order form a convex quad: area = two triangles.
        val area = triArea(hull[i], hull[j], hull[k]) + triArea(hull[i], hull[k], hull[l])
        if (area > bestArea) {
            bestArea = area
            best = listOf(hull[i], hull[j], hull[k], hull[l])
        }
    }
    return best?.let { Quad.ordered(it) }
}

internal fun minOf3(a: Float, b: Float, c: Float) = min(a, min(b, c))
