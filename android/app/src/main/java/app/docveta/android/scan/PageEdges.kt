package app.docveta.android.scan

import kotlin.math.PI
import kotlin.math.abs
import kotlin.math.atan2
import kotlin.math.cos
import kotlin.math.hypot
import kotlin.math.max
import kotlin.math.min
import kotlin.math.roundToInt
import kotlin.math.sin
import kotlin.math.sqrt

/**
 * Where brightness changes sharply in a small greyscale picture: thin (non-maximum suppressed)
 * Sobel edges, each with the direction it faces. [angle] is the edge normal in [0, π), NaN off edges.
 */
internal class EdgeMap(val w: Int, val h: Int, val angle: FloatArray, val points: IntArray) {
    fun isEdge(x: Int, y: Int) = x in 0 until w && y in 0 until h && !angle[y * w + x].isNaN()

    companion object {
        private const val MIN_STRENGTH = 16
        private const val MAX_SHARE = 0.30f // at most this share of pixels count as edges (busy surfaces)

        fun of(px: ByteArray, w: Int, h: Int): EdgeMap {
            val mag = IntArray(w * h)
            val dir = ByteArray(w * h) // gradient direction in 4 steps, for the thinning
            val gxs = IntArray(w * h)
            val gys = IntArray(w * h)
            fun p(x: Int, y: Int) = px[y * w + x].toInt() and 0xFF
            for (y in 1 until h - 1) for (x in 1 until w - 1) {
                val gx = p(x + 1, y - 1) + 2 * p(x + 1, y) + p(x + 1, y + 1) - p(x - 1, y - 1) - 2 * p(x - 1, y) - p(x - 1, y + 1)
                val gy = p(x - 1, y + 1) + 2 * p(x, y + 1) + p(x + 1, y + 1) - p(x - 1, y - 1) - 2 * p(x, y - 1) - p(x + 1, y - 1)
                val i = y * w + x
                gxs[i] = gx
                gys[i] = gy
                mag[i] = hypot(gx.toFloat(), gy.toFloat()).toInt()
                val a = (atan2(gy.toFloat(), gx.toFloat()) * 4f / PI.toFloat()).roundToInt()
                dir[i] = (((a % 4) + 4) % 4).toByte()
            }
            // Keep only the ridge of each edge, then the strongest of those.
            val keep = BooleanArray(w * h)
            val hist = IntArray(1025)
            var kept = 0
            for (y in 1 until h - 1) for (x in 1 until w - 1) {
                val i = y * w + x
                val m = mag[i]
                if (m < MIN_STRENGTH) continue
                val (a, b) = when (dir[i].toInt()) {
                    0 -> mag[i - 1] to mag[i + 1]
                    1 -> mag[i - w - 1] to mag[i + w + 1]
                    2 -> mag[i - w] to mag[i + w]
                    else -> mag[i - w + 1] to mag[i + w - 1]
                }
                if (m >= a && m > b) {
                    keep[i] = true
                    hist[min(1024, m)]++
                    kept++
                }
            }
            var threshold = MIN_STRENGTH
            val cap = (MAX_SHARE * w * h).toInt()
            if (kept > cap) {
                var above = 0
                for (m in 1024 downTo 0) {
                    above += hist[m]
                    if (above >= cap) { threshold = m; break }
                }
            }
            val angle = FloatArray(w * h) { Float.NaN }
            val pts = ArrayList<Int>()
            for (i in keep.indices) if (keep[i] && mag[i] >= threshold) {
                var a = atan2(gys[i].toFloat(), gxs[i].toFloat())
                if (a < 0f) a += PI.toFloat()
                if (a >= PI.toFloat()) a -= PI.toFloat()
                angle[i] = a
                pts.add(i)
            }
            return EdgeMap(w, h, angle, pts.toIntArray())
        }
    }
}

/** A straight line nx·x + ny·y = c with (nx, ny) of unit length. [border] marks the picture's own edges. */
internal data class Line(val nx: Float, val ny: Float, val c: Float, val border: Boolean = false) {
    fun distance(p: Pt) = nx * p.x + ny * p.y - c

    fun intersect(o: Line): Pt? {
        val det = nx * o.ny - ny * o.nx
        if (abs(det) < 1e-4f) return null
        return Pt((c * o.ny - ny * o.c) / det, (nx * o.c - c * o.nx) / det)
    }

    companion object {
        fun through(a: Pt, b: Pt): Line {
            val dx = b.x - a.x
            val dy = b.y - a.y
            val len = max(1e-6f, hypot(dx, dy))
            val nx = -dy / len
            val ny = dx / len
            return Line(nx, ny, nx * a.x + ny * a.y)
        }
    }
}

/**
 * Finds the page from its outline rather than its brightness, so a white page on a light desk or on
 * a busy surface is found too: straight lines from a Hough transform over the edges, every
 * four-line combination that makes a plausible page, and the one whose sides are best backed by
 * real edges, with paper on one side and background on the other.
 */
internal object PageEdges {
    private const val THETA_BINS = 180
    private const val VOTE_SPREAD = 6 // degrees either side of each edge's own direction
    private const val MAX_LINES = 14
    private const val SAMPLES = 40
    private const val BORDER_SUPPORT = 0.45f

    private val cosT = FloatArray(THETA_BINS) { cos(it * PI / THETA_BINS).toFloat() }
    private val sinT = FloatArray(THETA_BINS) { sin(it * PI / THETA_BINS).toFloat() }

    class Scored(val quad: Quad, val score: Float)

    /** Candidate pages, best first. [luma] is the (blurred) picture [edges] came from. */
    fun candidates(edges: EdgeMap, luma: ByteArray, limit: Int = 4): List<Scored> {
        val lines = lines(edges, luma) + borders(edges.w, edges.h)
        val w = edges.w
        val h = edges.h
        val minGap = 0.12f * min(w, h)
        val centre = Pt(w / 2f, h / 2f)
        // Pairs of roughly parallel lines far enough apart to be opposite sides.
        val pairs = ArrayList<Pair<Line, Line>>()
        for (i in lines.indices) for (j in i + 1 until lines.size) {
            val a = lines[i]
            val b = lines[j]
            if (abs(a.nx * b.nx + a.ny * b.ny) < 0.82f) continue // more than ~35° apart
            val foot = Pt(centre.x - a.distance(centre) * a.nx, centre.y - a.distance(centre) * a.ny)
            if (abs(b.distance(foot)) < minGap) continue
            pairs.add(a to b)
        }
        val found = ArrayList<Scored>()
        for (p in pairs.indices) for (q in p + 1 until pairs.size) {
            val (a, b) = pairs[p]
            val (c, d) = pairs[q]
            if (a === c || a === d || b === c || b === d) continue
            if (abs(a.nx * c.nx + a.ny * c.ny) > 0.64f) continue // the two pairs must cross at a fair angle
            if (listOf(a, b, c, d).count { it.border } > 1) continue
            val corners = listOf(a.intersect(c), a.intersect(d), b.intersect(d), b.intersect(c))
            if (corners.any { it == null || it.x < -0.02f * w || it.y < -0.02f * h || it.x > 1.02f * w || it.y > 1.02f * h }) continue
            val quad = Quad.ordered(corners.map { it!! }).clampedTo(w.toFloat(), h.toFloat())
            val s = score(quad, edges, luma, brightnessFound = false) ?: continue
            found.add(Scored(quad, s))
        }
        found.sortByDescending { it.score }
        // Near-duplicates (the same page from slightly different lines) only once.
        val out = ArrayList<Scored>()
        for (s in found) {
            if (out.none { it.quad.differenceTo(s.quad) < 0.04f }) out.add(s)
            if (out.size >= limit) break
        }
        return out
    }

    /**
     * How much this looks like the page: its share of the picture, times how well its sides lie on
     * real edges. Null when it isn't plausible: a side without edges under it, or a "side" with the
     * same brightness on both sides of it (a ruled line, a row of text) rather than paper against
     * background. [brightnessFound] quads already passed the brightness checks, so they get the benefit
     * of the doubt.
     */
    fun score(q: Quad, edges: EdgeMap, luma: ByteArray, brightnessFound: Boolean): Float? {
        val w = edges.w
        val h = edges.h
        val areaFrac = q.area() / (w.toFloat() * h)
        if (!q.isConvex() || areaFrac < 0.08f || areaFrac > 0.97f) return null
        val pts = q.points
        val centre = Pt(pts.map { it.x }.average().toFloat(), pts.map { it.y }.average().toFloat())
        val reach = stepReach(w, h)
        var supportSum = 0f
        var sign = 0
        val borderSides = ArrayList<Int>()
        var paper = 0f
        var background = 0f
        for (i in 0 until 4) {
            val a = pts[i]
            val b = pts[(i + 1) % 4]
            if (onBorder(a, b, w, h)) {
                borderSides.add(i)
                supportSum += BORDER_SUPPORT
                continue
            }
            val line = Line.through(a, b)
            val inward = if (line.distance(centre) > 0) 1f else -1f
            val sideAngle = normalAngle(line)
            var hits = 0
            for (s in 0 until SAMPLES) {
                val t = 0.06f + 0.88f * s / (SAMPLES - 1)
                if (edgeNear(edges, Pt(a.x + (b.x - a.x) * t, a.y + (b.y - a.y) * t), line, sideAngle)) hits++
            }
            val support = hits.toFloat() / SAMPLES
            val (inside, outside) = levels(luma, w, h, a, b, line, inward, reach)
            val step = inside - outside
            paper += inside
            background += outside
            if (brightnessFound) {
                supportSum += max(support, 0.55f)
                continue
            }
            if (support < 0.35f || abs(step) < 7f || !quietBeside(edges, a, b, line, sideAngle, reach)) return null
            val sg = if (step > 0) 1 else -1
            if (sign == 0) sign = sg else if (sign != sg) return null // paper on the same side of every edge
            supportSum += support
        }
        if (borderSides.size > (if (brightnessFound) 2 else 1)) return null
        if (borderSides.isNotEmpty() && !brightnessFound) {
            // A side may lie on the picture's border only where the paper runs off the picture: the
            // strip along it must look like the paper, not like the background.
            val real = 4 - borderSides.size
            paper /= real
            background /= real
            for (i in borderSides) {
                val a = pts[i]
                val b = pts[(i + 1) % 4]
                val line = Line.through(a, b)
                val inward = if (line.distance(centre) > 0) 1f else -1f
                val strip = levels(luma, w, h, a, b, line, inward, reach).first
                if (abs(strip - paper) > abs(strip - background)) return null
            }
        }
        val support = supportSum / 4f
        if (!brightnessFound && support < 0.5f) return null
        return areaFrac * support * support
    }

    /**
     * Fits each side to the edge pixels along it (least squares, to a fraction of a pixel), then
     * takes the corners where the fitted sides meet. Sides on the picture's border, or with too few
     * edges to trust, stay where they were.
     */
    fun refine(q: Quad, edges: EdgeMap, luma: ByteArray): Quad {
        val w = edges.w
        val h = edges.h
        val pts = q.points
        val centre = Pt(pts.map { it.x }.average().toFloat(), pts.map { it.y }.average().toFloat())
        val lines = ArrayList<Line>(4)
        for (i in 0 until 4) {
            val a = pts[i]
            val b = pts[(i + 1) % 4]
            val side = Line.through(a, b)
            val inward = if (side.distance(centre) > 0) 1f else -1f
            lines.add(if (onBorder(a, b, w, h)) side else fitSide(edges, luma, a, b, side, inward, max(2f, 0.02f * hypot(w.toFloat(), h.toFloat()))) ?: side)
        }
        // Side i runs from corner i to corner i+1, so corner i is where sides i-1 and i meet.
        val corners = (0 until 4).map { lines[(it + 3) % 4].intersect(lines[it]) ?: return q }
        val r = Quad(corners[0], corners[1], corners[2], corners[3]).clampedTo(w.toFloat(), h.toFloat())
        return if (r.isConvex() && r.differenceTo(q) < 0.06f) r else q
    }

    /**
     * The edge pixels near a side often form several parallel ridges: the page's edge, a ruled
     * border printed just inside it, a shadow. The one with the biggest brightness step across it
     * is the paper's edge, and the side is fitted to that ridge alone.
     */
    private fun fitSide(edges: EdgeMap, luma: ByteArray, a: Pt, b: Pt, side: Line, inward: Float, reach: Float): Line? {
        val len = a.dist(b)
        if (len < 4f) return null
        val ux = (b.x - a.x) / len
        val uy = (b.y - a.y) / len
        val sideAngle = normalAngle(side)
        val minCount = max(6, (0.25f * len).toInt())
        fun near(line: Line, r: Float): List<Int> = edges.points.filter { i ->
            val x = (i % edges.w) + 0.5f
            val y = (i / edges.w) + 0.5f
            val t = (x - a.x) * ux + (y - a.y) * uy
            t >= 0.04f * len && t <= 0.96f * len && abs(line.distance(Pt(x, y))) <= r && angleGap(edges.angle[i], sideAngle) <= 0.26f // ~15°
        }
        // Ridges: peaks in how many edge pixels lie at each distance from the side.
        val span = reach.toInt() + 1
        val hist = IntArray(2 * span + 1)
        for (i in near(side, reach)) hist[side.distance(Pt((i % edges.w) + 0.5f, (i / edges.w) + 0.5f)).roundToInt() + span]++
        val smooth = IntArray(hist.size) { k -> (if (k > 0) hist[k - 1] else 0) + hist[k] + (if (k < hist.size - 1) hist[k + 1] else 0) }
        val stepReach = stepReach(edges.w, edges.h)
        var ridge: Line? = null
        var bestStep = 7f
        for (k in smooth.indices) {
            if (smooth[k] < minCount) continue
            if ((k > 0 && smooth[k - 1] > smooth[k]) || (k < smooth.size - 1 && smooth[k + 1] >= smooth[k])) continue
            val shifted = Line(side.nx, side.ny, side.c + (k - span))
            val st = abs(step(luma, edges.w, edges.h, a, b, shifted, inward, stepReach))
            if (st > bestStep) {
                bestStep = st
                ridge = shifted
            }
        }
        var line = ridge ?: return null
        repeat(2) {
            val pts = near(line, 1.5f)
            if (pts.size < minCount) return null
            var sx = 0.0
            var sy = 0.0
            var sxx = 0.0
            var syy = 0.0
            var sxy = 0.0
            for (i in pts) {
                val x = (i % edges.w) + 0.5
                val y = (i / edges.w) + 0.5
                sx += x; sy += y; sxx += x * x; syy += y * y; sxy += x * y
            }
            val n = pts.size
            val mx = sx / n
            val my = sy / n
            val cxx = sxx / n - mx * mx
            val cyy = syy / n - my * my
            val cxy = sxy / n - mx * my
            val theta = 0.5 * atan2(2 * cxy, cxx - cyy) // direction of most spread; the normal is across it
            val nx = -sin(theta).toFloat()
            val ny = cos(theta).toFloat()
            line = Line(nx, ny, (nx * mx + ny * my).toFloat())
        }
        // Keep the original orientation sign so "inside" stays on the same side.
        return if (line.nx * side.nx + line.ny * side.ny < 0) Line(-line.nx, -line.ny, -line.c) else line
    }

    /**
     * The strongest straight lines among the edges (Hough transform, each edge voting only near its
     * own direction). Lines with about the same brightness on both sides, such as rows of text and
     * ruled lines, can't be the edge of the paper and are skipped.
     */
    internal fun lines(edges: EdgeMap, luma: ByteArray): List<Line> {
        val w = edges.w
        val h = edges.h
        val diag = sqrt((w * w + h * h).toFloat())
        val offset = diag.toInt() + 1
        val rhoBins = 2 * offset + 1
        val acc = IntArray(THETA_BINS * rhoBins)
        for (i in edges.points) {
            val x = (i % w) + 0.5f
            val y = (i / w) + 0.5f
            val k0 = (edges.angle[i] / PI.toFloat() * THETA_BINS).roundToInt()
            for (dk in -VOTE_SPREAD..VOTE_SPREAD) {
                val k = ((k0 + dk) % THETA_BINS + THETA_BINS) % THETA_BINS
                val r = (x * cosT[k] + y * sinT[k]).roundToInt() + offset
                acc[k * rhoBins + r]++
            }
        }
        val minVotes = max(12, (0.10f * min(w, h)).toInt())
        val peaks = ArrayList<Long>() // votes in the high bits, bin in the low bits, for sorting
        for (k in 0 until THETA_BINS) for (r in 1 until rhoBins - 1) {
            val v = acc[k * rhoBins + r]
            if (v < minVotes) continue
            var isMax = true
            loop@ for (dk in -1..1) for (dr in -1..1) {
                if (dk == 0 && dr == 0) continue
                val kk = ((k + dk) % THETA_BINS + THETA_BINS) % THETA_BINS
                if (acc[kk * rhoBins + r + dr] > v) { isMax = false; break@loop }
            }
            if (isMax) peaks.add((v.toLong() shl 32) or (k * rhoBins + r).toLong())
        }
        peaks.sortDescending()
        val out = ArrayList<Line>()
        val centre = Pt(w / 2f, h / 2f)
        for (p in peaks) {
            val bin = (p and 0xFFFFFFFFL).toInt()
            val k = bin / rhoBins
            val r = bin % rhoBins - offset
            val line = Line(cosT[k], sinT[k], r.toFloat())
            val dup = out.any { o ->
                val dot = o.nx * line.nx + o.ny * line.ny
                if (abs(dot) < 0.996f) return@any false // more than ~5° apart
                val foot = Pt(centre.x - o.distance(centre) * o.nx, centre.y - o.distance(centre) * o.ny)
                abs(line.distance(foot)) < 0.025f * diag
            }
            if (!dup && abs(lineStep(edges, luma, line)) >= 7f && quietAlong(edges, line)) out.add(line)
            if (out.size >= MAX_LINES) break
        }
        return out
    }

    /** The brightness step across a whole line, measured only where edges lie on it. */
    private fun lineStep(edges: EdgeMap, luma: ByteArray, line: Line): Float {
        val (a, b) = acrossPicture(edges, line) ?: return 0f
        val n = 80
        val sideAngle = normalAngle(line)
        val reach = stepReach(edges.w, edges.h)
        var sum = 0f
        var count = 0
        for (s in 0 until n) {
            val t = (s + 0.5f) / n
            val p = Pt(a.x + (b.x - a.x) * t, a.y + (b.y - a.y) * t)
            if (!edgeNear(edges, p, line, sideAngle)) continue
            sum += stepAt(luma, edges.w, edges.h, p, line, 1f, reach)
            count++
        }
        return if (count < 8) 0f else sum / count
    }

    /**
     * A row of text has more rows just beyond it on both sides; the edge of the paper has open
     * desk (or randomly oriented texture) on at least one. True when one side of the stretch from
     * a to b has few edges running parallel to it.
     */
    private fun quietBeside(edges: EdgeMap, a: Pt, b: Pt, line: Line, sideAngle: Float, reach: Float): Boolean {
        val from = (3 * reach).roundToInt()
        val to = (6 * reach).roundToInt()
        var busyPlus = 0
        var busyMinus = 0
        for (s in 0 until SAMPLES) {
            val t = 0.06f + 0.88f * s / (SAMPLES - 1)
            val q = Pt(a.x + (b.x - a.x) * t, a.y + (b.y - a.y) * t)
            val d = line.distance(q)
            val p = Pt(q.x - line.nx * d, q.y - line.ny * d)
            fun busy(dir: Int): Boolean {
                for (o in from..to) {
                    val x = (p.x + line.nx * o * dir).toInt()
                    val y = (p.y + line.ny * o * dir).toInt()
                    if (edges.isEdge(x, y) && angleGap(edges.angle[y * edges.w + x], sideAngle) < 0.35f) return true
                }
                return false
            }
            if (busy(1)) busyPlus++
            if (busy(-1)) busyMinus++
        }
        return min(busyPlus, busyMinus) < SAMPLES / 2
    }

    /** [quietBeside] over the stretch of a whole line that has edges on it. */
    private fun quietAlong(edges: EdgeMap, line: Line): Boolean {
        val (a, b) = extent(edges, line) ?: return false
        return quietBeside(edges, a, b, line, normalAngle(line), stepReach(edges.w, edges.h))
    }

    /** The first and last points of [line] inside the picture that have an edge on them. */
    private fun extent(edges: EdgeMap, line: Line): Pair<Pt, Pt>? {
        val (a, b) = acrossPicture(edges, line) ?: return null
        val sideAngle = normalAngle(line)
        val n = 120
        var first = -1
        var last = -1
        for (s in 0 until n) {
            val t = (s + 0.5f) / n
            if (edgeNear(edges, Pt(a.x + (b.x - a.x) * t, a.y + (b.y - a.y) * t), line, sideAngle)) {
                if (first < 0) first = s
                last = s
            }
        }
        if (last - first < 8) return null
        fun at(s: Int) = Pt(a.x + (b.x - a.x) * (s + 0.5f) / n, a.y + (b.y - a.y) * (s + 0.5f) / n)
        return at(first) to at(last)
    }

    /** Where [line] enters and leaves the picture. */
    private fun acrossPicture(edges: EdgeMap, line: Line): Pair<Pt, Pt>? {
        val w = edges.w.toFloat()
        val h = edges.h.toFloat()
        val ends = listOf(Line(1f, 0f, 0f), Line(1f, 0f, w), Line(0f, 1f, 0f), Line(0f, 1f, h))
            .mapNotNull { line.intersect(it) }
            .filter { it.x >= -0.5f && it.x <= w + 0.5f && it.y >= -0.5f && it.y <= h + 0.5f }
        if (ends.size < 2) return null
        var a = ends[0]
        var b = ends[1]
        for (p in ends) for (q in ends) if (p.dist(q) > a.dist(b)) { a = p; b = q }
        return a to b
    }

    private fun borders(w: Int, h: Int) = listOf(
        Line(0f, 1f, 0f, border = true), Line(1f, 0f, w.toFloat(), border = true),
        Line(0f, 1f, h.toFloat(), border = true), Line(1f, 0f, 0f, border = true),
    )

    private fun onBorder(a: Pt, b: Pt, w: Int, h: Int): Boolean {
        val e = 1.5f
        return (a.x <= e && b.x <= e) || (a.y <= e && b.y <= e) || (a.x >= w - e && b.x >= w - e) || (a.y >= h - e && b.y >= h - e)
    }

    private fun normalAngle(l: Line): Float {
        var a = atan2(l.ny, l.nx)
        if (a < 0f) a += PI.toFloat()
        if (a >= PI.toFloat()) a -= PI.toFloat()
        return a
    }

    /** Difference between two undirected angles in [0, π), in radians. */
    private fun angleGap(a: Float, b: Float): Float {
        val d = abs(a - b)
        return min(d, PI.toFloat() - d)
    }

    private fun edgeNear(edges: EdgeMap, p: Pt, line: Line, sideAngle: Float): Boolean {
        for (o in -2..2) {
            val x = (p.x + line.nx * o).toInt()
            val y = (p.y + line.ny * o).toInt()
            if (edges.isEdge(x, y) && angleGap(edges.angle[y * edges.w + x], sideAngle) < 0.35f) return true // ~20°
        }
        return false
    }

    private fun stepReach(w: Int, h: Int) = max(2f, 0.008f * min(w, h))

    /** Average brightness inside minus outside, just across [line] along the stretch from a to b. */
    private fun step(luma: ByteArray, w: Int, h: Int, a: Pt, b: Pt, line: Line, inward: Float, reach: Float): Float =
        levels(luma, w, h, a, b, line, inward, reach).let { it.first - it.second }

    /** Average brightness just inside and just outside [line], along the stretch from a to b. */
    private fun levels(luma: ByteArray, w: Int, h: Int, a: Pt, b: Pt, line: Line, inward: Float, reach: Float): Pair<Float, Float> {
        var inside = 0f
        var outside = 0f
        for (s in 0 until SAMPLES) {
            val t = 0.06f + 0.88f * s / (SAMPLES - 1)
            val q = Pt(a.x + (b.x - a.x) * t, a.y + (b.y - a.y) * t)
            val d = line.distance(q)
            val p = Pt(q.x - line.nx * d, q.y - line.ny * d) // onto the line: it may be a shifted copy of the side
            inside += look(luma, w, h, p, line, inward, reach)
            outside += look(luma, w, h, p, line, -inward, reach)
        }
        return inside / SAMPLES to outside / SAMPLES
    }

    private fun stepAt(luma: ByteArray, w: Int, h: Int, p: Pt, line: Line, inward: Float, reach: Float): Float =
        (look(luma, w, h, p, line, inward, reach) - look(luma, w, h, p, line, -inward, reach)).toFloat()

    /** Brightness a little way from p, to one side of [line]: the median of three distances, so a thin printed line next to the edge doesn't spoil it. */
    private fun look(luma: ByteArray, w: Int, h: Int, p: Pt, line: Line, dir: Float, reach: Float): Int {
        fun at(k: Float) = luma[(p.y + line.ny * reach * k * dir).toInt().coerceIn(0, h - 1) * w + (p.x + line.nx * reach * k * dir).toInt().coerceIn(0, w - 1)].toInt() and 0xFF
        val v0 = at(1f)
        val v1 = at(2f)
        val v2 = at(3f)
        return max(min(v0, v1), min(max(v0, v1), v2))
    }
}
