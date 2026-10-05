package app.docveta.android.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.outlined.Close
import androidx.compose.material3.Button
import androidx.compose.material3.Checkbox
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.unit.dp
import app.docveta.android.data.Taxonomy
import kotlinx.coroutines.launch

val taxonomyTitles = mapOf("tags" to "Tags", "correspondents" to "Who is it from?", "document-types" to "What is it?")

/**
 * Pick tags, correspondents or document types of a space: search, tick, and (optionally) create
 * a new one from what was typed. Single choice closes on tap.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun TaxonomyPicker(
    kind: String,
    spaceId: String?,
    selected: List<String>,
    multi: Boolean,
    title: String = taxonomyTitles[kind] ?: kind,
    allowCreate: Boolean = spaceId != null,
    onDismiss: () -> Unit,
    onDone: (List<Taxonomy>) -> Unit,
) {
    val c = LocalContainer.current
    var items by remember { mutableStateOf<List<Taxonomy>?>(null) }
    var picked by remember { mutableStateOf(selected) }
    var q by remember { mutableStateOf("") }
    var busy by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    val scope = rememberCoroutineScope()
    LaunchedEffect(kind, spaceId) { items = runCatching { c.repo.taxonomy(kind, spaceId) }.getOrDefault(emptyList()) }
    val all = items.orEmpty()
    val shown = all.filter { it.name.contains(q.trim(), ignoreCase = true) }
    val exact = all.any { it.name.equals(q.trim(), ignoreCase = true) }
    ModalBottomSheet(onDismissRequest = onDismiss, sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true)) {
        Column(Modifier.padding(horizontal = 16.dp).padding(bottom = 16.dp).imePadding()) {
            Text(title, style = MaterialTheme.typography.titleLarge)
            Spacer(Modifier.height(10.dp))
            OutlinedTextField(q, { q = it }, Modifier.fillMaxWidth(), singleLine = true, placeholder = { Text(if (allowCreate) "Search or create" else "Search") })
            if (error != null) Text(error!!, color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodySmall)
            Spacer(Modifier.height(6.dp))
            LazyColumn(Modifier.weight(1f, fill = false).height(380.dp)) {
                if (allowCreate && spaceId != null && q.isNotBlank() && !exact) item {
                    Row(Modifier.fillMaxWidth().clickable(enabled = !busy) {
                        busy = true
                        scope.launch {
                            runCatching { c.repo.createTaxonomy(kind, spaceId, q.trim()) }.onSuccess { t ->
                                items = all + t
                                picked = if (multi) picked + t.id else listOf(t.id)
                                q = ""
                                if (!multi) onDone(listOf(t))
                            }.onFailure { error = it.friendly() }
                            busy = false
                        }
                    }.padding(vertical = 14.dp), verticalAlignment = Alignment.CenterVertically) {
                        Text("Create “${q.trim()}”", color = MaterialTheme.colorScheme.primary, style = MaterialTheme.typography.titleSmall)
                    }
                }
                if (items == null) item { LoadingBox(Modifier.fillMaxWidth().height(80.dp)) }
                else if (shown.isEmpty() && q.isBlank()) item { Text("Nothing here yet.", Modifier.padding(vertical = 16.dp), color = MaterialTheme.colorScheme.onSurfaceVariant) }
                items(shown, key = { it.id }) { t ->
                    val on = t.id in picked
                    Row(Modifier.fillMaxWidth().clickable {
                        if (multi) picked = if (on) picked - t.id else picked + t.id
                        else onDone(listOf(t))
                    }.padding(vertical = 12.dp), verticalAlignment = Alignment.CenterVertically) {
                        if (multi) Checkbox(on, null) else if (on) Icon(Icons.Filled.Check, null, tint = MaterialTheme.colorScheme.primary) else Spacer(Modifier.width(24.dp))
                        Spacer(Modifier.width(12.dp))
                        if (kind == "tags") {
                            Box(Modifier.size(9.dp).clip(CircleShape).background(colorFor(t.color)))
                            Spacer(Modifier.width(8.dp))
                        }
                        Text(t.name, Modifier.weight(1f), style = MaterialTheme.typography.bodyLarge)
                        Text(t.documentCount.toString(), style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                }
            }
            if (multi) Button({ onDone(all.filter { it.id in picked }) }, Modifier.fillMaxWidth()) { Text("Done") }
        }
    }
}

/** A labelled box that shows the chosen names and opens a picker. */
@Composable
fun PickerField(label: String, names: List<String>, placeholder: String, enabled: Boolean = true, onClear: (() -> Unit)? = null, onClick: () -> Unit) {
    Column(FieldPad.clip(RoundedCornerShape(12.dp)).clickable(enabled = enabled, onClick = onClick).background(MaterialTheme.colorScheme.surfaceContainer).padding(horizontal = 14.dp, vertical = 10.dp)) {
        Text(label, style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(names.joinToString(", ").ifBlank { placeholder }, Modifier.weight(1f), style = MaterialTheme.typography.bodyLarge, color = if (names.isEmpty()) MaterialTheme.colorScheme.onSurfaceVariant else MaterialTheme.colorScheme.onSurface)
            if (onClear != null && names.isNotEmpty()) IconButton(onClear, Modifier.size(28.dp)) { Icon(Icons.Outlined.Close, "Clear", Modifier.size(18.dp)) }
        }
    }
}

/** Names for ids, looked up once per space (for showing a workflow's or a filter's choices). */
@Composable
fun rememberNames(kind: String, spaceId: String?): Map<String, String> {
    val c = LocalContainer.current
    var map by remember(kind, spaceId) { mutableStateOf<Map<String, String>>(emptyMap()) }
    LaunchedEffect(kind, spaceId) { map = runCatching { c.repo.taxonomy(kind, spaceId) }.getOrDefault(emptyList()).associate { it.id to it.name } }
    return map
}
