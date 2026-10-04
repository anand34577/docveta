package app.docveta.android.scan

/** Camera frames arrive in the sensor's orientation; the detector wants the picture the way a person sees it. */
object Luma {
    /**
     * Copies the brightness plane of a camera frame into a tight array. [rowStride] may be wider than [w]
     * (padding at the end of each row) and [pixelStride] is normally 1.
     */
    fun tight(plane: ByteArray, w: Int, h: Int, rowStride: Int, pixelStride: Int = 1): ByteArray {
        val out = ByteArray(w * h)
        for (y in 0 until h) {
            val row = y * rowStride
            if (pixelStride == 1) System.arraycopy(plane, row, out, y * w, w)
            else for (x in 0 until w) out[y * w + x] = plane[row + x * pixelStride]
        }
        return out
    }

    /** Rotates by 0, 90, 180 or 270 degrees clockwise. Returns the new array and its width and height. */
    fun rotate(src: ByteArray, w: Int, h: Int, degrees: Int): Triple<ByteArray, Int, Int> = when (((degrees % 360) + 360) % 360) {
        90 -> {
            val out = ByteArray(w * h)
            for (y in 0 until h) for (x in 0 until w) out[x * h + (h - 1 - y)] = src[y * w + x]
            Triple(out, h, w)
        }
        180 -> {
            val out = ByteArray(w * h)
            for (i in out.indices) out[w * h - 1 - i] = src[i]
            Triple(out, w, h)
        }
        270 -> {
            val out = ByteArray(w * h)
            for (y in 0 until h) for (x in 0 until w) out[(w - 1 - x) * h + y] = src[y * w + x]
            Triple(out, h, w)
        }
        else -> Triple(src, w, h)
    }
}
