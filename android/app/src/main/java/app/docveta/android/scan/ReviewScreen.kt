package app.docveta.android.scan

import android.graphics.Bitmap
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.items
import androidx.compose.foundation.lazy.items as rowItems
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.ArrowBack
import androidx.compose.material.icons.outlined.AddAPhoto
import androidx.compose.material.icons.outlined.ArrowBack
import androidx.compose.material.icons.outlined.ArrowForward
import androidx.compose.material.icons.outlined.Crop
import androidx.compose.material.icons.outlined.Delete
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.SegmentedButton
import androidx.compose.material3.SegmentedButtonDefaults
import androidx.compose.material3.SingleChoiceSegmentedButtonRow
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.produceState
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.unit.dp
import app.docveta.android.ui.LocalMe
import app.docveta.android.ui.SpaceDot

/** The pages of a scan together: reorder, re-crop, choose a look, name it, pick the space, and send it. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ReviewScreen(session: ScanSession, onAddMore: () -> Unit, onEdit: (String) -> Unit, onUploaded: () -> Unit, onBack: () -> Unit) {
    val me = LocalMe.current
    val writable = me.spaces.filter { it.canWrite }
    LaunchedEffect(Unit) {
        if (session.spaceId == null) session.spaceId = (writable.firstOrNull { it.id == me.spaces.firstOrNull { s -> s.id == session.spaceId }?.id } ?: writable.firstOrNull { it.isPersonal } ?: writable.firstOrNull())?.id
    }
    var spaceMenu by remember { mutableStateOf(false) }
    Column(Modifier.fillMaxSize().imePadding()) {
        TopAppBar(
            title = { Text("${session.pages.size} page${if (session.pages.size == 1) "" else "s"}") },
            navigationIcon = { IconButton(onBack) { Icon(Icons.AutoMirrored.Outlined.ArrowBack, "Back to the camera") } },
            actions = { IconButton(onAddMore) { Icon(Icons.Outlined.AddAPhoto, "Add another page") } },
        )
        LazyVerticalGrid(GridCells.Adaptive(150.dp), Modifier.weight(1f), contentPadding = PaddingValues(12.dp), horizontalArrangement = Arrangement.spacedBy(12.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            items(session.pages, key = { it.id }) { p ->
                val idx = session.pages.indexOf(p)
                Column(Modifier.clip(RoundedCornerShape(14.dp)).background(MaterialTheme.colorScheme.surfaceContainer).padding(8.dp)) {
                    val bmp by produceState<Bitmap?>(null, p.id, p.quad, p.filter, p.turns) { value = runCatching { session.preview(p, 420) }.getOrNull() }
                    Box(Modifier.fillMaxWidth().aspectRatio(0.75f).clip(RoundedCornerShape(8.dp)).background(MaterialTheme.colorScheme.surfaceVariant).clickable { onEdit(p.id) }, contentAlignment = Alignment.Center) {
                        bmp?.let { Image(it.asImageBitmap(), "Page ${idx + 1}", Modifier.fillMaxSize(), contentScale = ContentScale.Fit) } ?: CircularProgressIndicator(Modifier.size(22.dp), strokeWidth = 2.dp)
                        Text("${idx + 1}", Modifier.align(Alignment.TopStart).padding(6.dp).background(Color0, RoundedCornerShape(6.dp)).padding(horizontal = 6.dp), color = androidx.compose.ui.graphics.Color.White, style = MaterialTheme.typography.labelSmall)
                        if (!p.detected) Text("Check edges", Modifier.align(Alignment.BottomCenter).padding(6.dp).background(androidx.compose.ui.graphics.Color(0xCCB77900), RoundedCornerShape(6.dp)).padding(horizontal = 6.dp, vertical = 2.dp), color = androidx.compose.ui.graphics.Color.White, style = MaterialTheme.typography.labelSmall)
                    }
                    Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween, verticalAlignment = Alignment.CenterVertically) {
                        IconButton({ session.move(p.id, -1) }, enabled = idx > 0, modifier = Modifier.size(36.dp)) { Icon(Icons.Outlined.ArrowBack, "Move earlier", Modifier.size(18.dp)) }
                        IconButton({ onEdit(p.id) }, Modifier.size(36.dp)) { Icon(Icons.Outlined.Crop, "Crop and filter", Modifier.size(18.dp)) }
                        IconButton({ session.remove(p.id); if (session.pages.isEmpty()) onBack() }, Modifier.size(36.dp)) { Icon(Icons.Outlined.Delete, "Delete page", Modifier.size(18.dp)) }
                        IconButton({ session.move(p.id, 1) }, enabled = idx < session.pages.lastIndex, modifier = Modifier.size(36.dp)) { Icon(Icons.Outlined.ArrowForward, "Move later", Modifier.size(18.dp)) }
                    }
                }
            }
        }
        Column(Modifier.fillMaxWidth().background(MaterialTheme.colorScheme.surfaceContainerLow).navigationBarsPadding().padding(horizontal = 16.dp, vertical = 12.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
            Text("Look of all pages", style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
            LazyRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                rowItems(FilterLabels) { (k, label) ->
                    FilterChip(session.pages.isNotEmpty() && session.pages.all { it.filter == k }, {
                        session.pages.forEach { p -> session.update(p.id) { it.copy(filter = k) } }
                        session.chooseFilterForNew(k)
                    }, label = { Text(label) })
                }
            }
            OutlinedTextField(session.title, { session.title = it }, Modifier.fillMaxWidth(), singleLine = true, label = { Text("Title") })
            if (writable.size > 1) {
                Box {
                    val current = writable.firstOrNull { it.id == session.spaceId }
                    OutlinedButton({ spaceMenu = true }, Modifier.fillMaxWidth()) {
                        SpaceDot(current)
                        Spacer(Modifier.width(8.dp))
                        Text("Save in ${current?.label ?: "…"}")
                    }
                    DropdownMenu(spaceMenu, { spaceMenu = false }) {
                        writable.forEach { s -> DropdownMenuItem(text = { Text(s.label) }, leadingIcon = { SpaceDot(s) }, onClick = { session.spaceId = s.id; spaceMenu = false }) }
                    }
                }
            }
            if (session.pages.size > 1) SingleChoiceSegmentedButtonRow(Modifier.fillMaxWidth()) {
                SegmentedButton(session.asPdf, { session.asPdf = true }, SegmentedButtonDefaults.itemShape(0, 2)) { Text("One PDF") }
                SegmentedButton(!session.asPdf, { session.asPdf = false }, SegmentedButtonDefaults.itemShape(1, 2)) { Text("Separate pages") }
            }
            Button({ session.submit(onUploaded) }, Modifier.fillMaxWidth().height(52.dp), enabled = !session.working && session.pages.isNotEmpty(), shape = RoundedCornerShape(14.dp)) {
                if (session.working) {
                    CircularProgressIndicator(Modifier.size(20.dp), strokeWidth = 2.dp, color = MaterialTheme.colorScheme.onPrimary)
                    Spacer(Modifier.width(10.dp))
                    Text("Preparing…")
                } else Text(if (session.asPdf || session.pages.size == 1) "Upload as PDF" else "Upload ${session.pages.size} pictures")
            }
            session.error?.let { Text(it, color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodySmall) }
        }
    }
}

private val Color0 = androidx.compose.ui.graphics.Color(0x99000000)
