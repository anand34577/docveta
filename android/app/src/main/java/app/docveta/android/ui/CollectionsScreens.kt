package app.docveta.android.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material.icons.outlined.Close
import androidx.compose.material.icons.outlined.CollectionsBookmark
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.AssistChip
import androidx.compose.material3.Checkbox
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.InputChip
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import app.docveta.android.data.DocCollection
import app.docveta.android.data.DocQuery
import app.docveta.android.data.Document
import app.docveta.android.data.addToCollection
import app.docveta.android.data.collections
import app.docveta.android.data.removeFromCollection
import app.docveta.android.data.saveCollection
import coil.compose.AsyncImage
import coil.request.ImageRequest
import kotlinx.coroutines.delay

/** "Only you" or "Shared with Family". */
@Composable
fun collectionSharedLabel(c: DocCollection): String {
    val me = LocalMe.current
    val s = c.spaceId?.let { id -> me.spaces.firstOrNull { it.id == id } }
    return if (s != null) "Shared with ${s.label}" else "Only you"
}

/** The first of its documents' thumbnails, or the collection's colour with an icon while it is empty. */
@Composable
private fun CollectionCover(c: DocCollection, modifier: Modifier = Modifier) {
    val repo = LocalContainer.current.repo
    val shape = RoundedCornerShape(10.dp)
    val first = c.covers.firstOrNull()
    Box(modifier.clip(shape).background(colorFor(c.color).copy(alpha = 0.18f)).border(0.5.dp, MaterialTheme.colorScheme.outlineVariant, shape), contentAlignment = Alignment.Center) {
        Icon(Icons.Outlined.CollectionsBookmark, null, tint = colorFor(c.color), modifier = Modifier.size(24.dp))
        if (first != null) {
            val url = repo.coverUrl(first)
            AsyncImage(
                model = ImageRequest.Builder(LocalContext.current).data(url).memoryCacheKey(url).diskCacheKey(url).build(),
                contentDescription = null, contentScale = ContentScale.Crop, alignment = Alignment.TopCenter, modifier = Modifier.fillMaxSize(),
            )
        }
    }
}

private val ideas = listOf("Car papers", "House", "Medical", "Travel", "Education", "Tax")

/** Every collection the person can see; tap one to open it. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
fun CollectionsScreen(onBack: () -> Unit, onOpen: (String) -> Unit) {
    val c = LocalContainer.current
    val list = rememberLoader { c.repo.collections() }
    var creating by remember { mutableStateOf<String?>(null) }
    Page("Collections", onBack, actions = { IconButton({ creating = "" }) { Icon(Icons.Outlined.Add, "New collection") } }, scroll = false) {
        Loaded(list) { all ->
            if (all.isEmpty()) {
                EmptyState(
                    Icons.Outlined.CollectionsBookmark, "No collections yet",
                    "Keep the documents that belong together in one place: the papers of your car, or everything for the house.",
                    Modifier.fillMaxWidth().padding(vertical = 32.dp),
                ) {
                    FlowRow(Modifier.padding(horizontal = 24.dp), horizontalArrangement = Arrangement.spacedBy(8.dp, Alignment.CenterHorizontally)) {
                        ideas.forEach { n -> AssistChip({ creating = n }, label = { Text(n) }) }
                    }
                }
            } else LazyColumn(Modifier.fillMaxSize()) {
                items(all, key = { it.id }) { col ->
                    Row(Modifier.fillMaxWidth().clickable { onOpen(col.id) }.padding(horizontal = 16.dp, vertical = 10.dp), verticalAlignment = Alignment.CenterVertically) {
                        CollectionCover(col, Modifier.size(52.dp))
                        Spacer(Modifier.width(14.dp))
                        Column(Modifier.weight(1f)) {
                            Text(col.name, style = MaterialTheme.typography.bodyLarge, maxLines = 1, overflow = TextOverflow.Ellipsis)
                            Text(
                                "${plural(col.documentCount, "document")} · ${collectionSharedLabel(col)}",
                                style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1, overflow = TextOverflow.Ellipsis,
                            )
                            if (col.description.isNotBlank()) Text(col.description, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1, overflow = TextOverflow.Ellipsis)
                        }
                    }
                    HorizontalDivider()
                }
            }
        }
    }
    creating?.let { name ->
        CollectionDialog(null, name, onDismiss = { creating = null }) { saved -> creating = null; list.reload(); onOpen(saved.id) }
    }
}

/** Name, description, colour and who sees it; creates a collection or edits [existing]. */
@Composable
fun CollectionDialog(existing: DocCollection?, initialName: String = "", onDismiss: () -> Unit, onSaved: (DocCollection) -> Unit) {
    val c = LocalContainer.current
    val me = LocalMe.current
    val run = rememberRunner()
    var name by remember { mutableStateOf(existing?.name ?: initialName) }
    var description by remember { mutableStateOf(existing?.description.orEmpty()) }
    var color by remember { mutableStateOf(existing?.color ?: "blue") }
    var space by remember { mutableStateOf(existing?.spaceId) }
    val shareable = me.spaces.filter { !it.isPersonal && (it.canWrite || it.id == existing?.spaceId) }
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(if (existing == null) "New collection" else "Edit collection") },
        text = {
            Column(Modifier.verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                OutlinedTextField(name, { if (it.length <= 80) name = it }, label = { Text("Name") }, placeholder = { Text("e.g. Car papers, House, Medical") }, singleLine = true, modifier = Modifier.fillMaxWidth())
                OutlinedTextField(description, { if (it.length <= 500) description = it }, label = { Text("Description (optional)") }, minLines = 2, modifier = Modifier.fillMaxWidth())
                Text("Colour", style = MaterialTheme.typography.labelLarge)
                ColorPicker(color, names = colorNames.filter { it != "lime" }) { color = it }
                if (shareable.isNotEmpty()) {
                    SelectField<String?>(
                        "Who sees it", listOf<Pair<String?, String>>(null to "Only me") + shareable.map { it.id to "Everyone in ${it.label}" }, space, Modifier.fillMaxWidth(),
                        supporting = "People in a space see the documents in it they can open; its editors can add and remove.",
                    ) { space = it }
                }
            }
        },
        confirmButton = {
            TextButton({ run.run { onSaved(c.repo.saveCollection(existing, name, description, color, space)) } }, enabled = name.isNotBlank() && !run.busy) {
                Text(if (existing == null) "Create" else "Save")
            }
        },
        dismissButton = { TextButton(onDismiss) { Text("Cancel") } },
    )
}

/** Pick a collection (or make one) for some documents. */
@Composable
fun AddToCollectionDialog(ids: List<String>, onDismiss: () -> Unit, onDone: () -> Unit = {}) {
    val c = LocalContainer.current
    val ctx = LocalContext.current
    val run = rememberRunner()
    val list = rememberLoader { c.repo.collections() }
    var creating by remember { mutableStateOf(false) }
    suspend fun add(col: DocCollection) {
        val n = c.repo.addToCollection(col.id, ids)
        toast(ctx, if (n > 0) "Added ${plural(n, "document")} to ${col.name}" else "Already in ${col.name}")
        onDone()
        onDismiss()
    }
    if (creating) {
        CollectionDialog(null, "", onDismiss = { creating = false }) { saved -> creating = false; run.run { add(saved) } }
        return
    }
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(if (ids.size == 1) "Add to a collection" else "Add ${ids.size} documents to a collection") },
        text = {
            val editable = list.data.orEmpty().filter { it.canEdit }
            Column(Modifier.verticalScroll(rememberScrollState())) {
                if (list.data == null && list.error == null) Text("Loading…", style = MaterialTheme.typography.bodyMedium)
                list.error?.let { Text(it, color = MaterialTheme.colorScheme.error) }
                if (list.data != null && editable.isEmpty()) Text("No collections yet. Make the first one.", style = MaterialTheme.typography.bodyMedium)
                editable.forEach { col ->
                    Row(Modifier.fillMaxWidth().clickable(enabled = !run.busy) { run.run { add(col) } }.padding(vertical = 8.dp), verticalAlignment = Alignment.CenterVertically) {
                        CollectionCover(col, Modifier.size(40.dp))
                        Spacer(Modifier.width(12.dp))
                        Column(Modifier.weight(1f)) {
                            Text(col.name, maxLines = 1, overflow = TextOverflow.Ellipsis)
                            Text("${plural(col.documentCount, "document")} · ${collectionSharedLabel(col)}", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1, overflow = TextOverflow.Ellipsis)
                        }
                    }
                }
            }
        },
        confirmButton = { TextButton({ creating = true }) { Text("New collection") } },
        dismissButton = { TextButton(onDismiss) { Text("Cancel") } },
    )
}

/** On a document: the collections it is in, with a way to add it to another. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
fun DocumentCollections(docId: String, canAdd: Boolean, onOpen: (String) -> Unit) {
    val c = LocalContainer.current
    val ctx = LocalContext.current
    val run = rememberRunner()
    val list = rememberLoader(docId) { runCatching { c.repo.collections(docId) }.getOrDefault(emptyList()) }
    var adding by remember { mutableStateOf(false) }
    Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text("Collections", Modifier.weight(1f), style = MaterialTheme.typography.titleSmall)
            if (canAdd) TextButton({ adding = true }) { Text("Add") }
        }
        val l = list.data.orEmpty()
        if (l.isEmpty()) {
            if (list.data != null) Text("Not in a collection. Group it with related documents, like all your car papers.", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        } else FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
            l.forEach { col ->
                InputChip(
                    false, { onOpen(col.id) }, label = { Text(col.name, maxLines = 1, overflow = TextOverflow.Ellipsis) },
                    leadingIcon = { Icon(Icons.Outlined.CollectionsBookmark, null, Modifier.size(16.dp), tint = colorFor(col.color)) },
                    trailingIcon = if (col.canEdit) ({
                        Icon(Icons.Outlined.Close, "Remove from ${col.name}", Modifier.size(16.dp).clickable {
                            run.run { c.repo.removeFromCollection(col.id, listOf(docId)); toast(ctx, "Removed from ${col.name}"); list.reload() }
                        })
                    }) else null,
                )
            }
        }
    }
    if (adding) AddToCollectionDialog(listOf(docId), onDismiss = { adding = false }, onDone = { list.reload() })
}

/** Search the library and tick documents to put in [collection]. */
@Composable
fun AddDocumentsDialog(collection: DocCollection, onDismiss: () -> Unit, onAdded: () -> Unit) {
    val c = LocalContainer.current
    val ctx = LocalContext.current
    val run = rememberRunner()
    var q by remember { mutableStateOf("") }
    var results by remember { mutableStateOf<List<Document>?>(null) }
    var inside by remember { mutableStateOf<Set<String>>(emptySet()) }
    var picked by remember { mutableStateOf<List<String>>(emptyList()) }
    LaunchedEffect(Unit) { inside = runCatching { c.repo.documents(DocQuery(collectionId = collection.id), limit = 200).items.map { it.id }.toSet() }.getOrDefault(emptySet()) }
    LaunchedEffect(q) {
        if (q.isNotBlank()) delay(300)
        results = runCatching { c.repo.documents(DocQuery(q = q, sort = if (q.isBlank()) "-added" else null), limit = 40).items }.getOrDefault(emptyList())
    }
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("Add to ${collection.name}") },
        text = {
            Column {
                OutlinedTextField(q, { q = it }, placeholder = { Text("Search your documents") }, singleLine = true, modifier = Modifier.fillMaxWidth())
                Spacer(Modifier.height(8.dp))
                val l = results
                when {
                    l == null -> Text("Loading…", style = MaterialTheme.typography.bodyMedium)
                    l.isEmpty() -> Text("No documents match.", style = MaterialTheme.typography.bodyMedium)
                    else -> LazyColumn(Modifier.height(320.dp)) {
                        items(l, key = { it.id }) { d ->
                            val isIn = d.id in inside
                            val on = d.id in picked
                            Row(
                                Modifier.fillMaxWidth().clickable(enabled = !isIn) { picked = if (on) picked - d.id else picked + d.id }.padding(vertical = 4.dp),
                                verticalAlignment = Alignment.CenterVertically,
                            ) {
                                Checkbox(on || isIn, { picked = if (it) picked + d.id else picked - d.id }, enabled = !isIn)
                                Column(Modifier.weight(1f)) {
                                    Text(d.title, maxLines = 1, overflow = TextOverflow.Ellipsis, style = MaterialTheme.typography.bodyMedium)
                                    if (isIn) Text("Already in this collection", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                                }
                            }
                        }
                    }
                }
            }
        },
        confirmButton = {
            TextButton({
                run.run {
                    val n = c.repo.addToCollection(collection.id, picked)
                    toast(ctx, "Added ${plural(n, "document")}")
                    onAdded()
                    onDismiss()
                }
            }, enabled = picked.isNotEmpty() && !run.busy) { Text(if (picked.isEmpty()) "Add" else "Add ${picked.size}") }
        },
        dismissButton = { TextButton(onDismiss) { Text("Cancel") } },
    )
}
