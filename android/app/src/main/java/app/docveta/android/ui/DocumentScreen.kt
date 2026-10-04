package app.docveta.android.ui

import android.content.Context
import android.content.Intent
import androidx.compose.foundation.background
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
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.OpenInNew
import androidx.compose.material.icons.automirrored.outlined.Send
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.outlined.AutoAwesome
import androidx.compose.material.icons.outlined.CalendarMonth
import androidx.compose.material.icons.outlined.Close
import androidx.compose.material.icons.outlined.Delete
import androidx.compose.material.icons.outlined.DoneAll
import androidx.compose.material.icons.outlined.Link
import androidx.compose.material.icons.outlined.LockOpen
import androidx.compose.material.icons.outlined.MoreVert
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material.icons.outlined.RestoreFromTrash
import androidx.compose.material.icons.outlined.Share
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.Checkbox
import androidx.compose.material3.DatePicker
import androidx.compose.material3.DatePickerDialog
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilledTonalButton
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Tab
import androidx.compose.material3.TabRow
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.rememberDatePickerState
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.core.content.FileProvider
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import app.docveta.android.AppContainer
import app.docveta.android.data.AiSuggestion
import app.docveta.android.data.ApiException
import app.docveta.android.data.Document
import app.docveta.android.data.Note
import app.docveta.android.data.Share
import app.docveta.android.data.Taxonomy
import coil.compose.AsyncImage
import java.io.File
import java.time.Instant
import java.time.LocalDate
import java.time.ZoneOffset
import kotlinx.coroutines.launch

class DocViewModel(private val c: AppContainer, val id: String) : ViewModel() {
    var doc by mutableStateOf<Document?>(null)
        private set
    var error by mutableStateOf<String?>(null)
        private set
    var message by mutableStateOf<String?>(null)
    var file by mutableStateOf<File?>(null)
        private set
    var fileProgress by mutableStateOf(0f)
        private set
    var fileError by mutableStateOf<String?>(null)
        private set
    var notes by mutableStateOf<List<Note>>(emptyList())
        private set
    var suggestions by mutableStateOf<List<AiSuggestion>>(emptyList())
        private set
    var busy by mutableStateOf(false)
        private set
    var gone by mutableStateOf(false)
        private set

    init {
        load()
    }

    fun load() {
        viewModelScope.launch {
            try {
                val d = c.repo.document(id)
                doc = d
                error = null
                loadFile(d)
                notes = runCatching { c.repo.notes(id) }.getOrDefault(notes)
                if (d.suggestionCount > 0) suggestions = runCatching { c.repo.suggestions(id) }.getOrDefault(emptyList())
                // A document still being read: look again shortly so the text layer and suggestions appear.
                if (d.status == "processing") {
                    kotlinx.coroutines.delay(3000)
                    load()
                }
            } catch (e: Exception) {
                error = e.friendly()
            }
        }
    }

    private suspend fun loadFile(d: Document) {
        if (!d.isPdf && !d.isImage && !d.hasDerived && !d.hasArchive) return
        if (file != null && doc?.version == d.version) return
        try {
            fileError = null
            file = c.repo.viewableFile(d) { fileProgress = it }
        } catch (e: Exception) {
            fileError = e.friendly()
        }
    }

    private fun act(block: suspend () -> Unit) {
        viewModelScope.launch {
            busy = true
            try {
                block()
            } catch (e: ApiException) {
                message = if (e.status == 412) "Someone else just changed this document. Reloaded." else e.message
                if (e.status == 412) load()
            } catch (e: Exception) {
                message = e.friendly()
            } finally {
                busy = false
            }
        }
    }

    private fun updated(d: Document) {
        val versionChanged = doc?.version != d.version
        doc = d
        if (versionChanged && (d.isPdf || d.isImage)) viewModelScope.launch { file = null; loadFile(d) }
    }

    fun rename(title: String) {
        val d = doc ?: return
        if (title.isBlank() || title.trim() == d.title) return
        act { updated(c.repo.setTitle(id, title.trim(), d.version)) }
    }

    fun setDate(iso: String?) = doc?.let { d -> act { updated(c.repo.setDate(id, iso, d.version)) } }
    fun setCorrespondent(t: Taxonomy?) = doc?.let { d -> act { updated(c.repo.setCorrespondent(id, t?.id, d.version)) } }
    fun setType(t: Taxonomy?) = doc?.let { d -> act { updated(c.repo.setType(id, t?.id, d.version)) } }
    fun setTags(ids: List<String>) = doc?.let { d -> act { updated(c.repo.setTags(id, ids, d.version)) } }

    fun review(onDone: () -> Unit) = act {
        updated(c.repo.markReviewed(id))
        message = "Marked as reviewed"
        onDone()
    }

    fun moveToTrash(onDone: () -> Unit) = act {
        c.repo.trash(id)
        gone = true
        onDone()
    }

    fun restore() = act { c.repo.restore(id); load() }
    fun deleteForever(onDone: () -> Unit) = act { c.repo.deleteForever(id); gone = true; onDone() }
    fun reprocess() = act { c.repo.reprocess(id); message = "Processing again"; load() }
    fun unlock(password: String, onDone: (Boolean) -> Unit) = act {
        try {
            updated(c.repo.unlock(id, password))
            message = "Unlocked. Reading the text now."
            onDone(true)
            load()
        } catch (e: ApiException) {
            message = e.message
            onDone(false)
        }
    }

    fun addNote(body: String) = act { notes = listOf(c.repo.addNote(id, body)) + notes; doc = doc?.copy(noteCount = (doc?.noteCount ?: 0) + 1) }
    fun deleteNote(n: Note) = act { c.repo.deleteNote(id, n.id); notes = notes.filterNot { it.id == n.id } }

    fun resolve(accept: Boolean, ids: List<String>? = null) = act {
        c.repo.resolveSuggestions(id, accept, ids)
        suggestions = if (ids == null) emptyList() else suggestions.filterNot { it.id in ids }
        doc = c.repo.document(id)
    }

    suspend fun originalFile(): File? = try {
        doc?.let { c.repo.originalFile(it) }
    } catch (e: Exception) {
        message = e.friendly()
        null
    }

    suspend fun shares(): List<Share> = runCatching { c.repo.shares(id) }.getOrDefault(emptyList())
    suspend fun createShare(days: Int?, password: String?, download: Boolean) = c.repo.createShare(id, days, password, download)
    suspend fun revokeShare(s: Share) = runCatching { c.repo.revokeShare(s.id) }
    suspend fun taxonomy(kind: String, space: String) = runCatching { c.repo.taxonomy(kind, space) }.getOrDefault(emptyList())
    suspend fun createTaxonomy(kind: String, space: String, name: String) = c.repo.createTaxonomy(kind, space, name)
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun DocumentScreen(id: String, startPage: Int = 0, onBack: () -> Unit) {
    val me = LocalMe.current
    val vm = container("doc-$id") { DocViewModel(it, id) }
    val doc = vm.doc
    val snack = remember { SnackbarHostState() }
    val scope = rememberCoroutineScope()
    val ctx = LocalContext.current
    var tab by remember { mutableIntStateOf(0) }
    var menu by remember { mutableStateOf(false) }
    var unlock by remember { mutableStateOf(false) }
    var shareLink by remember { mutableStateOf(false) }
    var confirmDelete by remember { mutableStateOf(false) }
    val canEdit = doc != null && me.spaces.firstOrNull { it.id == doc.space.id }?.canWrite == true && doc.deletedAt == null

    LaunchedEffect(vm.message) {
        vm.message?.let {
            snack.showSnackbar(it)
            vm.message = null
        }
    }

    Scaffold(
        snackbarHost = { SnackbarHost(snack) },
        topBar = {
            TopAppBar(
                title = { Column { Text(doc?.title ?: "Document", maxLines = 1, overflow = TextOverflow.Ellipsis, style = MaterialTheme.typography.titleMedium); if (doc != null) Text(listOfNotNull(doc.correspondent?.name, me.spaces.firstOrNull { it.id == doc.space.id }?.label).joinToString(" · "), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1) } },
                navigationIcon = { BackButton(onBack) },
                actions = {
                    if (doc != null) {
                        if (doc.inbox && canEdit) IconButton({ vm.review { onBack() } }) { Icon(Icons.Outlined.DoneAll, "Mark as reviewed") }
                        if (doc.deletedAt != null) {
                            IconButton({ vm.restore() }) { Icon(Icons.Outlined.RestoreFromTrash, "Restore") }
                        } else {
                            IconButton({ scope.launch { vm.originalFile()?.let { shareFile(ctx, it, doc.mimeType) } } }) { Icon(Icons.Outlined.Share, "Share the file") }
                        }
                        Box {
                            IconButton({ menu = true }) { Icon(Icons.Outlined.MoreVert, "More") }
                            DropdownMenu(menu, { menu = false }) {
                                if (doc.deletedAt == null) {
                                    DropdownMenuItem(text = { Text("Open in another app") }, leadingIcon = { Icon(Icons.AutoMirrored.Outlined.OpenInNew, null) }, onClick = { menu = false; scope.launch { vm.originalFile()?.let { openFile(ctx, it, doc.mimeType) } } })
                                    DropdownMenuItem(text = { Text("Share with a link") }, leadingIcon = { Icon(Icons.Outlined.Link, null) }, onClick = { menu = false; shareLink = true })
                                    if (canEdit) {
                                        if (doc.status == "needs_password") DropdownMenuItem(text = { Text("Enter PDF password") }, leadingIcon = { Icon(Icons.Outlined.LockOpen, null) }, onClick = { menu = false; unlock = true })
                                        DropdownMenuItem(text = { Text("Process again") }, leadingIcon = { Icon(Icons.Outlined.Refresh, null) }, onClick = { menu = false; vm.reprocess() })
                                        DropdownMenuItem(text = { Text("Move to Trash") }, leadingIcon = { Icon(Icons.Outlined.Delete, null) }, onClick = { menu = false; vm.moveToTrash { scope.launch { snack.showSnackbar("Moved to Trash") }; onBack() } })
                                    }
                                } else if (canEdit) {
                                    DropdownMenuItem(text = { Text("Delete forever", color = MaterialTheme.colorScheme.error) }, leadingIcon = { Icon(Icons.Outlined.Delete, null, tint = MaterialTheme.colorScheme.error) }, onClick = { menu = false; confirmDelete = true })
                                }
                            }
                        }
                    }
                },
            )
        },
    ) { pad ->
        Column(Modifier.padding(pad).fillMaxSize()) {
            when {
                doc == null && vm.error != null -> ErrorState(vm.error!!) { vm.load() }
                doc == null -> LoadingBox()
                else -> {
                    TabRow(tab) {
                        listOf("Preview", "Details", "Notes" + if (doc.noteCount > 0) " (${doc.noteCount})" else "").forEachIndexed { i, t -> Tab(tab == i, { tab = i }, text = { Text(t) }) }
                    }
                    when (tab) {
                        0 -> Preview(vm, doc, ctx, scope, startPage)
                        1 -> Details(vm, doc, canEdit)
                        else -> NotesTab(vm, canEdit)
                    }
                }
            }
        }
    }
    if (unlock && doc != null) PasswordDialog({ unlock = false }) { pw -> vm.unlock(pw) { ok -> if (ok) unlock = false } }
    if (shareLink && doc != null) ShareLinkSheet(vm, { shareLink = false })
    if (confirmDelete) AlertDialog(
        onDismissRequest = { confirmDelete = false }, title = { Text("Delete forever?") }, text = { Text("“${doc?.title}” will be permanently deleted. This can't be undone.") },
        confirmButton = { TextButton({ confirmDelete = false; vm.deleteForever(onBack) }) { Text("Delete forever", color = MaterialTheme.colorScheme.error) } },
        dismissButton = { TextButton({ confirmDelete = false }) { Text("Cancel") } },
    )
}

@Composable
private fun Preview(vm: DocViewModel, doc: Document, ctx: Context, scope: kotlinx.coroutines.CoroutineScope, startPage: Int) {
    val f = vm.file
    Box(Modifier.fillMaxSize()) {
        when {
            f != null && f.extension.equals("pdf", ignoreCase = true) -> PdfView(f, startPage = startPage)
            f != null -> ImagePreview(f)
            vm.fileError != null -> ErrorState(vm.fileError!!) { vm.load() }
            doc.isPdf || doc.isImage || doc.hasDerived || doc.hasArchive -> Column(Modifier.align(Alignment.Center).padding(32.dp), horizontalAlignment = Alignment.CenterHorizontally) {
                LinearProgressIndicator(progress = { vm.fileProgress.coerceIn(0f, 1f) }.takeIf { vm.fileProgress > 0f } ?: { 0f }, Modifier.fillMaxWidth(0.6f))
                Spacer(Modifier.height(12.dp))
                Text("Opening…", style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
            else -> EmptyState(Icons.AutoMirrored.Outlined.OpenInNew, "No preview here", "This type of file opens in another app.") {
                FilledTonalButton({ scope.launch { vm.originalFile()?.let { openFile(ctx, it, doc.mimeType) } } }) { Text("Open file") }
            }
        }
        if (doc.status == "processing") Row(Modifier.align(Alignment.TopCenter).padding(10.dp).clip(RoundedCornerShape(50)).background(MaterialTheme.colorScheme.inverseSurface.copy(alpha = 0.9f)).padding(horizontal = 14.dp, vertical = 6.dp), verticalAlignment = Alignment.CenterVertically) {
            androidx.compose.material3.CircularProgressIndicator(Modifier.size(14.dp), strokeWidth = 2.dp, color = MaterialTheme.colorScheme.inverseOnSurface)
            Spacer(Modifier.width(8.dp))
            Text(stageLabel(doc.processingStage) + "…", color = MaterialTheme.colorScheme.inverseOnSurface, style = MaterialTheme.typography.labelMedium)
        }
    }
}

@Composable
private fun ImagePreview(f: File) {
    Box(Modifier.fillMaxSize().background(MaterialTheme.colorScheme.surfaceVariant)) {
        AsyncImage(f, null, Modifier.fillMaxSize().pinchZoom(), contentScale = ContentScale.Fit)
    }
}

@OptIn(ExperimentalLayoutApi::class, ExperimentalMaterial3Api::class)
@Composable
private fun Details(vm: DocViewModel, doc: Document, canEdit: Boolean) {
    val me = LocalMe.current
    var title by remember(doc.id, doc.title) { mutableStateOf(doc.title) }
    var picker by remember { mutableStateOf<String?>(null) }
    var datePicker by remember { mutableStateOf(false) }
    Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp), verticalArrangement = Arrangement.spacedBy(16.dp)) {
        if (doc.status == "failed" || doc.status == "needs_password") {
            Row(Modifier.fillMaxWidth().background(MaterialTheme.colorScheme.errorContainer, RoundedCornerShape(12.dp)).padding(12.dp)) {
                Column {
                    Text(if (doc.status == "needs_password") "Password protected" else "Couldn't read this document", style = MaterialTheme.typography.titleSmall)
                    if (doc.processingError.isNotBlank()) Text(doc.processingError, style = MaterialTheme.typography.bodySmall)
                }
            }
        }
        if (vm.suggestions.isNotEmpty() && canEdit) SuggestionsCard(vm)
        OutlinedTextField(
            title, { title = it }, label = { Text("Title") }, modifier = Modifier.fillMaxWidth(), enabled = canEdit, singleLine = false, maxLines = 3,
            trailingIcon = { if (title.trim() != doc.title && title.isNotBlank()) IconButton({ vm.rename(title) }) { Icon(Icons.Filled.Check, "Save title") } },
            keyboardOptions = androidx.compose.foundation.text.KeyboardOptions(imeAction = androidx.compose.ui.text.input.ImeAction.Done), keyboardActions = androidx.compose.foundation.text.KeyboardActions(onDone = { vm.rename(title) }),
        )
        FieldButton("Date on the document", formatDate(doc.documentDate, me.dateFormat).ifBlank { "Not set" }, Icons.Outlined.CalendarMonth, canEdit, onClear = if (doc.documentDate != null && canEdit) ({ vm.setDate(null) }) else null) { datePicker = true }
        FieldButton("Who is it from?", doc.correspondent?.name ?: "Not set", null, canEdit, onClear = if (doc.correspondent != null && canEdit) ({ vm.setCorrespondent(null) }) else null) { picker = "correspondents" }
        FieldButton("What is it?", doc.documentType?.name ?: "Not set", null, canEdit, onClear = if (doc.documentType != null && canEdit) ({ vm.setType(null) }) else null) { picker = "document-types" }
        Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
            Text("Tags", style = MaterialTheme.typography.labelLarge, color = MaterialTheme.colorScheme.onSurfaceVariant)
            FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                doc.tags.forEach { TagChip(it) }
                if (canEdit) Box(Modifier.clip(RoundedCornerShape(8.dp)).clickable { picker = "tags" }.background(MaterialTheme.colorScheme.surfaceVariant).padding(horizontal = 10.dp, vertical = 4.dp)) { Text(if (doc.tags.isEmpty()) "Add tags" else "Edit", style = MaterialTheme.typography.labelMedium) }
            }
        }
        if (doc.customFields.any { !it.value.toString().let { v -> v == "null" || v == "\"\"" } }) {
            HorizontalDivider()
            doc.customFields.forEach { f ->
                val v = f.value.toString().trim('"')
                if (v != "null" && v.isNotBlank()) Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween) {
                    Text(f.name, color = MaterialTheme.colorScheme.onSurfaceVariant, style = MaterialTheme.typography.bodyMedium)
                    Text(v + (f.currency?.let { " $it" } ?: ""), style = MaterialTheme.typography.bodyMedium)
                }
            }
        }
        HorizontalDivider()
        InfoRow("Added", formatDate(doc.addedAt))
        InfoRow("File", doc.originalFilename.ifBlank { "—" })
        InfoRow("Size", formatBytes(doc.sizeBytes) + (doc.pageCount?.let { " · $it page${if (it > 1) "s" else ""}" } ?: ""))
        InfoRow("Searchable", if (doc.status == "ready") "Yes" else "Not yet")
    }
    if (datePicker) {
        val initial = doc.documentDate?.let { runCatching { LocalDate.parse(it).atStartOfDay().toInstant(ZoneOffset.UTC).toEpochMilli() }.getOrNull() }
        val st = rememberDatePickerState(initialSelectedDateMillis = initial)
        DatePickerDialog(onDismissRequest = { datePicker = false }, confirmButton = {
            TextButton({ datePicker = false; st.selectedDateMillis?.let { vm.setDate(Instant.ofEpochMilli(it).atZone(ZoneOffset.UTC).toLocalDate().toString()) } }) { Text("OK") }
        }, dismissButton = { TextButton({ datePicker = false }) { Text("Cancel") } }) { DatePicker(st) }
    }
    picker?.let { kind ->
        TaxonomySheet(
            title = when (kind) { "tags" -> "Tags"; "correspondents" -> "Who is it from?"; else -> "What is it?" },
            kind = kind, spaceId = doc.space.id, vm = vm, multi = kind == "tags",
            selected = when (kind) { "tags" -> doc.tags.map { it.id }; "correspondents" -> listOfNotNull(doc.correspondent?.id); else -> listOfNotNull(doc.documentType?.id) },
            onDismiss = { picker = null },
            onDone = { ids, items ->
                picker = null
                when (kind) {
                    "tags" -> vm.setTags(ids)
                    "correspondents" -> vm.setCorrespondent(items.firstOrNull())
                    else -> vm.setType(items.firstOrNull())
                }
            },
        )
    }
}

@Composable
private fun FieldButton(label: String, value: String, icon: androidx.compose.ui.graphics.vector.ImageVector?, enabled: Boolean, onClear: (() -> Unit)?, onClick: () -> Unit) {
    Column(Modifier.fillMaxWidth().clip(RoundedCornerShape(12.dp)).clickable(enabled = enabled, onClick = onClick).background(MaterialTheme.colorScheme.surfaceContainer).padding(horizontal = 14.dp, vertical = 10.dp)) {
        Text(label, style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
        Row(verticalAlignment = Alignment.CenterVertically) {
            if (icon != null) {
                Icon(icon, null, Modifier.size(18.dp), tint = MaterialTheme.colorScheme.onSurfaceVariant)
                Spacer(Modifier.width(8.dp))
            }
            Text(value, Modifier.weight(1f), style = MaterialTheme.typography.bodyLarge, color = if (value == "Not set") MaterialTheme.colorScheme.onSurfaceVariant else MaterialTheme.colorScheme.onSurface)
            if (onClear != null) IconButton(onClear, Modifier.size(28.dp)) { Icon(Icons.Outlined.Close, "Clear", Modifier.size(18.dp)) }
        }
    }
}

@Composable
private fun InfoRow(k: String, v: String) {
    Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween) {
        Text(k, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
        Text(v, style = MaterialTheme.typography.bodyMedium, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(start = 24.dp))
    }
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun SuggestionsCard(vm: DocViewModel) {
    Column(Modifier.fillMaxWidth().clip(RoundedCornerShape(16.dp)).background(MaterialTheme.colorScheme.primaryContainer.copy(alpha = 0.6f)).padding(14.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Icon(Icons.Outlined.AutoAwesome, null, Modifier.size(18.dp), tint = MaterialTheme.colorScheme.onPrimaryContainer)
            Spacer(Modifier.width(8.dp))
            Text("Suggested by AI", Modifier.weight(1f), style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.onPrimaryContainer)
            TextButton({ vm.resolve(false) }) { Text("Dismiss") }
            Button({ vm.resolve(true) }, contentPadding = androidx.compose.foundation.layout.PaddingValues(horizontal = 14.dp)) { Text("Accept all") }
        }
        FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            vm.suggestions.forEach { s ->
                val label = when (s.field) { "tag" -> "Tag"; "correspondent" -> "From"; "document_type" -> "Type"; "document_date" -> "Date"; "title" -> "Title"; else -> s.value.field ?: "Field" }
                Row(Modifier.clip(RoundedCornerShape(10.dp)).background(MaterialTheme.colorScheme.surface).padding(start = 10.dp), verticalAlignment = Alignment.CenterVertically) {
                    Text("$label: ${s.value.name}", style = MaterialTheme.typography.labelLarge)
                    IconButton({ vm.resolve(true, listOf(s.id)) }, Modifier.size(36.dp)) { Icon(Icons.Filled.Check, "Accept", Modifier.size(18.dp), tint = MaterialTheme.colorScheme.primary) }
                    IconButton({ vm.resolve(false, listOf(s.id)) }, Modifier.size(36.dp)) { Icon(Icons.Outlined.Close, "Dismiss", Modifier.size(18.dp)) }
                }
            }
        }
    }
}

@Composable
private fun NotesTab(vm: DocViewModel, canEdit: Boolean) {
    var text by remember { mutableStateOf("") }
    Column(Modifier.fillMaxSize().imePadding()) {
        LazyColumn(Modifier.weight(1f), contentPadding = androidx.compose.foundation.layout.PaddingValues(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            if (vm.notes.isEmpty()) item { Text("No notes yet.", color = MaterialTheme.colorScheme.onSurfaceVariant) }
            items(vm.notes, key = { it.id }) { n ->
                Column(Modifier.fillMaxWidth().clip(RoundedCornerShape(12.dp)).background(MaterialTheme.colorScheme.surfaceContainer).padding(12.dp)) {
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        Text(n.author?.name ?: "Someone", style = MaterialTheme.typography.labelLarge, modifier = Modifier.weight(1f))
                        Text(timeAgo(n.createdAt), style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                        if (n.canDelete) IconButton({ vm.deleteNote(n) }, Modifier.size(28.dp)) { Icon(Icons.Outlined.Delete, "Delete note", Modifier.size(16.dp)) }
                    }
                    Text(n.body.replace(Regex("@\\[([^\\]]+)]\\([0-9a-fA-F-]{36}\\)"), "@$1"), style = MaterialTheme.typography.bodyMedium)
                }
            }
        }
        if (canEdit) Row(Modifier.fillMaxWidth().padding(12.dp), verticalAlignment = Alignment.Bottom) {
            OutlinedTextField(text, { text = it }, Modifier.weight(1f), placeholder = { Text("Add a note…") }, maxLines = 4, shape = RoundedCornerShape(20.dp))
            Spacer(Modifier.width(8.dp))
            IconButton({ vm.addNote(text.trim()); text = "" }, enabled = text.isNotBlank()) { Icon(Icons.AutoMirrored.Outlined.Send, "Add note") }
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun TaxonomySheet(title: String, kind: String, spaceId: String, vm: DocViewModel, multi: Boolean, selected: List<String>, onDismiss: () -> Unit, onDone: (List<String>, List<Taxonomy>) -> Unit) {
    var items by remember { mutableStateOf<List<Taxonomy>?>(null) }
    var picked by remember { mutableStateOf(selected) }
    var q by remember { mutableStateOf("") }
    var busy by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()
    LaunchedEffect(Unit) { items = vm.taxonomy(kind, spaceId) }
    val shown = items.orEmpty().filter { it.name.contains(q.trim(), ignoreCase = true) }
    val exact = items.orEmpty().any { it.name.equals(q.trim(), ignoreCase = true) }
    ModalBottomSheet(onDismissRequest = onDismiss, sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true)) {
        Column(Modifier.padding(horizontal = 16.dp).padding(bottom = 16.dp).imePadding()) {
            Text(title, style = MaterialTheme.typography.titleLarge)
            Spacer(Modifier.height(10.dp))
            OutlinedTextField(q, { q = it }, Modifier.fillMaxWidth(), singleLine = true, placeholder = { Text("Search or create") })
            Spacer(Modifier.height(6.dp))
            LazyColumn(Modifier.weight(1f, fill = false).height(360.dp)) {
                if (q.isNotBlank() && !exact) item {
                    Row(Modifier.fillMaxWidth().clickable(enabled = !busy) {
                        busy = true
                        scope.launch {
                            runCatching { vm.createTaxonomy(kind, spaceId, q.trim()) }.onSuccess { t ->
                                items = items.orEmpty() + t
                                picked = if (multi) picked + t.id else listOf(t.id)
                                q = ""
                                if (!multi) onDone(picked, listOf(t))
                            }
                            busy = false
                        }
                    }.padding(vertical = 14.dp), verticalAlignment = Alignment.CenterVertically) {
                        Text("Create “${q.trim()}”", color = MaterialTheme.colorScheme.primary, style = MaterialTheme.typography.titleSmall)
                    }
                }
                if (items == null) item { LoadingBox(Modifier.fillMaxWidth().height(80.dp)) }
                items(shown, key = { it.id }) { t ->
                    val on = picked.contains(t.id)
                    Row(Modifier.fillMaxWidth().clickable {
                        if (multi) picked = if (on) picked - t.id else picked + t.id
                        else onDone(listOf(t.id), listOf(t))
                    }.padding(vertical = 12.dp), verticalAlignment = Alignment.CenterVertically) {
                        if (multi) Checkbox(on, null) else if (on) Icon(Icons.Filled.Check, null, tint = MaterialTheme.colorScheme.primary)
                        if (multi || on) Spacer(Modifier.width(12.dp))
                        Text(t.name, Modifier.weight(1f), style = MaterialTheme.typography.bodyLarge)
                        Text(t.documentCount.toString(), style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                }
            }
            if (multi) Button({ onDone(picked, items.orEmpty().filter { it.id in picked }) }, Modifier.fillMaxWidth()) { Text("Done") }
        }
    }
}

@Composable
private fun PasswordDialog(onDismiss: () -> Unit, onSubmit: (String) -> Unit) {
    var pw by remember { mutableStateOf("") }
    AlertDialog(
        onDismissRequest = onDismiss, title = { Text("Unlock this PDF") },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
                Text("Docveta keeps an unlocked copy so it can read and search it. The password isn't saved.", style = MaterialTheme.typography.bodySmall)
                OutlinedTextField(pw, { pw = it }, singleLine = true, label = { Text("PDF password") }, visualTransformation = androidx.compose.ui.text.input.PasswordVisualTransformation())
            }
        },
        confirmButton = { TextButton({ onSubmit(pw) }, enabled = pw.isNotEmpty()) { Text("Unlock") } },
        dismissButton = { TextButton(onDismiss) { Text("Cancel") } },
    )
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun ShareLinkSheet(vm: DocViewModel, onDismiss: () -> Unit) {
    val ctx = LocalContext.current
    var days by remember { mutableStateOf<Int?>(7) }
    var password by remember { mutableStateOf("") }
    var download by remember { mutableStateOf(true) }
    var link by remember { mutableStateOf<String?>(null) }
    var existing by remember { mutableStateOf<List<Share>>(emptyList()) }
    var busy by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    val scope = rememberCoroutineScope()
    LaunchedEffect(Unit) { existing = vm.shares().filter { it.status == "active" } }
    ModalBottomSheet(onDismissRequest = onDismiss, sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true)) {
        Column(Modifier.padding(horizontal = 20.dp).padding(bottom = 24.dp).verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(14.dp)) {
            Text("Share with a link", style = MaterialTheme.typography.titleLarge)
            Text("Anyone with the link can view this document. They don't need an account.", style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
            FlowRowChips(listOf(1 to "1 day", 7 to "7 days", 30 to "30 days", null to "No expiry"), days) { days = it }
            OutlinedTextField(password, { password = it }, Modifier.fillMaxWidth(), singleLine = true, label = { Text("Password (optional)") })
            Row(verticalAlignment = Alignment.CenterVertically) { Checkbox(download, { download = it }); Text("They can download the file") }
            if (error != null) Text(error!!, color = MaterialTheme.colorScheme.error)
            if (link == null) {
                Button({
                    busy = true
                    scope.launch {
                        try {
                            link = vm.createShare(days, password.ifBlank { null }, download).link
                            existing = vm.shares().filter { it.status == "active" }
                        } catch (e: Exception) {
                            error = e.friendly()
                        }
                        busy = false
                    }
                }, Modifier.fillMaxWidth(), enabled = !busy) { Text("Create link") }
            } else {
                Text(link!!, style = MaterialTheme.typography.bodyMedium, modifier = Modifier.fillMaxWidth().clip(RoundedCornerShape(10.dp)).background(MaterialTheme.colorScheme.surfaceVariant).padding(12.dp))
                Button({
                    ctx.startActivity(Intent.createChooser(Intent(Intent.ACTION_SEND).apply { type = "text/plain"; putExtra(Intent.EXTRA_TEXT, link) }, "Share link"))
                }, Modifier.fillMaxWidth()) { Icon(Icons.Outlined.Share, null); Spacer(Modifier.width(8.dp)); Text("Send the link") }
            }
            if (existing.isNotEmpty()) {
                HorizontalDivider()
                Text("Active links", style = MaterialTheme.typography.titleSmall)
                existing.forEach { s ->
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        Column(Modifier.weight(1f)) {
                            Text(if (s.expiresAt != null) "until ${formatDate(s.expiresAt)}" else "no expiry", style = MaterialTheme.typography.bodyMedium)
                            Text("${s.accessCount} view${if (s.accessCount == 1) "" else "s"}${if (s.hasPassword) " · password" else ""}", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                        }
                        TextButton({ scope.launch { vm.revokeShare(s); existing = existing - s } }) { Text("Stop", color = MaterialTheme.colorScheme.error) }
                    }
                }
            }
        }
    }
}

@OptIn(ExperimentalLayoutApi::class, ExperimentalMaterial3Api::class)
@Composable
private fun <T> FlowRowChips(options: List<Pair<T, String>>, selected: T, onSelect: (T) -> Unit) {
    FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        options.forEach { (v, l) -> androidx.compose.material3.FilterChip(selected == v, { onSelect(v) }, label = { Text(l) }) }
    }
}

fun shareFile(ctx: Context, f: File, mime: String) {
    val uri = FileProvider.getUriForFile(ctx, ctx.packageName + ".files", f)
    ctx.startActivity(Intent.createChooser(Intent(Intent.ACTION_SEND).apply { type = mime.ifBlank { "*/*" }; putExtra(Intent.EXTRA_STREAM, uri); addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION) }, "Share"))
}

fun openFile(ctx: Context, f: File, mime: String) {
    val uri = FileProvider.getUriForFile(ctx, ctx.packageName + ".files", f)
    try {
        ctx.startActivity(Intent(Intent.ACTION_VIEW).apply { setDataAndType(uri, mime.ifBlank { "*/*" }); addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION or Intent.FLAG_ACTIVITY_NEW_TASK) })
    } catch (e: android.content.ActivityNotFoundException) {
        android.widget.Toast.makeText(ctx, "No app can open this file", android.widget.Toast.LENGTH_LONG).show()
    }
}
