package app.docveta.android.scan

import java.io.ByteArrayOutputStream
import java.io.OutputStream

/** One scanned page: a JPEG, its size in pixels, and the words read on it (if the phone read them). */
class PdfPageImage(val jpeg: ByteArray, val width: Int, val height: Int, val gray: Boolean = false, val words: List<PdfWord> = emptyList())

/** A word and its box in the page's pixels; [last] ends its line (no space after it). */
data class PdfWord(val text: String, val x: Float, val y: Float, val w: Float, val h: Float, val last: Boolean = false)

/**
 * Writes a PDF that holds one JPEG per page, full-page, with the JPEG bytes stored as-is
 * (so a scan stays as small as its JPEGs). Words read on the phone go on top as invisible text,
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
            obj {
                it.ascii("<< /Type /XObject /Subtype /Image /Width ${p.width} /Height ${p.height} /ColorSpace /${if (p.gray) "DeviceGray" else "DeviceRGB"} /BitsPerComponent 8 /Filter /DCTDecode /Length ${p.jpeg.size} >>\nstream\n")
                it.write(p.jpeg)
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
