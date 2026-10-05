package app.docveta.android

import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import app.docveta.android.scan.OcrModels
import app.docveta.android.scan.OnnxOcr
import app.docveta.android.scan.Raster
import java.util.zip.GZIPInputStream
import org.junit.Assert.assertTrue
import org.junit.Assume.assumeTrue
import org.junit.Test
import org.junit.runner.RunWith

/**
 * Reading text on a real phone or emulator: ONNX Runtime's Android build and the models in the
 * app's assets. `./gradlew connectedDebugAndroidTest`; skipped for a build made without models.
 */
@RunWith(AndroidJUnit4::class)
class PhoneOcrDeviceTest {
    @Test
    fun readsAPrintedPageWithTheBundledModels() {
        val ctx = InstrumentationRegistry.getInstrumentation().targetContext
        assumeTrue("this build has no OCR models", OcrModels.present(ctx))
        val scripts = OcrModels.available(ctx)
        assertTrue(scripts.toString(), "en" in scripts && "devanagari" in scripts)
        val bytes = GZIPInputStream(javaClass.getResourceAsStream("/ocr-page.pgm.gz")!!).readBytes()
        val header = String(bytes, 0, 32, Charsets.US_ASCII).split(Regex("\\s+"))
        val w = header[1].toInt()
        val h = header[2].toInt()
        val start = bytes.size - w * h
        val page = Raster(w, h, IntArray(w * h) { val v = bytes[start + it].toInt() and 0xFF; (0xFF shl 24) or (v shl 16) or (v shl 8) or v })
        OnnxOcr({ OcrModels.model(ctx, it) }, { OcrModels.dict(ctx, it) }, scripts).use { ocr ->
            val t0 = System.nanoTime()
            val text = ocr.readPage(page, listOf("en")).joinToString(" ") { it.text }
            android.util.Log.i("PhoneOcrDeviceTest", "read in ${(System.nanoTime() - t0) / 1_000_000} ms: $text")
            for (word in listOf("Invoice", "4821", "1,842.50", "March", "भारत")) assertTrue("'$word' not in: $text", word in text)
        }
    }
}
