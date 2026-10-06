package app.docveta.android.scan

import java.io.ByteArrayOutputStream
import java.io.OutputStream
import java.util.zip.Deflater
import java.util.zip.DeflaterOutputStream

/**
 * One scanned page: its picture, its size in pixels, and the words read on it (if the phone read
 * them). The picture is a JPEG, or with [flate] raw pixels (see [PdfWriter.lossless]) of [bits] each.
 */
class PdfPageImage(val data: ByteArray, val width: Int, val height: Int, val gray: Boolean = false, val words: List<PdfWord> = emptyList(), val flate: Boolean = false, val bits: Int = 8)

/** A word and its box in the page's pixels; [last] ends its line (no space after it). */
data class PdfWord(val text: String, val x: Float, val y: Float, val w: Float, val h: Float, val last: Boolean = false)

/**
 * Writes a PDF that holds one picture per page, full-page, with the JPEG (or Flate) bytes stored
 * as-is (so a scan stays as small as its pictures). Words read on the phone go on top as invisible text,
 * like any searchable scan: selectable and searchable, and used by the server instead of reading
 * the page again. Without words, Docveta reads the text from it like any other scan.
 */
object PdfWriter {
    /** [dpi] only decides the printed size of the page (150 dpi: a 1240 x 1754 px picture is A5-ish, 2480 x 3508 is A4). */
    fun write(pages: List<PdfPageImage>, out: OutputStream, title: String = "", dpi: Int = 200) {
        require(pages.isNotEmpty()) { "no pages" }
        val w = CountingStream(out)
        val offsets = ArrayList<Long>() // offsets[i] = byte offset of object i+1

        fun obj(body: (CountingStream) -> Unit) {
            offsets.add(w.count)
            w.ascii("${offsets.size} 0 obj\n")
            body(w)
            w.ascii("\nendobj\n")
        }

        w.ascii("%PDF-1.4\n%âãÏÓ\n")
        // Object layout: 1 catalog, 2 pages, 3 info, then per page: page, content, image; then the
        // font for the text (when there is any).
        val font = 4 + pages.size * 3
        val hasText = pages.any { it.words.isNotEmpty() }
        obj { it.ascii("<< /Type /Catalog /Pages 2 0 R >>") }
        obj { s ->
            val kids = pages.indices.joinToString(" ") { "${4 + it * 3} 0 R" }
            s.ascii("<< /Type /Pages /Kids [$kids] /Count ${pages.size} >>")
        }
        obj { it.ascii("<< /Producer (Docveta) /Title (${escape(title)}) >>") }
        for ((i, p) in pages.withIndex()) {
            val pw = p.width * 72.0 / dpi
            val ph = p.height * 72.0 / dpi
            val pageNo = 4 + i * 3
            obj {
                val fonts = if (p.words.isNotEmpty()) " /Font << /F1 $font 0 R >>" else ""
                it.ascii("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 ${fmt(pw)} ${fmt(ph)}] /Resources << /XObject << /Im0 ${pageNo + 2} 0 R >>$fonts >> /Contents ${pageNo + 1} 0 R >>")
            }
            val content = ("q ${fmt(pw)} 0 0 ${fmt(ph)} 0 0 cm /Im0 Do Q" + textLayer(p, ph, dpi)).toByteArray(Charsets.US_ASCII)
            obj {
                it.ascii("<< /Length ${content.size} >>\nstream\n")
                it.write(content)
                it.ascii("\nendstream")
            }
            val filter = when {
                !p.flate -> "/DCTDecode"
                p.bits == 1 -> "/FlateDecode"
                else -> "/FlateDecode /DecodeParms << /Predictor 15 /Colors ${if (p.gray) 1 else 3} /BitsPerComponent 8 /Columns ${p.width} >>"
            }
            obj {
                it.ascii("<< /Type /XObject /Subtype /Image /Width ${p.width} /Height ${p.height} /ColorSpace /${if (p.gray) "DeviceGray" else "DeviceRGB"} /BitsPerComponent ${p.bits} /Filter $filter /Length ${p.data.size} >>\nstream\n")
                it.write(p.data)
                it.ascii("\nendstream")
            }
        }
        if (hasText) {
            // A Type 0 font with no glyphs of its own (the text is invisible): character codes are
            // UTF-16 code units, mapped back to themselves for copying and search.
            obj { it.ascii("<< /Type /Font /Subtype /Type0 /BaseFont /GlyphLessFont /Encoding /Identity-H /DescendantFonts [${font + 1} 0 R] /ToUnicode ${font + 3} 0 R >>") }
            obj { it.ascii("<< /Type /Font /Subtype /CIDFontType2 /BaseFont /GlyphLessFont /CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) /Supplement 0 >> /FontDescriptor ${font + 2} 0 R /DW 500 /CIDToGIDMap /Identity >>") }
            obj { it.ascii("<< /Type /FontDescriptor /FontName /GlyphLessFont /Flags 5 /FontBBox [0 0 500 1000] /ItalicAngle 0 /Ascent 1000 /Descent 0 /CapHeight 1000 /StemV 80 >>") }
            val cmap = TO_UNICODE.toByteArray(Charsets.US_ASCII)
            obj {
                it.ascii("<< /Length ${cmap.size} >>\nstream\n")
                it.write(cmap)
                it.ascii("\nendstream")
            }
        }
        val xref = w.count
        w.ascii("xref\n0 ${offsets.size + 1}\n0000000000 65535 f \n")
        for (o in offsets) w.ascii(String.format(java.util.Locale.US, "%010d 00000 n \n", o))
        w.ascii("trailer\n<< /Size ${offsets.size + 1} /Root 1 0 R /Info 3 0 R >>\nstartxref\n$xref\n%%EOF\n")
        w.flush()
    }

    /**
     * The page pixel for pixel, Flate-compressed the way PDF stores it: 1 bit per pixel when it is
     * only black and white (the B&W filter; far smaller than a JPEG and sharper), 8-bit gray when it
     * has no colour, RGB otherwise. Gray and colour rows use the PNG "Up" predictor, which lets
     * Flate squeeze the even paper between lines.
     */
    fun lossless(r: Raster): PdfPageImage {
        var gray = true
        var bw = true
        for (p in r.px) {
            val g = (p shr 8) and 0xFF
            if ((p shr 16) and 0xFF != g || p and 0xFF != g) { gray = false; bw = false; break }
            if (g != 0 && g != 255) bw = false
        }
        val out = ByteArrayOutputStream()
        DeflaterOutputStream(out, Deflater(Deflater.BEST_COMPRESSION)).use { z ->
            if (bw) {
                val row = ByteArray((r.w + 7) / 8)
                for (y in 0 until r.h) {
                    row.fill(0)
                    for (x in 0 until r.w) if (r.px[y * r.w + x] and 0xFF != 0) row[x shr 3] = (row[x shr 3].toInt() or (0x80 ushr (x and 7))).toByte()
                    z.write(row)
                }
            } else {
                val n = if (gray) 1 else 3
                var prev = ByteArray(r.w * n)
                var cur = ByteArray(r.w * n)
                val row = ByteArray(1 + r.w * n).also { it[0] = 2 } // 2 = Up
                for (y in 0 until r.h) {
                    for (x in 0 until r.w) {
                        val p = r.px[y * r.w + x]
                        if (gray) cur[x] = p.toByte() else { cur[x * 3] = (p shr 16).toByte(); cur[x * 3 + 1] = (p shr 8).toByte(); cur[x * 3 + 2] = p.toByte() }
                    }
                    for (i in cur.indices) row[i + 1] = (cur[i] - prev[i]).toByte()
                    z.write(row)
                    prev = cur.also { cur = prev }
                }
            }
        }
        return PdfPageImage(out.toByteArray(), r.w, r.h, gray, flate = true, bits = if (bw) 1 else 8)
    }

    fun toBytes(pages: List<PdfPageImage>, title: String = "", dpi: Int = 200): ByteArray {
        val b = ByteArrayOutputStream()
        write(pages, b, title, dpi)
        return b.toByteArray()
    }

    /** Invisible text (render mode 3) over the picture, each word stretched to its box. */
    private fun textLayer(p: PdfPageImage, pageHeight: Double, dpi: Int): String {
        if (p.words.isEmpty()) return ""
        val k = 72.0 / dpi
        val sb = StringBuilder(" BT 3 Tr")
        for (word in p.words) {
            val text = if (word.last) word.text else word.text + " "
            if (word.text.isBlank() || word.h <= 0f || word.w <= 0f) continue
            val size = word.h * k
            val scale = 100.0 * (word.w * k) / (word.text.length * 0.5 * size)
            val x = word.x * k
            val y = pageHeight - (word.y + word.h) * k
            sb.append(" /F1 ").append(fmt(size)).append(" Tf ").append(fmt(scale)).append(" Tz 1 0 0 1 ").append(fmt(x)).append(' ').append(fmt(y)).append(" Tm <")
            for (ch in text) sb.append(String.format(java.util.Locale.US, "%04X", ch.code))
            sb.append("> Tj")
        }
        return sb.append(" ET").toString()
    }

    private const val TO_UNICODE = "/CIDInit /ProcSet findresource begin 12 dict begin begincmap " +
        "/CIDSystemInfo << /Registry (Adobe) /Ordering (UCS) /Supplement 0 >> def /CMapName /Adobe-Identity-UCS def /CMapType 2 def " +
        "1 begincodespacerange <0000> <FFFF> endcodespacerange 1 beginbfrange <0000> <FFFF> <0000> endbfrange " +
        "endcmap CMapName currentdict /CMap defineresource pop end end"

    private fun fmt(d: Double) = String.format(java.util.Locale.US, "%.2f", d)
    private fun escape(s: String) = s.filter { it.code in 32..126 }.replace("\\", "\\\\").replace("(", "\\(").replace(")", "\\)")

    private class CountingStream(private val inner: OutputStream) : OutputStream() {
        var count = 0L
            private set

        override fun write(b: Int) {
            inner.write(b)
            count++
        }

        override fun write(b: ByteArray, off: Int, len: Int) {
            inner.write(b, off, len)
            count += len
        }

        override fun flush() = inner.flush()
        fun ascii(s: String) = write(s.toByteArray(Charsets.ISO_8859_1))
    }
}
