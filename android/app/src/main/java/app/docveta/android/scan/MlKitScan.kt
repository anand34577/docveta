package app.docveta.android.scan

import android.app.Activity
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.IntentSenderRequest
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.platform.LocalContext
import com.google.mlkit.vision.documentscanner.GmsDocumentScannerOptions
import com.google.mlkit.vision.documentscanner.GmsDocumentScanning
import com.google.mlkit.vision.documentscanner.GmsDocumentScanningResult

/**
 * Google's document scanner: finds the page edges live, crops, straightens and cleans each page,
 * and can import from the gallery. Its pages go into the same review (reorder, one PDF or
 * separate pictures, upload) as the built-in camera. Devices without Google Play services fall
 * back to the built-in scanner, [CameraScreen].
 */
@Composable
fun ScanEntry(session: ScanSession, onDone: () -> Unit, onClose: () -> Unit) {
    val ctx = LocalContext.current
    var fallback by rememberSaveable { mutableStateOf(false) }
    var started by rememberSaveable { mutableStateOf(false) }
    val launcher = rememberLauncherForActivityResult(ActivityResultContracts.StartIntentSenderForResult()) { r ->
        val uris = if (r.resultCode == Activity.RESULT_OK) GmsDocumentScanningResult.fromActivityResultIntent(r.data)?.pages?.map { it.imageUri }.orEmpty() else emptyList()
        when {
            uris.isNotEmpty() -> session.addScanned(uris, ctx.contentResolver, onDone)
            session.pages.isNotEmpty() -> onDone()
            else -> onClose()
        }
    }
    LaunchedEffect(fallback) {
        if (fallback || started) return@LaunchedEffect
        started = true
        val options = GmsDocumentScannerOptions.Builder()
            .setGalleryImportAllowed(true)
            .setPageLimit(30)
            .setResultFormats(GmsDocumentScannerOptions.RESULT_FORMAT_JPEG)
            .setScannerMode(GmsDocumentScannerOptions.SCANNER_MODE_FULL)
            .build()
        GmsDocumentScanning.getClient(options).getStartScanIntent(ctx as Activity)
            .addOnSuccessListener { launcher.launch(IntentSenderRequest.Builder(it).build()) }
            .addOnFailureListener { fallback = true }
    }
    if (fallback) CameraScreen(session, onDone, onClose)
}
