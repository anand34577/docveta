package app.docveta.android.scan

import java.io.ByteArrayOutputStream
import java.io.OutputStream

/** One scanned page: a JPEG and its size in pixels. */
class PdfPageImage(val jpeg: ByteArray, val width: Int, val height: Int, val gray: Boolean = false)

/**
 * Writes a PDF that holds one JPEG per page, full-page, with the JPEG bytes stored as-is
 * (so a scan stays as small as its JPEGs). Docveta then reads the text from it like any other scan.
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
        // Object layout: 1 catalog, 2 pages, 3 info, then per page: page, content, image.
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
                it.ascii("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 ${fmt(pw)} ${fmt(ph)}] /Resources << /XObject << /Im0 ${pageNo + 2} 0 R >> >> /Contents ${pageNo + 1} 0 R >>")
            }
            val content = "q ${fmt(pw)} 0 0 ${fmt(ph)} 0 0 cm /Im0 Do Q".toByteArray(Charsets.US_ASCII)
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
