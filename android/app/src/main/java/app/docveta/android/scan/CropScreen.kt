package app.docveta.android.scan

import android.graphics.Bitmap
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.gestures.detectDragGestures
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.RotateLeft
import androidx.compose.material.icons.automirrored.outlined.RotateRight
import androidx.compose.material.icons.outlined.AutoFixHigh
import androidx.compose.material.icons.outlined.CropFree
import androidx.compose.material.icons.outlined.Visibility
import androidx.compose.material3.Button
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.TopAppBarDefaults
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.ClipOp
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.graphics.drawscope.clipPath
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.unit.IntOffset
import androidx.compose.ui.unit.IntSize
import androidx.compose.ui.unit.dp
import app.docveta.android.ui.BackButton
import kotlin.math.min
import kotlinx.coroutines.launch

val FilterLabels = listOf(
    PageFilters.Kind.ORIGINAL to "Original",
    PageFilters.Kind.ENHANCED to "Enhanced",
    PageFilters.Kind.GRAY to "Grey",
    PageFilters.Kind.BLACK_WHITE to "Black & white",
)

/**
 * Adjust where the page is: four corner handles over the photo, a magnifier while dragging, and
 * "Auto" to let Docveta have another go. Nothing changes the photo itself; only the corners are saved.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun CropScreen(session: ScanSession, pageId: String, onBack: () -> Unit) {
    val page = session.pages.firstOrNull { it.id == pageId }
    if (page == null) {
        LaunchedEffect(Unit) { onBack() }
        return
    }
    val scope = rememberCoroutineScope()
    var quad by remember(pageId) { mutableStateOf(page.quad) }
    var image by remember(pageId) { mutableStateOf<ImageBitmap?>(null) }
    var scale by remember(pageId) { mutableStateOf(1f) } // preview pixels per original pixel
    var preview by remember { mutableStateOf<Bitmap?>(null) }
    var showing by remember { mutableStateOf(false) }
    var active by remember { mutableStateOf(-1) }
    var finding by remember { mutableStateOf(false) }
    var turns by remember(pageId) { mutableStateOf(page.turns) }
    var filter by remember(pageId) { mutableStateOf(page.filter) }

    LaunchedEffect(pageId) {
        val bmp = kotlinx.coroutines.withContext(kotlinx.coroutines.Dispatchers.Default) { ImageIO.decodeUpright(page.file, 1600) }
        scale = bmp.width.toFloat() / page.width
        image = bmp.asImageBitmap()
    }
    // The result preview follows the current corners, filter and turn.
    LaunchedEffect(showing, quad, filter, turns) {
        if (showing) preview = session.preview(page.copy(quad = quad, filter = filter, turns = turns), 900)
    }

    fun commit() {
        session.update(pageId) { it.copy(quad = quad, turns = turns, filter = filter) }
        session.chooseFilterForNew(filter)
    }

    Column(Modifier.fillMaxSize().background(Color(0xFF111318))) {
        TopAppBar(
            title = { Text(if (showing) "Result" else "Adjust the corners") }, navigationIcon = { BackButton { onBack() } },
            colors = TopAppBarDefaults.topAppBarColors(containerColor = Color.Transparent, titleContentColor = Color.White, navigationIconContentColor = Color.White, actionIconContentColor = Color.White),
            actions = { TextButton({ commit(); onBack() }) { Text("Done", color = Color.White) } },
        )
        Box(Modifier.weight(1f).fillMaxWidth()) {
            val img = image
            if (showing) {
                preview?.let { androidx.compose.foundation.Image(it.asImageBitmap(), null, Modifier.fillMaxSize().padding(16.dp), contentScale = androidx.compose.ui.layout.ContentScale.Fit) }
            } else if (img != null) {
                BoxWithConstraints(Modifier.fillMaxSize().padding(horizontal = 20.dp, vertical = 16.dp)) {
                    val boxW = constraints.maxWidth.toFloat()
                    val boxH = constraints.maxHeight.toFloat()
                    val k = min(boxW / img.width, boxH / img.height) // box pixels per preview pixel
                    val ox = (boxW - img.width * k) / 2f
                    val oy = (boxH - img.height * k) / 2f
                    // Corner positions in box pixels.
                    fun toBox(p: Pt) = Offset(ox + p.x * scale * k, oy + p.y * scale * k)
                    fun fromBox(o: Offset) = Pt(((o.x - ox) / (scale * k)).coerceIn(0f, page.width.toFloat()), ((o.y - oy) / (scale * k)).coerceIn(0f, page.height.toFloat()))
                    Canvas(
                        Modifier.fillMaxSize().pointerInput(pageId, k, ox, oy, scale) {
                            val grab = 56.dp.toPx()
                            detectDragGestures(
                                onDragStart = { start ->
                                    val pts = quad.points.map { toBox(it) }
                                    val i = pts.indices.minBy { (pts[it] - start).getDistance() }
                                    active = if ((pts[i] - start).getDistance() <= grab * 1.6f) i else -1
                                },
                                onDragEnd = { active = -1 },
                                onDragCancel = { active = -1 },
                                onDrag = { change, delta ->
                                    if (active >= 0) {
                                        change.consume()
                                        val moved = quad.moved(active, fromBox(toBox(quad.points[active]) + delta))
                                        if (moved.isConvex()) quad = moved // a bow-tie isn't a page
                                    }
                                },
                            )
                        },
                    ) {
                        drawImage(img, dstOffset = IntOffset(ox.toInt(), oy.toInt()), dstSize = IntSize((img.width * k).toInt(), (img.height * k).toInt()))
                        val path = Path().apply {
                            val p = quad.points.map { toBox(it) }
                            moveTo(p[0].x, p[0].y); p.drop(1).forEach { lineTo(it.x, it.y) }; close()
                        }
                        // Dim everything outside the page.
                        clipPath(path, ClipOp.Difference) { drawRect(Color.Black.copy(alpha = 0.55f), Offset(ox, oy), Size(img.width * k, img.height * k)) }
                        drawPath(path, Color(0xFF6C8CFF), style = Stroke(width = 2.5.dp.toPx()))
                        quad.points.forEachIndexed { i, pt ->
                            val c = toBox(pt)
                            drawCircle(Color.White, 14.dp.toPx(), c)
                            drawCircle(if (i == active) Color(0xFF22C55E) else Color(0xFF4F6BED), 10.dp.toPx(), c)
                        }
                        // Magnifier: shows the area under the finger, in the corner farthest from it.
                        if (active >= 0) {
                            val c = toBox(quad.points[active])
                            val r = 56.dp.toPx()
                            val left = c.x < size.width / 2
                            val centre = Offset(if (left) size.width - r - 8.dp.toPx() else r + 8.dp.toPx(), r + 8.dp.toPx())
                            val circle = Path().apply { addOval(androidx.compose.ui.geometry.Rect(centre, r)) }
                            clipPath(circle) {
                                val zoom = 3f
                                val src = c - Offset(ox, oy)
                                val half = r / zoom
                                val sx = (src.x / k - half / k).toInt().coerceIn(0, (img.width - 1))
                                val sy = (src.y / k - half / k).toInt().coerceIn(0, (img.height - 1))
                                val sw = min((2 * half / k).toInt().coerceAtLeast(1), img.width - sx)
                                val sh = min((2 * half / k).toInt().coerceAtLeast(1), img.height - sy)
                                drawRect(Color.Black, centre - Offset(r, r), Size(2 * r, 2 * r))
                                drawImage(img, IntOffset(sx, sy), IntSize(sw, sh), IntOffset((centre.x - r).toInt(), (centre.y - r).toInt()), IntSize((2 * r).toInt(), (2 * r).toInt()))
                            }
                            drawCircle(Color.White, r, centre, style = Stroke(width = 3.dp.toPx()))
                            drawLine(Color(0xFF4F6BED), Offset(centre.x - 8.dp.toPx(), centre.y), Offset(centre.x + 8.dp.toPx(), centre.y), 2.dp.toPx())
                            drawLine(Color(0xFF4F6BED), Offset(centre.x, centre.y - 8.dp.toPx()), Offset(centre.x, centre.y + 8.dp.toPx()), 2.dp.toPx())
                        }
                    }
                }
            }
        }
        // Filters
        LazyRow(contentPadding = androidx.compose.foundation.layout.PaddingValues(horizontal = 16.dp, vertical = 4.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            items(FilterLabels) { (k, label) ->
                FilterChip(filter == k, { filter = k; showing = true }, label = { Text(label) })
            }
        }
        // Tools
        Row(Modifier.fillMaxWidth().navigationBarsPadding().padding(horizontal = 8.dp, vertical = 6.dp), horizontalArrangement = Arrangement.SpaceEvenly, verticalAlignment = Alignment.CenterVertically) {
            Tool(Icons.Outlined.AutoFixHigh, if (finding) "Looking…" else "Auto") {
                finding = true
                scope.launch {
                    session.redetect(pageId)?.let { quad = it } ?: run { session.error = "Couldn't find the page edges. Drag the corners instead." }
                    finding = false
                    showing = false
                }
            }
            Tool(Icons.Outlined.CropFree, "Full photo") { quad = Quad.full(page.width.toFloat(), page.height.toFloat()); showing = false }
            Tool(Icons.AutoMirrored.Outlined.RotateLeft, "Turn left") { turns = (turns + 3) % 4; showing = true }
            Tool(Icons.AutoMirrored.Outlined.RotateRight, "Turn right") { turns = (turns + 1) % 4; showing = true }
            Tool(Icons.Outlined.Visibility, if (showing) "Corners" else "Preview") { showing = !showing }
        }
        session.error?.let { msg -> Text(msg, Modifier.fillMaxWidth().background(Color(0xFF3A1D22)).clickableDismiss { session.error = null }.padding(12.dp), color = Color.White, style = MaterialTheme.typography.bodySmall) }
    }
}

private fun Modifier.clickableDismiss(onClick: () -> Unit) = this.then(Modifier.pointerInput(Unit) { detectTapGestures { onClick() } })

@Composable
private fun Tool(icon: androidx.compose.ui.graphics.vector.ImageVector, label: String, onClick: () -> Unit) {
    Column(Modifier.pointerInput(label) { detectTapGestures { onClick() } }.padding(horizontal = 10.dp, vertical = 4.dp), horizontalAlignment = Alignment.CenterHorizontally) {
        Icon(icon, label, tint = Color.White, modifier = Modifier.size(24.dp))
        Text(label, color = Color.White, style = MaterialTheme.typography.labelSmall)
    }
}
