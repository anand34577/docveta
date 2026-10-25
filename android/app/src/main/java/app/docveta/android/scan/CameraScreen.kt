package app.docveta.android.scan

import android.Manifest
import android.content.pm.PackageManager
import android.os.Handler
import android.os.Looper
import android.view.ViewGroup
import androidx.activity.compose.BackHandler
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.PickVisualMediaRequest
import androidx.activity.result.contract.ActivityResultContracts
import androidx.camera.core.AspectRatio
import androidx.camera.core.Camera
import androidx.camera.core.CameraSelector
import androidx.camera.core.FocusMeteringAction
import androidx.camera.core.ImageAnalysis
import androidx.camera.core.ImageCapture
import androidx.camera.core.ImageCaptureException
import androidx.camera.core.Preview
import androidx.camera.lifecycle.ProcessCameraProvider
import androidx.camera.view.PreviewView
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.AutoMode
import androidx.compose.material.icons.outlined.Close
import androidx.compose.material.icons.outlined.FlashOff
import androidx.compose.material.icons.outlined.FlashOn
import androidx.compose.material.icons.outlined.PhotoLibrary
import androidx.compose.material3.Button
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.compose.ui.unit.dp
import androidx.compose.ui.viewinterop.AndroidView
import androidx.core.content.ContextCompat
import app.docveta.android.ui.LocalContainer
import coil.compose.AsyncImage
import java.io.File
import java.util.UUID
import java.util.concurrent.Executors
import java.util.concurrent.atomic.AtomicLong
import android.util.Log
import androidx.lifecycle.Lifecycle
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow

/** What the camera currently sees: the page outline in the (upright) analysis picture's coordinates. */
private data class Seen(val quad: Quad?, val w: Int, val h: Int, val steady: Boolean)

/**
 * The scanner camera: finds the page on every frame, outlines it live, and (if wanted) takes the
 * picture by itself once the page has been held still for a moment. A shot whose edges couldn't be
 * found opens [onEdit] straight away, so the corners can be placed by hand.
 */
@Composable
fun CameraScreen(session: ScanSession, onDone: () -> Unit, onEdit: (String) -> Unit, onClose: () -> Unit) {
    val ctx = LocalContext.current
    val c = LocalContainer.current
    var granted by remember { mutableStateOf(ContextCompat.checkSelfPermission(ctx, Manifest.permission.CAMERA) == PackageManager.PERMISSION_GRANTED) }
    // Refused for good ("Don't ask again", or twice): Android no longer shows the question, so the
    // button opens the app's settings instead of doing nothing.
    var blocked by remember { mutableStateOf(false) }
    val ask = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) { ok ->
        granted = ok
        blocked = !ok && (ctx as? android.app.Activity)?.let { !androidx.core.app.ActivityCompat.shouldShowRequestPermissionRationale(it, Manifest.permission.CAMERA) } == true
    }
    val gallery = rememberLauncherForActivityResult(ActivityResultContracts.PickMultipleVisualMedia()) { uris -> uris.forEach { session.addUri(it, ctx.contentResolver) } }
    LaunchedEffect(Unit) { if (!granted) ask.launch(Manifest.permission.CAMERA) }
    // Back from the settings: the camera may be allowed now.
    val owner = LocalLifecycleOwner.current
    DisposableEffect(owner) {
        val observer = androidx.lifecycle.LifecycleEventObserver { _, event ->
            if (event == Lifecycle.Event.ON_RESUME && !granted) granted = ContextCompat.checkSelfPermission(ctx, Manifest.permission.CAMERA) == PackageManager.PERMISSION_GRANTED
        }
        owner.lifecycle.addObserver(observer)
        onDispose { owner.lifecycle.removeObserver(observer) }
    }

    Box(Modifier.fillMaxSize().background(Color.Black)) {
        if (granted) LiveCamera(session, c.session.autoCapture, onDone, onEdit, onClose, onPickGallery = { gallery.launch(PickVisualMediaRequest(ActivityResultContracts.PickVisualMedia.ImageOnly)) })
        else Column(Modifier.align(Alignment.Center).padding(32.dp), horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.spacedBy(14.dp)) {
            Text("Docveta needs the camera to scan documents.", color = Color.White, style = MaterialTheme.typography.bodyLarge)
            if (blocked) Button({
                runCatching { ctx.startActivity(android.content.Intent(android.provider.Settings.ACTION_APPLICATION_DETAILS_SETTINGS, android.net.Uri.fromParts("package", ctx.packageName, null))) }
            }) { Text("Open settings") }
            else Button({ ask.launch(Manifest.permission.CAMERA) }) { Text("Allow camera") }
            if (blocked) Text("Allow the camera under Permissions, then come back.", color = Color.White.copy(alpha = 0.8f), style = MaterialTheme.typography.bodyMedium)
            androidx.compose.material3.OutlinedButton({ gallery.launch(PickVisualMediaRequest(ActivityResultContracts.PickVisualMedia.ImageOnly)) }) { Text("Choose photos instead") }
            androidx.compose.material3.TextButton(onClose) { Text("Close", color = Color.White) }
        }
    }
}

@Composable
private fun LiveCamera(session: ScanSession, autoDefault: Boolean, onDone: () -> Unit, onEdit: (String) -> Unit, onClose: () -> Unit, onPickGallery: () -> Unit) {
    val ctx = LocalContext.current
    val container = LocalContainer.current
    val lifecycle = LocalLifecycleOwner.current
    val main = remember { Handler(Looper.getMainLooper()) }
    val seen = remember { MutableStateFlow(Seen(null, 0, 0, false)) }
    val seenNow by seen.collectAsState()
    var camera by remember { mutableStateOf<Camera?>(null) }
    var torch by remember { mutableStateOf(false) }
    var auto by remember { mutableStateOf(autoDefault) }
    var capturing by remember { mutableStateOf(false) }
    var flash by remember { mutableStateOf(false) }
    val capture = remember { ImageCapture.Builder().setCaptureMode(ImageCapture.CAPTURE_MODE_MINIMIZE_LATENCY).setTargetAspectRatio(AspectRatio.RATIO_4_3).build() }
    val previewView = remember { PreviewView(ctx).apply { scaleType = PreviewView.ScaleType.FIT_CENTER; keepScreenOn = true; layoutParams = ViewGroup.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.MATCH_PARENT) } }
    val tracker = remember { QuadTracker(stillFrames = 7) }
    var lastShot by remember { mutableStateOf(0L) }
    // Leaving the screen closes the camera, and CameraX drops a picture still being taken: Done,
    // the thumbnail and Back wait for the page to be added, then go.
    var leaving by remember { mutableStateOf<(() -> Unit)?>(null) }
    fun leave(go: () -> Unit) { if (capturing) leaving = go else go() }
    BackHandler(enabled = capturing) { leaving = onClose }

    fun shoot() {
        if (capturing) return
        capturing = true
        val file = File(ctx.cacheDir, "scans/shot-${UUID.randomUUID()}.jpg").also { it.parentFile?.mkdirs() }
        capture.takePicture(ImageCapture.OutputFileOptions.Builder(file).build(), ContextCompat.getMainExecutor(ctx), object : ImageCapture.OnImageSavedCallback {
            override fun onImageSaved(r: ImageCapture.OutputFileResults) {
                lastShot = System.currentTimeMillis()
                flash = true
                tracker.reset()
                session.addFile(file) { page ->
                    capturing = false
                    val go = leaving.also { leaving = null }
                    if (go != null) go() else if (!page.detected) onEdit(page.id)
                }
                main.postDelayed({ flash = false }, 120)
            }

            override fun onError(e: ImageCaptureException) {
                capturing = false
                leaving = null // stay, so the message is seen
                session.error = "The camera couldn't take the picture"
            }
        })
    }

    // When a frame was last looked at; the watchdog below starts the camera again if frames stop coming.
    val lastFrame = remember { AtomicLong(0L) }
    var rebinds by remember { mutableStateOf(0) }
    DisposableEffect(lifecycle, rebinds) {
        val executor = Executors.newSingleThreadExecutor()
        val future = ProcessCameraProvider.getInstance(ctx)
        var disposed = false
        future.addListener({
            // Left (or restarted) before the camera was ready: binding now would keep it running
            // behind other screens, feeding frames to a finder that has been shut down.
            if (disposed) return@addListener
            val provider = future.get()
            val preview = Preview.Builder().setTargetAspectRatio(AspectRatio.RATIO_4_3).build().also { it.setSurfaceProvider(previewView.surfaceProvider) }
            val analysis = ImageAnalysis.Builder().setTargetAspectRatio(AspectRatio.RATIO_4_3).setBackpressureStrategy(ImageAnalysis.STRATEGY_KEEP_ONLY_LATEST).build()
            var lastRun = 0L
            analysis.setAnalyzer(executor) { image ->
                try {
                    val now = System.currentTimeMillis()
                    lastFrame.set(now)
                    if (now - lastRun >= 120) {
                        lastRun = now
                        // In colour, for the model; turned the way the person holds the phone.
                        val frame = image.toBitmap()
                        val up = PageFilters.rotate(ImageIO.toRaster(frame), image.imageInfo.rotationDegrees / 90)
                        frame.recycle()
                        val q = tracker.update(PageFinder.page(PageFinder.get(ctx), up))
                        seen.value = Seen(q, up.w, up.h, tracker.isSteady)
                    }
                } catch (e: Throwable) {
                    // One bad frame (e.g. short of memory) mustn't end page finding: skip it.
                    Log.w("Scanner", "Couldn't look for the page in a frame", e)
                } finally {
                    image.close()
                }
            }
            lastFrame.set(System.currentTimeMillis())
            provider.unbindAll()
            // A phone or tablet without a back camera (the app installs without one), or a camera
            // another app holds: say so rather than crash. Photos can still be chosen.
            camera = try {
                provider.bindToLifecycle(lifecycle, CameraSelector.DEFAULT_BACK_CAMERA, preview, capture, analysis)
            } catch (e: Exception) {
                Log.w("Scanner", "Couldn't start the camera", e)
                session.error = "The camera couldn't be started. You can choose photos instead."
                null
            }
        }, ContextCompat.getMainExecutor(ctx))
        onDispose {
            disposed = true
            if (future.isDone) runCatching { future.get().unbindAll() }
            executor.shutdownNow()
        }
    }
    // Watchdog: on some phones frames stop reaching the page finder after many shots, with no
    // error; the outline then froze until the app was restarted. Start the camera again instead.
    LaunchedEffect(lifecycle) {
        while (true) {
            delay(1000)
            val resumed = lifecycle.lifecycle.currentState.isAtLeast(Lifecycle.State.RESUMED)
            if (resumed && !capturing && camera != null && System.currentTimeMillis() - lastFrame.get() > 4000) {
                Log.w("Scanner", "No camera frames for 4 s; starting the camera again")
                lastFrame.set(System.currentTimeMillis())
                tracker.reset()
                seen.value = Seen(null, 0, 0, false)
                rebinds++
            }
        }
    }
    LaunchedEffect(torch, camera) { camera?.cameraControl?.enableTorch(torch) }
    LaunchedEffect(session.error) { if (session.error != null) { capturing = false; leaving = null } } // the shot couldn't be used
    LaunchedEffect(seenNow.steady, auto) {
        if (auto && seenNow.steady && !capturing && System.currentTimeMillis() - lastShot > 2500) shoot()
    }

    Box(Modifier.fillMaxSize()) {
        AndroidView({ previewView }, Modifier.fillMaxSize())
        // Tap to focus, and the page outline.
        Canvas(Modifier.fillMaxSize().pointerInput(camera) {
            detectTapGestures { p ->
                camera?.cameraControl?.startFocusAndMetering(FocusMeteringAction.Builder(previewView.meteringPointFactory.createPoint(p.x, p.y)).build())
            }
        }) {
            val q = seenNow.quad ?: return@Canvas
            if (seenNow.w == 0) return@Canvas
            val k = minOf(size.width / seenNow.w, size.height / seenNow.h)
            val ox = (size.width - seenNow.w * k) / 2f
            val oy = (size.height - seenNow.h * k) / 2f
            fun m(p: Pt) = Offset(ox + p.x * k, oy + p.y * k)
            val path = Path().apply {
                moveTo(m(q.tl).x, m(q.tl).y); lineTo(m(q.tr).x, m(q.tr).y); lineTo(m(q.br).x, m(q.br).y); lineTo(m(q.bl).x, m(q.bl).y); close()
            }
            val colour = if (seenNow.steady) Color(0xFF22C55E) else Color(0xFF6C8CFF)
            drawPath(path, colour.copy(alpha = 0.18f))
            drawPath(path, colour, style = Stroke(width = 4.dp.toPx()))
            q.points.forEach { drawCircle(colour, 6.dp.toPx(), m(it)) }
        }
        if (flash) Box(Modifier.fillMaxSize().background(Color.White.copy(alpha = 0.7f)))

        // Top bar
        Row(Modifier.fillMaxWidth().statusBarsPadding().padding(horizontal = 8.dp, vertical = 4.dp), verticalAlignment = Alignment.CenterVertically) {
            RoundIcon(Icons.Outlined.Close, "Close") { session.discard(); onClose() }
            Spacer(Modifier.weight(1f))
            // One document, or a pile: every page (or every front and back) its own document.
            Text(
                when (session.sides) { 1 -> "Pile: 1 page each"; 2 -> "Pile: front + back"; else -> "One document" },
                Modifier.clip(RoundedCornerShape(50)).background(if (session.sides > 0) MaterialTheme.colorScheme.primary else Color.Black.copy(alpha = 0.45f))
                    .clickable { session.chooseSides((session.sides + 1) % 3) }.padding(horizontal = 14.dp, vertical = 10.dp),
                color = if (session.sides > 0) MaterialTheme.colorScheme.onPrimary else Color.White, style = MaterialTheme.typography.labelLarge,
            )
            Spacer(Modifier.width(8.dp))
            RoundIcon(Icons.Outlined.AutoMode, if (auto) "Automatic capture is on" else "Automatic capture is off", active = auto) { auto = !auto; container.session.autoCapture = auto }
            Spacer(Modifier.width(8.dp))
            RoundIcon(if (torch) Icons.Outlined.FlashOn else Icons.Outlined.FlashOff, "Flashlight", active = torch) { torch = !torch }
        }
        // Hint
        val which = when {
            session.sides == 2 -> "Document ${session.doc + 1} · ${if (session.pagesInDoc == 0) "front" else "back"} · "
            session.sides == 1 || session.doc > 0 -> "Document ${session.doc + 1} · "
            else -> ""
        }
        Text(
            which + when {
                capturing -> "Saving…"
                seenNow.quad == null -> "Fit the whole page in view"
                seenNow.steady -> if (auto) "Got it" else "Tap the button"
                else -> if (auto) "Hold steady…" else "Page found"
            },
            Modifier.align(Alignment.TopCenter).statusBarsPadding().padding(top = 64.dp).clip(RoundedCornerShape(50)).background(Color.Black.copy(alpha = 0.55f)).padding(horizontal = 14.dp, vertical = 6.dp),
            color = Color.White, style = MaterialTheme.typography.labelLarge,
        )
        // Bottom bar
        if (session.sides != 1 && session.pagesInDoc > 0 && !capturing) Text(
            "Next document",
            Modifier.align(Alignment.BottomCenter).navigationBarsPadding().padding(bottom = 112.dp).clip(RoundedCornerShape(50))
                .background(Color.Black.copy(alpha = 0.55f)).border(1.dp, Color.White.copy(alpha = 0.6f), RoundedCornerShape(50))
                .clickable { session.nextDocument() }.padding(horizontal = 16.dp, vertical = 8.dp),
            color = Color.White, style = MaterialTheme.typography.labelLarge,
        )
        Row(Modifier.align(Alignment.BottomCenter).fillMaxWidth().navigationBarsPadding().padding(horizontal = 20.dp, vertical = 18.dp), verticalAlignment = Alignment.CenterVertically) {
            Box(Modifier.weight(1f), contentAlignment = Alignment.CenterStart) {
                val last = session.pages.lastOrNull()
                if (last == null) RoundIcon(Icons.Outlined.PhotoLibrary, "Choose photos", onClick = onPickGallery)
                else Box(Modifier.size(56.dp).clip(RoundedCornerShape(12.dp)).border(2.dp, Color.White, RoundedCornerShape(12.dp)).clickable(onClickLabel = "Review the pages", role = Role.Button) { leave(onDone) }
                    .semantics(mergeDescendants = true) { contentDescription = "${session.pages.size} page${if (session.pages.size == 1) "" else "s"} scanned" }) {
                    AsyncImage(last.file, null, Modifier.fillMaxSize(), contentScale = androidx.compose.ui.layout.ContentScale.Crop)
                    Text(session.pages.size.toString(), Modifier.align(Alignment.TopEnd).background(MaterialTheme.colorScheme.primary, CircleShape).padding(horizontal = 6.dp), color = MaterialTheme.colorScheme.onPrimary, style = MaterialTheme.typography.labelSmall)
                }
            }
            Box(Modifier.size(76.dp).clip(CircleShape).border(4.dp, Color.White, CircleShape).padding(6.dp).clip(CircleShape).background(if (capturing || session.working) Color.Gray else Color.White).clickable(enabled = !capturing, role = Role.Button) { shoot() }
                .semantics { contentDescription = "Take picture" })
            Box(Modifier.weight(1f), contentAlignment = Alignment.CenterEnd) {
                if (session.pages.isNotEmpty()) Button({ leave(onDone) }, shape = RoundedCornerShape(50)) { Text(session.docs.size.let { if (it > 1) "Done ($it)" else "Done" }) }
                else Spacer(Modifier.size(1.dp))
            }
        }
        session.error?.let { msg ->
            Text(msg, Modifier.align(Alignment.Center).clip(RoundedCornerShape(12.dp)).background(Color.Black.copy(alpha = 0.8f)).clickable { session.error = null }.padding(16.dp), color = Color.White)
        }
    }
}

@Composable
private fun RoundIcon(icon: androidx.compose.ui.graphics.vector.ImageVector, desc: String, active: Boolean = false, onClick: () -> Unit) {
    IconButton(onClick, Modifier.size(44.dp).clip(CircleShape).background(if (active) MaterialTheme.colorScheme.primary else Color.Black.copy(alpha = 0.45f))) {
        Icon(icon, desc, tint = if (active) MaterialTheme.colorScheme.onPrimary else Color.White)
    }
}
