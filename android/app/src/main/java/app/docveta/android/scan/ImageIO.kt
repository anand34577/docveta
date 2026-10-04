package app.docveta.android.scan

import android.graphics.Bitmap
import android.graphics.BitmapFactory
import android.graphics.Matrix
import androidx.exifinterface.media.ExifInterface
import java.io.ByteArrayOutputStream
import java.io.File
import java.io.InputStream
import kotlin.math.max

/** The Android-only part of scanning: reading photos upright, and converting to and from [Raster]. */
object ImageIO {
    /** Decodes a photo, honouring its EXIF rotation, scaled down so neither side exceeds [maxSide]. */
    fun decodeUpright(open: () -> InputStream, orientation: Int, maxSide: Int): Bitmap {
        val bounds = BitmapFactory.Options().apply { inJustDecodeBounds = true }
        open().use { BitmapFactory.decodeStream(it, null, bounds) }
        var sample = 1
        while (max(bounds.outWidth, bounds.outHeight) / (sample * 2) >= maxSide) sample *= 2
        val opts = BitmapFactory.Options().apply { inSampleSize = sample }
        val raw = open().use { BitmapFactory.decodeStream(it, null, opts) } ?: throw IllegalArgumentException("Can't read this picture")
        var bmp = raw
        val longSide = max(bmp.width, bmp.height)
        if (longSide > maxSide) {
            val k = maxSide.toFloat() / longSide
            bmp = Bitmap.createScaledBitmap(bmp, (bmp.width * k).toInt().coerceAtLeast(1), (bmp.height * k).toInt().coerceAtLeast(1), true).also { if (it !== raw) raw.recycle() }
        }
        val m = Matrix()
        when (orientation) {
            ExifInterface.ORIENTATION_ROTATE_90 -> m.postRotate(90f)
            ExifInterface.ORIENTATION_ROTATE_180 -> m.postRotate(180f)
            ExifInterface.ORIENTATION_ROTATE_270 -> m.postRotate(270f)
            ExifInterface.ORIENTATION_FLIP_HORIZONTAL -> m.postScale(-1f, 1f)
            ExifInterface.ORIENTATION_FLIP_VERTICAL -> m.postScale(1f, -1f)
            ExifInterface.ORIENTATION_TRANSPOSE -> { m.postRotate(90f); m.postScale(-1f, 1f) }
            ExifInterface.ORIENTATION_TRANSVERSE -> { m.postRotate(270f); m.postScale(-1f, 1f) }
        }
        if (m.isIdentity) return bmp
        return Bitmap.createBitmap(bmp, 0, 0, bmp.width, bmp.height, m, true).also { if (it !== bmp) bmp.recycle() }
    }

    fun decodeUpright(file: File, maxSide: Int): Bitmap {
        val orientation = runCatching { ExifInterface(file.path).getAttributeInt(ExifInterface.TAG_ORIENTATION, ExifInterface.ORIENTATION_NORMAL) }.getOrDefault(ExifInterface.ORIENTATION_NORMAL)
        return decodeUpright({ file.inputStream() }, orientation, maxSide)
    }

    fun orientationOf(open: () -> InputStream): Int =
        runCatching { open().use { ExifInterface(it).getAttributeInt(ExifInterface.TAG_ORIENTATION, ExifInterface.ORIENTATION_NORMAL) } }.getOrDefault(ExifInterface.ORIENTATION_NORMAL)

    fun toRaster(b: Bitmap): Raster {
        val px = IntArray(b.width * b.height)
        b.getPixels(px, 0, b.width, 0, 0, b.width, b.height)
        return Raster(b.width, b.height, px)
    }

    fun toBitmap(r: Raster): Bitmap = Bitmap.createBitmap(r.px, r.w, r.h, Bitmap.Config.ARGB_8888)

    fun jpeg(b: Bitmap, quality: Int): ByteArray = ByteArrayOutputStream().also { b.compress(Bitmap.CompressFormat.JPEG, quality, it) }.toByteArray()

    fun saveJpeg(b: Bitmap, file: File, quality: Int = 92) {
        file.parentFile?.mkdirs()
        file.outputStream().use { b.compress(Bitmap.CompressFormat.JPEG, quality, it) }
    }
}
