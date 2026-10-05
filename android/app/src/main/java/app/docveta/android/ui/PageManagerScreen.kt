package app.docveta.android.ui

import android.graphics.Bitmap
import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.itemsIndexed
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.ArrowBack
import androidx.compose.material.icons.automirrored.outlined.ArrowForward
import androidx.compose.material.icons.automirrored.outlined.RotateLeft
import androidx.compose.material.icons.automirrored.outlined.RotateRight
import androidx.compose.material.icons.outlined.ContentCut
import androidx.compose.material.icons.outlined.DeleteOutline
import androidx.compose.material3.Button
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.produceState
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import app.docveta.android.data.Document
import app.docveta.android.data.downloadFile
import app.docveta.android.data.editPages
import app.docveta.android.data.split
import coil.compose.AsyncImage
import java.io.File

/** One page while arranging: its page number in the current file, and how far it's turned. */
private data class PageItem(val key: Int, val rotate: Int = 0)

/**
 * Arrange pages: turn, delete, reorder, and copy pages into a new document. Saving writes a new
 * version of the file (the old one stays under Versions). Pictures can only be turned.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun PageManagerScreen(id: String, onBack: () -> Unit) {
    val c = LocalContainer.current
    val ctx = LocalContext.current
    val run = rememberRunner()
    val confirm = rememberConfirmation()
    val loaded = rememberLoader(id) {
        val d = c.repo.document(id)
        d to c.repo.downloadFile(d, "original")
    }
    val pair = loaded.data
    if (pair == null) {
        Page("Arrange pages", onBack) { if (loaded.error != null) ErrorState(loaded.error!!) { loaded.reload() } else LoadingBox(Modifier.fillMaxWidth().padding(64.dp)) }
        return
    }
    val (doc, file) = pair
    val pdf = remember(file) { if (doc.isPdf) runCatching { PdfPages(file) }.getOrNull() else null }
    DisposableEffect(pdf) { onDispose { pdf?.close() } }
    val original = remember(pdf) { if (pdf != null) (1..pdf.count).map { PageItem(it) } else listOf(PageItem(1)) }
    var items by remember(original) { mutableStateOf(original) }
    var picked by remember { mutableStateOf<Set<Int>>(emptySet()) }
    val changed = items != original

    fun rotate(key: Int, by: Int) { items = items.map { if (it.key == key) it.copy(rotate = ((it.rotate + by) % 360 + 360) % 360) else it } }
    fun move(i: Int, d: Int) { val j = i + d; if (j in items.indices) items = items.toMutableList().also { val t = it[i]; it[i] = it[j]; it[j] = t } }
    fun remove(keys: Set<Int>) { if (items.size - keys.size >= 1) items = items.filter { it.key !in keys }; picked = emptySet() }

    Column(Modifier.fillMaxSize().navigationBarsPadding()) {
        TopAppBar(
            title = { Column { Text("Arrange pages"); Text(doc.title, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1) } },
            navigationIcon = { BackButton { if (changed) confirm.ask("Leave without saving?", "Your page changes are lost.", "Leave", true, onBack) else onBack() } },
            actions = {
                if (changed) TextButton({ items = original; picked = emptySet() }) { Text("Reset") }
                TextButton({
                    run.run("Pages saved. The previous file is kept under Versions.") { c.repo.editPages(doc.id, items.map { it.key to it.rotate }); onBack() }
                }, enabled = changed && !run.busy) { Text("Save") }
            },
        )
        Text(
            if (doc.isPdf) "Turn, reorder or delete pages. Tap pages to select them, then copy them into a new document." else "Turn the picture the right way up.",
            Modifier.padding(horizontal = 16.dp, vertical = 4.dp), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        if (doc.isPdf && picked.isNotEmpty()) Row(Modifier.fillMaxWidth().padding(horizontal = 12.dp, vertical = 4.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Button({
                val nums = items.filter { it.key in picked }.map { it.key }.sorted()
                run.run("${plural(nums.size, "page")} copied into a new document") { c.repo.split(doc.id, listOf(nums.joinToString(","))); picked = emptySet() }
            }, enabled = !run.busy && !changed) { Icon(Icons.Outlined.ContentCut, null, Modifier.size(18.dp)); Text(" New document from ${picked.size}") }
            OutlinedButton({ remove(picked) }) { Text("Delete ${picked.size}") }
        }
        if (doc.isPdf && picked.isNotEmpty() && changed) Text("Save your other changes first to copy pages.", Modifier.padding(horizontal = 16.dp), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        LazyVerticalGrid(GridCells.Fixed(2), Modifier.fillMaxSize(), contentPadding = PaddingValues(12.dp), horizontalArrangement = Arrangement.spacedBy(12.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            itemsIndexed(items, key = { _, it -> it.key }) { i, it ->
                val on = it.key in picked
                Column(
                    Modifier.clip(RoundedCornerShape(12.dp)).border(BorderStroke(if (on) 2.dp else 1.dp, if (on) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.outlineVariant), RoundedCornerShape(12.dp))
                        .background(MaterialTheme.colorScheme.surface).padding(6.dp),
                ) {
                    Box(Modifier.fillMaxWidth().aspectRatio(0.75f).clip(RoundedCornerShape(6.dp)).background(MaterialTheme.colorScheme.surfaceVariant).clickable(enabled = doc.isPdf) { picked = if (on) picked - it.key else picked + it.key }, contentAlignment = Alignment.Center) {
                        val turned = it.rotate % 180 != 0
                        val m = Modifier.fillMaxSize().padding(4.dp).graphicsLayer { rotationZ = it.rotate.toFloat(); val s = if (turned) 0.75f else 1f; scaleX = s; scaleY = s }
                        if (pdf != null) PageThumb(pdf, it.key - 1, m) else AsyncImage(file, null, m, contentScale = ContentScale.Fit)
                        Text("${i + 1}", Modifier.align(Alignment.TopStart).padding(4.dp).clip(RoundedCornerShape(4.dp)).background(Color(0x99000000)).padding(horizontal = 6.dp), color = Color.White, style = MaterialTheme.typography.labelSmall)
                    }
                    Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceEvenly) {
                        IconButton({ rotate(it.key, -90) }, Modifier.size(36.dp)) { Icon(Icons.AutoMirrored.Outlined.RotateLeft, "Turn left", Modifier.size(20.dp)) }
                        IconButton({ rotate(it.key, 90) }, Modifier.size(36.dp)) { Icon(Icons.AutoMirrored.Outlined.RotateRight, "Turn right", Modifier.size(20.dp)) }
                        if (doc.isPdf) {
                            IconButton({ move(i, -1) }, Modifier.size(36.dp), enabled = i > 0) { Icon(Icons.AutoMirrored.Outlined.ArrowBack, "Move earlier", Modifier.size(20.dp)) }
                            IconButton({ move(i, 1) }, Modifier.size(36.dp), enabled = i < items.lastIndex) { Icon(Icons.AutoMirrored.Outlined.ArrowForward, "Move later", Modifier.size(20.dp)) }
                            IconButton({ remove(setOf(it.key)) }, Modifier.size(36.dp), enabled = items.size > 1) { Icon(Icons.Outlined.DeleteOutline, "Delete page", Modifier.size(20.dp)) }
                        }
                    }
                }
            }
        }
    }
}

@Composable
private fun PageThumb(pdf: PdfPages, index: Int, modifier: Modifier) {
    val bmp by produceState<Bitmap?>(null, pdf, index) { value = runCatching { pdf.render(index, 360) }.getOrNull() }
    bmp?.let { Image(it.asImageBitmap(), "Page ${index + 1}", modifier, contentScale = ContentScale.Fit) }
}
