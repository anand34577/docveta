package app.docveta.android.ui

import android.content.Context
import android.content.Intent
import android.net.Uri
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
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
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.OpenInNew
import androidx.compose.material.icons.automirrored.outlined.Send
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.outlined.Search
import androidx.compose.material.icons.outlined.AlternateEmail
import androidx.compose.material.icons.outlined.AutoAwesome
import androidx.compose.material.icons.outlined.CalendarMonth
import androidx.compose.material.icons.outlined.Close
import androidx.compose.material.icons.outlined.ContentCopy
import androidx.compose.material.icons.outlined.Delete
import androidx.compose.material.icons.outlined.DoneAll
import androidx.compose.material.icons.outlined.Download
import androidx.compose.material.icons.outlined.GridView
import androidx.compose.material.icons.outlined.History
import androidx.compose.material.icons.outlined.KeyboardArrowDown
import androidx.compose.material.icons.outlined.KeyboardArrowUp
import androidx.compose.material.icons.outlined.Link
import androidx.compose.material.icons.outlined.LockOpen
import androidx.compose.material.icons.outlined.MoreVert
import androidx.compose.material.icons.outlined.QuestionAnswer
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material.icons.outlined.RestoreFromTrash
import androidx.compose.material.icons.outlined.Share
import androidx.compose.material.icons.automirrored.outlined.TextSnippet
import androidx.compose.material.icons.outlined.Upload
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.Checkbox
import androidx.compose.material3.DatePicker
import androidx.compose.material3.DatePickerDialog
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilledTonalButton
import androidx.compose.material3.FilterChip
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.ScrollableTabRow
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Switch
import androidx.compose.material3.Tab
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
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.core.content.FileProvider
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import app.docveta.android.AppContainer
import app.docveta.android.data.AiSuggestion
import app.docveta.android.data.ApiException
import app.docveta.android.data.CustomField
import app.docveta.android.data.Document
import app.docveta.android.data.Note
import app.docveta.android.data.Share
import app.docveta.android.data.Taxonomy
import app.docveta.android.data.assignAsn
import app.docveta.android.data.customFields
import app.docveta.android.data.directory
import app.docveta.android.data.downloadFile
import app.docveta.android.data.history
import app.docveta.android.data.pages
import app.docveta.android.data.reprocess
import app.docveta.android.data.restoreVersion
import app.docveta.android.data.runAi
import app.docveta.android.data.setCustomField
import app.docveta.android.data.setLanguage
import app.docveta.android.data.setLocation
import app.docveta.android.data.setSpace
import app.docveta.android.data.similar
import app.docveta.android.data.uploadVersion
import coil.compose.AsyncImage
import java.io.File
import java.time.Instant
import java.time.LocalDate
import java.time.ZoneOffset
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.booleanOrNull
import kotlinx.serialization.json.contentOrNull

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
    var fields by mutableStateOf<List<CustomField>>(emptyList())
        private set
    var busy by mutableStateOf(false)
        private set
    var gone by mutableStateOf(false)
        private set

    private var poll: Job? = null

    init {
        load()
    }

    /**
     * Coming back to the screen: look again, unless a load is still running. The screen also
     * "comes back" when its opening animation ends, and starting over there threw away the
     * file that was halfway down.
     */
    fun refresh() {
        if (doc != null && poll?.isActive != true) load()
    }

    /** Loads the document; while it's still being read, looks again every few seconds (one loop at a time). */
    fun load() {
        poll?.cancel()
        poll = viewModelScope.launch {
            while (true) {
                try {
                    val d = c.repo.document(id)
                    val first = doc == null
                    doc = d
                    error = null
                    if (d.deletedAt == null) c.session.rememberDoc(d.id, d.title)
                    loadFile(d)
                    notes = runCatching { c.repo.notes(id) }.getOrDefault(notes)
                    suggestions = if (d.suggestionCount > 0) runCatching { c.repo.suggestions(id) }.getOrDefault(emptyList()) else emptyList()
                    if (first || fields.isEmpty()) fields = runCatching { c.repo.customFields(d.space.id).filter { it.spaceId == d.space.id } }.getOrDefault(emptyList())
                    if (d.status != "processing") break
                } catch (e: Exception) {
                    if (e is kotlinx.coroutines.CancellationException) throw e
                    if ((e as? ApiException)?.status == 404) c.session.forgetDoc(id)
                    error = e.friendly()
                    break
                }
                delay(3000)
            }
        }
    }

    private suspend fun loadFile(d: Document) {
        if (!d.isPdf && !d.isImage && !d.hasDerived && !d.hasArchive) return
        if (file?.name == c.repo.viewableName(d)) return
        try {
            fileError = null
            file = c.repo.viewableFile(d) { fileProgress = it }
        } catch (e: Exception) {
            if (e is kotlinx.coroutines.CancellationException) throw e
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
                if (e is kotlinx.coroutines.CancellationException) throw e
                message = e.friendly()
            } finally {
                busy = false
            }
        }
    }

    private fun updated(d: Document) {
        val spaceChanged = doc?.space?.id != d.space.id
        doc = d
        if (spaceChanged) viewModelScope.launch { fields = runCatching { c.repo.customFields(d.space.id).filter { it.spaceId == d.space.id } }.getOrDefault(emptyList()) }
        viewModelScope.launch { loadFile(d) }
    }

    // What is on its way to the server: the field saves on Done, on its tick and when it's left,
    // and two of those for the same text must not become two requests.
    private var savingTitle: String? = null
    private var savingLocation: String? = null

    fun rename(title: String) {
        val d = doc ?: return
        val t = title.trim()
        if (t.isEmpty() || t == d.title || t == savingTitle) return
        savingTitle = t
        act { try { updated(c.repo.setTitle(id, t, d.version)) } finally { savingTitle = null } }
    }

    fun setDate(iso: String?) = doc?.let { d -> act { updated(c.repo.setDate(id, iso, d.version)) } }
    fun setCorrespondent(t: Taxonomy?) = doc?.let { d -> act { updated(c.repo.setCorrespondent(id, t?.id, d.version)) } }
    fun setType(t: Taxonomy?) = doc?.let { d -> act { updated(c.repo.setType(id, t?.id, d.version)) } }
    fun setTags(ids: List<String>) = doc?.let { d -> act { updated(c.repo.setTags(id, ids, d.version)) } }
    fun setField(fieldId: String, value: JsonElement) = doc?.let { d -> act { updated(c.repo.setCustomField(id, fieldId, value, d.version)) } }
    fun moveTo(spaceId: String) = doc?.let { d -> act { updated(c.repo.setSpace(id, spaceId, d.version)); message = "Moved" } }
    fun setLanguage(lang: String) = doc?.let { d -> act { updated(c.repo.setLanguage(id, lang, d.version)) } }
    fun setLocation(where: String) {
        val d = doc ?: return
        if (where == d.physicalLocation || where == savingLocation) return
        savingLocation = where
        act { try { updated(c.repo.setLocation(id, where, d.version)) } finally { savingLocation = null } }
    }
    fun assignAsn() = act { val d = c.repo.assignAsn(id); updated(d); message = "Archive number ${d.asn} assigned. Write it on the paper original." }

    fun review(onDone: () -> Unit) = act {
        updated(c.repo.markReviewed(id))
        message = "Marked as reviewed"
        onDone()
    }

    fun moveToTrash(onDone: () -> Unit) = act {
        c.repo.trash(id)
        c.session.forgetDoc(id)
        gone = true
        onDone()
    }

    fun restore() = act { c.repo.restore(id); load() }
    fun deleteForever(onDone: () -> Unit) = act { c.repo.deleteForever(id); c.session.forgetDoc(id); gone = true; onDone() }
    fun reprocess(force: Boolean) = act { c.repo.reprocess(id, force); message = if (force) "Reading the text again" else "Processing again"; load() }
    fun askAi() = act { c.repo.runAi(id); message = "Asked AI for suggestions. They appear here in a moment."; delay(4000); load() }
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
    fun deleteNote(n: Note) = act { c.repo.deleteNote(id, n.id); notes = notes.filterNot { it.id == n.id }; doc = doc?.copy(noteCount = ((doc?.noteCount ?: 1) - 1).coerceAtLeast(0)) }

    fun resolve(accept: Boolean, ids: List<String>? = null) = act {
        c.repo.resolveSuggestions(id, accept, ids)
        suggestions = if (ids == null) emptyList() else suggestions.filterNot { it.id in ids }
        updated(c.repo.document(id))
    }

    fun uploadVersion(file: File, mime: String, name: String, onDone: () -> Unit) = act {
        updated(c.repo.uploadVersion(id, file, mime, name))
        file.delete()
        message = "New version added. Text recognition runs again."
        onDone()
        load()
    }

    fun restoreVersion(no: Int, onDone: () -> Unit) = act { updated(c.repo.restoreVersion(id, no)); message = "Version $no is the current file again"; onDone(); load() }

    suspend fun originalFile(): File? = try {
        doc?.let { c.repo.originalFile(it) }
    } catch (e: Exception) {
        if (e is kotlinx.coroutines.CancellationException) throw e
        message = e.friendly()
        null
    }

    suspend fun downloaded(kind: String, version: Int? = null): File? = try {
        doc?.let { c.repo.downloadFile(it, kind, version) }
    } catch (e: Exception) {
        if (e is kotlinx.coroutines.CancellationException) throw e
        message = e.friendly()
        null
    }

    suspend fun shares(): List<Share> = runCatching { c.repo.shares(id) }.getOrDefault(emptyList())
    suspend fun createShare(days: Int?, password: String?, download: Boolean) = c.repo.createShare(id, days, password, download)
    suspend fun revokeShare(s: Share) = runCatching { c.repo.revokeShare(s.id) }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun DocumentScreen(id: String, startPage: Int = 0, onBack: () -> Unit, onOpenDoc: (String) -> Unit = {}, onNavigate: (String) -> Unit = {}) {
    val me = LocalMe.current
    val ai by produceAi()
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
    val find = remember { PdfFindState() }
    val canEdit = doc != null && me.spaces.firstOrNull { it.id == doc.space.id }?.canWrite == true && doc.deletedAt == null

    LaunchedEffect(vm.message) {
        vm.message?.let {
            snack.showSnackbar(it)
            vm.message = null
        }
    }
    // Opening a page from Ask or a search result: start on the preview.
    androidx.lifecycle.compose.LifecycleEventEffect(androidx.lifecycle.Lifecycle.Event.ON_RESUME) { vm.refresh() }

    Scaffold(
        snackbarHost = { SnackbarHost(snack) },
        topBar = {
            TopAppBar(
                title = { Column { Text(doc?.title ?: "Document", maxLines = 1, overflow = TextOverflow.Ellipsis, style = MaterialTheme.typography.titleMedium); if (doc != null) Text(listOfNotNull(doc.correspondent?.name, me.spaces.firstOrNull { it.id == doc.space.id }?.label).joinToString(" · "), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1) } },
                navigationIcon = { BackButton(onBack) },
                actions = {
                    if (doc != null) {
                        if (doc.inbox && canEdit) IconButton({ vm.review { onBack() } }) { Icon(Icons.Outlined.DoneAll, "Mark as reviewed") }
                        if (doc.isPdf || doc.hasArchive || (doc.hasDerived && !doc.isImage)) IconButton({ tab = 0; find.open = true }) { Icon(Icons.Outlined.Search, "Find in document") }
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
                                    if (doc.hasArchive) DropdownMenuItem(text = { Text("Share searchable PDF") }, leadingIcon = { Icon(Icons.Outlined.Download, null) }, onClick = { menu = false; scope.launch { vm.downloaded("archive")?.let { shareFile(ctx, it, "application/pdf") } } })
                                    DropdownMenuItem(text = { Text("Share with a link") }, leadingIcon = { Icon(Icons.Outlined.Link, null) }, onClick = { menu = false; shareLink = true })
                                    if (ai.chat && doc.status == "ready") DropdownMenuItem(text = { Text("Ask about this document") }, leadingIcon = { Icon(Icons.Outlined.QuestionAnswer, null) }, onClick = { menu = false; onNavigate("ask/doc/${doc.id}") })
                                    if (canEdit) {
                                        if (doc.pagesEditable) DropdownMenuItem(text = { Text("Arrange pages") }, leadingIcon = { Icon(Icons.Outlined.GridView, null) }, onClick = { menu = false; onNavigate("doc/${doc.id}/pages") })
                                        if (doc.status == "needs_password") DropdownMenuItem(text = { Text("Enter PDF password") }, leadingIcon = { Icon(Icons.Outlined.LockOpen, null) }, onClick = { menu = false; unlock = true })
                                        if (ai.chat) DropdownMenuItem(text = { Text("Suggest tags with AI") }, leadingIcon = { Icon(Icons.Outlined.AutoAwesome, null) }, onClick = { menu = false; vm.askAi() })
                                        HorizontalDivider()
                                        DropdownMenuItem(text = { Text("Process again") }, leadingIcon = { Icon(Icons.Outlined.Refresh, null) }, onClick = { menu = false; vm.reprocess(false) })
                                        DropdownMenuItem(text = { Text("Re-read text (force OCR)") }, leadingIcon = { Icon(Icons.AutoMirrored.Outlined.TextSnippet, null) }, onClick = { menu = false; vm.reprocess(true) })
                                        HorizontalDivider()
                                        DropdownMenuItem(text = { Text("Move to Trash") }, leadingIcon = { Icon(Icons.Outlined.Delete, null) }, onClick = { menu = false; vm.moveToTrash { onBack() } })
                                    }
                                } else if (me.spaces.firstOrNull { it.id == doc.space.id }?.canWrite == true) {
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
                    val tabs = buildList {
                        add("preview" to "Preview"); add("details" to "Details"); add("notes" to ("Notes" + if (doc.noteCount > 0) " (${doc.noteCount})" else ""))
                        add("text" to "Text"); add("versions" to "Versions"); if (ai.embeddings) add("similar" to "Similar"); add("history" to "History")
                    }
                    val current = tabs.getOrNull(tab)?.first ?: "preview"
                    ScrollableTabRow(tabs.indexOfFirst { it.first == current }.coerceAtLeast(0), edgePadding = 8.dp) {
                        tabs.forEachIndexed { i, (_, t) ->
                            Tab(current == tabs[i].first, { tab = i }, text = { Text(t) }, selectedContentColor = MaterialTheme.colorScheme.primary, unselectedContentColor = MaterialTheme.colorScheme.onSurfaceVariant)
                        }
                    }
                    when (current) {
                        "preview" -> Preview(vm, doc, ctx, scope, startPage, find)
                        "details" -> Details(vm, doc, canEdit)
                        "notes" -> NotesTab(vm, canEdit)
                        "text" -> TextTab(doc)
                        "versions" -> VersionsTab(vm, doc, canEdit)
                        "similar" -> SimilarTab(doc, onOpenDoc)
                        else -> HistoryTab(doc)
                    }
                }
            }
        }
    }
    if (unlock && doc != null) PasswordDialog({ unlock = false }) { pw -> vm.unlock(pw) { ok -> if (ok) unlock = false } }
    if (shareLink && doc != null) ShareLinkSheet(vm, { shareLink = false })
    if (confirmDelete) ConfirmDialog("Delete forever?", "“${doc?.title}” will be permanently deleted. This can't be undone.", "Delete forever", destructive = true, onDismiss = { confirmDelete = false }) { vm.deleteForever(onBack) }
}

@Composable
private fun Preview(vm: DocViewModel, doc: Document, ctx: Context, scope: kotlinx.coroutines.CoroutineScope, startPage: Int, find: PdfFindState) {
    val c = LocalContainer.current
    val f = vm.file
    val isPdf = f != null && f.extension.equals("pdf", ignoreCase = true)
    Box(Modifier.fillMaxSize()) {
        when {
            isPdf -> PdfView(f!!, startPage = startPage, find = find, pageTexts = { c.repo.pages(doc.id).map { it.pageNo - 1 to it.text } })
            f != null -> ImagePreview(f)
            vm.fileError != null -> ErrorState(vm.fileError!!) { vm.load() }
            doc.isPdf || doc.isImage || doc.hasDerived || doc.hasArchive -> Column(Modifier.align(Alignment.Center).padding(32.dp), horizontalAlignment = Alignment.CenterHorizontally) {
                if (vm.fileProgress > 0f) LinearProgressIndicator(progress = { vm.fileProgress.coerceIn(0f, 1f) }, Modifier.fillMaxWidth(0.6f)) else LinearProgressIndicator(Modifier.fillMaxWidth(0.6f))
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
            Text(processingLabel(doc) + "…", color = MaterialTheme.colorScheme.inverseOnSurface, style = MaterialTheme.typography.labelMedium)
        }
        if (find.open && isPdf) FindBar(find, Modifier.align(Alignment.TopCenter))
        androidx.activity.compose.BackHandler(find.open) { find.close() }
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
    var location by remember(doc.id, doc.physicalLocation) { mutableStateOf(doc.physicalLocation) }
    var picker by remember { mutableStateOf<String?>(null) }
    var datePicker by remember { mutableStateOf(false) }
    var more by remember { mutableStateOf(false) }
    Column(Modifier.fillMaxSize().imePadding().verticalScroll(rememberScrollState()).padding(16.dp), verticalArrangement = Arrangement.spacedBy(16.dp)) {
        if (doc.status == "processing") Row(Modifier.fillMaxWidth().background(MaterialTheme.colorScheme.primaryContainer, RoundedCornerShape(12.dp)).padding(12.dp)) {
            Column {
                Text(processingLabel(doc) + "…", style = MaterialTheme.typography.titleSmall)
                val p = doc.progress
                if (p != null && p.pagesTotal > 1 && doc.processingStage == "ocr") {
                    LinearProgressIndicator(progress = { p.pagesDone.toFloat() / p.pagesTotal }, Modifier.fillMaxWidth().padding(vertical = 6.dp))
                }
                Text("You can already view, share and edit this document.", style = MaterialTheme.typography.bodySmall)
            }
        }
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
            // Leaving the field saves it, as on the web: a new title typed and then abandoned was lost.
            title, { title = it }, label = { Text("Title") }, modifier = Modifier.fillMaxWidth().onFocusChanged { if (!it.isFocused && canEdit) vm.rename(title) }, enabled = canEdit, singleLine = false, maxLines = 3,
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
        if (vm.fields.isNotEmpty()) {
            HorizontalDivider()
            vm.fields.forEach { f -> CustomFieldEditor(f, doc.customFields.firstOrNull { it.fieldId == f.id }?.value ?: JsonNull, canEdit, me.dateFormat) { v -> vm.setField(f.id, v) } }
        } else if (doc.customFields.any { it.value !is JsonNull }) {
            HorizontalDivider()
            doc.customFields.forEach { f ->
                val v = f.value.display()
                if (v.isNotBlank()) InfoRow(f.name, v + (f.currency?.let { " $it" } ?: ""))
            }
        }
        Row(Modifier.fillMaxWidth().clip(RoundedCornerShape(12.dp)).clickable { more = !more }.padding(vertical = 6.dp), verticalAlignment = Alignment.CenterVertically) {
            Text("More details", Modifier.weight(1f), style = MaterialTheme.typography.titleSmall)
            Icon(if (more) Icons.Outlined.KeyboardArrowUp else Icons.Outlined.KeyboardArrowDown, null)
        }
        if (more) {
            val spaces = me.spaces.filter { it.canWrite || it.id == doc.space.id }
            SelectField("Space", spaces.map { it.id to it.label }, doc.space.id, Modifier.fillMaxWidth(), enabled = canEdit, supporting = "Moving keeps tags and sender by matching names in the new space.") { if (it != doc.space.id) vm.moveTo(it) }
            SelectField("Language", languageOptions(doc.language), doc.language, Modifier.fillMaxWidth(), enabled = canEdit, supporting = "Used for text recognition and search.") { vm.setLanguage(it) }
            OutlinedTextField(
                location, { location = it }, label = { Text("Where is the paper original?") }, placeholder = { Text("e.g. Blue folder, cupboard 2") }, modifier = Modifier.fillMaxWidth().onFocusChanged { if (!it.isFocused && canEdit) vm.setLocation(location) }, enabled = canEdit, singleLine = true,
                trailingIcon = { if (location != doc.physicalLocation) IconButton({ vm.setLocation(location) }) { Icon(Icons.Filled.Check, "Save") } },
                keyboardOptions = androidx.compose.foundation.text.KeyboardOptions(imeAction = androidx.compose.ui.text.input.ImeAction.Done), keyboardActions = androidx.compose.foundation.text.KeyboardActions(onDone = { vm.setLocation(location) }),
            )
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text("Archive number (ASN)", Modifier.weight(1f), style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
                if (doc.asn != null) Text("#${doc.asn}", style = MaterialTheme.typography.titleSmall)
                else OutlinedButton({ vm.assignAsn() }, enabled = canEdit) { Text("Assign next number") }
            }
        }
        HorizontalDivider()
        InfoRow("Added", formatDate(doc.addedAt, me.dateFormat) + (doc.owner?.let { " by ${it.name}" } ?: ""))
        InfoRow("File", doc.originalFilename.ifBlank { "—" })
        InfoRow("Size", formatBytes(doc.sizeBytes) + (doc.pageCount?.let { " · $it page${if (it > 1) "s" else ""}" } ?: ""))
        InfoRow("Searchable", when { doc.hasArchive -> "Yes, with text layer"; doc.status == "ready" -> "Yes"; else -> "Not yet" })
    }
    if (datePicker) {
        val initial = doc.documentDate?.let { runCatching { LocalDate.parse(it.take(10)).atStartOfDay().toInstant(ZoneOffset.UTC).toEpochMilli() }.getOrNull() }
        val st = rememberDatePickerState(initialSelectedDateMillis = initial)
        DatePickerDialog(onDismissRequest = { datePicker = false }, confirmButton = {
            TextButton({ datePicker = false; st.selectedDateMillis?.let { vm.setDate(Instant.ofEpochMilli(it).atZone(ZoneOffset.UTC).toLocalDate().toString()) } }) { Text("OK") }
        }, dismissButton = { TextButton({ datePicker = false }) { Text("Cancel") } }) { DatePicker(st) }
    }
    picker?.let { kind ->
        TaxonomyPicker(
            kind, doc.space.id, multi = kind == "tags",
            selected = when (kind) { "tags" -> doc.tags.map { it.id }; "correspondents" -> listOfNotNull(doc.correspondent?.id); else -> listOfNotNull(doc.documentType?.id) },
            onDismiss = { picker = null },
        ) { items ->
            picker = null
            when (kind) {
                "tags" -> vm.setTags(items.map { it.id })
                "correspondents" -> vm.setCorrespondent(items.firstOrNull())
                else -> vm.setType(items.firstOrNull())
            }
        }
    }
}

private fun JsonElement.display(): String = when (this) {
    is JsonPrimitive -> if (isString) content else when (booleanOrNull) { true -> "Yes"; false -> "No"; null -> content }
    is JsonArray -> mapNotNull { (it as? JsonPrimitive)?.contentOrNull }.joinToString(", ")
    else -> ""
}

/** A typed editor for one custom field of the document's space (Amount, Due date, …). Saves on change. */
@OptIn(ExperimentalLayoutApi::class, ExperimentalMaterial3Api::class)
@Composable
private fun CustomFieldEditor(f: CustomField, value: JsonElement, canEdit: Boolean, dateFormat: String, onSave: (JsonElement) -> Unit) {
    val label = if (f.dataType == "monetary" && f.options.currency != null) "${f.name} (${f.options.currency})" else f.name
    val text0 = (value as? JsonPrimitive)?.takeIf { it !is JsonNull }?.content.orEmpty()
    when (f.dataType) {
        "boolean" -> Row(verticalAlignment = Alignment.CenterVertically) {
            Text(f.name, Modifier.weight(1f), style = MaterialTheme.typography.bodyLarge)
            Switch((value as? JsonPrimitive)?.booleanOrNull == true, { onSave(JsonPrimitive(it)) }, enabled = canEdit)
        }
        "date" -> {
            var picking by remember { mutableStateOf(false) }
            FieldButton(label, formatDate(text0, dateFormat).ifBlank { "Not set" }, Icons.Outlined.CalendarMonth, canEdit, onClear = if (text0.isNotBlank() && canEdit) ({ onSave(JsonNull) }) else null) { picking = true }
            if (picking) {
                val st = rememberDatePickerState(initialSelectedDateMillis = runCatching { LocalDate.parse(text0.take(10)).atStartOfDay().toInstant(ZoneOffset.UTC).toEpochMilli() }.getOrNull())
                DatePickerDialog(onDismissRequest = { picking = false }, confirmButton = {
                    TextButton({ picking = false; st.selectedDateMillis?.let { onSave(JsonPrimitive(Instant.ofEpochMilli(it).atZone(ZoneOffset.UTC).toLocalDate().toString())) } }) { Text("OK") }
                }, dismissButton = { TextButton({ picking = false }) { Text("Cancel") } }) { DatePicker(st) }
            }
        }
        "select" -> SelectField(label, listOf("" to "—") + f.options.choices.map { it to it }, text0, Modifier.fillMaxWidth(), enabled = canEdit) { onSave(if (it.isEmpty()) JsonNull else JsonPrimitive(it)) }
        "multiselect" -> {
            val chosen = (value as? JsonArray)?.mapNotNull { (it as? JsonPrimitive)?.contentOrNull }.orEmpty()
            Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
                Text(label, style = MaterialTheme.typography.labelLarge, color = MaterialTheme.colorScheme.onSurfaceVariant)
                FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                    f.options.choices.forEach { ch ->
                        val on = ch in chosen
                        FilterChip(on, { onSave(JsonArray((if (on) chosen - ch else chosen + ch).map { JsonPrimitive(it) })) }, label = { Text(ch) }, enabled = canEdit)
                    }
                }
            }
        }
        else -> {
            var text by remember(f.id, text0) { mutableStateOf(text0) }
            val numeric = f.dataType == "integer" || f.dataType == "decimal" || f.dataType == "monetary"
            fun commit() {
                if (text == text0) return
                val t = text.trim()
                onSave(when {
                    t.isEmpty() -> JsonNull
                    f.dataType == "integer" -> t.toLongOrNull()?.let { JsonPrimitive(it) } ?: JsonPrimitive(t)
                    numeric -> t.replace(",", "").toBigDecimalOrNull()?.let { JsonPrimitive(it) } ?: JsonPrimitive(t)
                    else -> JsonPrimitive(t)
                })
            }
            OutlinedTextField(
                text, { text = it }, label = { Text(label) }, modifier = Modifier.fillMaxWidth(), enabled = canEdit, singleLine = f.dataType != "longtext", minLines = if (f.dataType == "longtext") 3 else 1,
                trailingIcon = { if (text != text0) IconButton(::commit) { Icon(Icons.Filled.Check, "Save ${f.name}") } },
                keyboardOptions = androidx.compose.foundation.text.KeyboardOptions(keyboardType = when { numeric -> KeyboardType.Decimal; f.dataType == "url" -> KeyboardType.Uri; else -> KeyboardType.Text }, imeAction = if (f.dataType == "longtext") androidx.compose.ui.text.input.ImeAction.Default else androidx.compose.ui.text.input.ImeAction.Done),
                keyboardActions = androidx.compose.foundation.text.KeyboardActions(onDone = { commit() }),
            )
        }
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
                    Text("$label: ${s.value.name}" + if (s.value.isNew) " (new)" else "", style = MaterialTheme.typography.labelLarge)
                    IconButton({ vm.resolve(true, listOf(s.id)) }, Modifier.size(36.dp)) { Icon(Icons.Filled.Check, "Accept", Modifier.size(18.dp), tint = MaterialTheme.colorScheme.primary) }
                    IconButton({ vm.resolve(false, listOf(s.id)) }, Modifier.size(36.dp)) { Icon(Icons.Outlined.Close, "Dismiss", Modifier.size(18.dp)) }
                }
            }
        }
    }
}

private val mentionRe = Regex("@\\[([^\\]]{1,80})]\\(([0-9a-fA-F-]{36})\\)")

@Composable
private fun NotesTab(vm: DocViewModel, canEdit: Boolean) {
    val c = LocalContainer.current
    var text by remember { mutableStateOf("") }
    var mentioning by remember { mutableStateOf(false) }
    val confirm = rememberConfirmation()
    val people = rememberLoader { runCatching { c.repo.directory() }.getOrDefault(emptyList()) }
    Column(Modifier.fillMaxSize().imePadding()) {
        LazyColumn(Modifier.weight(1f), contentPadding = androidx.compose.foundation.layout.PaddingValues(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            if (vm.notes.isEmpty()) item { Text("No notes yet. Notes are good for reminders like “claim submitted on 12 March”.", color = MaterialTheme.colorScheme.onSurfaceVariant) }
            items(vm.notes, key = { it.id }) { n ->
                Column(Modifier.fillMaxWidth().clip(RoundedCornerShape(12.dp)).background(MaterialTheme.colorScheme.surfaceContainer).padding(12.dp)) {
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        Text(n.author?.name ?: "Former member", style = MaterialTheme.typography.labelLarge, modifier = Modifier.weight(1f))
                        Text(timeAgo(n.createdAt), style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                        if (n.canDelete) IconButton({ confirm.ask("Delete this note?", "This can't be undone.", "Delete note", true) { vm.deleteNote(n) } }, Modifier.size(28.dp)) { Icon(Icons.Outlined.Delete, "Delete note", Modifier.size(16.dp)) }
                    }
                    SelectionContainer { Text(n.body.replace(mentionRe, "@$1"), style = MaterialTheme.typography.bodyMedium) }
                }
            }
        }
        if (canEdit) Row(Modifier.fillMaxWidth().padding(12.dp), verticalAlignment = Alignment.Bottom) {
            IconButton({ mentioning = true }) { Icon(Icons.Outlined.AlternateEmail, "Mention someone") }
            OutlinedTextField(text, { text = it }, Modifier.weight(1f), placeholder = { Text("Add a note…") }, maxLines = 4, shape = RoundedCornerShape(20.dp))
            Spacer(Modifier.width(8.dp))
            IconButton({ vm.addNote(text.trim()); text = "" }, enabled = text.isNotBlank()) { Icon(Icons.AutoMirrored.Outlined.Send, "Add note") }
        }
    }
    if (mentioning) AlertDialog(
        onDismissRequest = { mentioning = false }, title = { Text("Mention someone") },
        text = {
            Column(Modifier.verticalScroll(rememberScrollState())) {
                Text("They get a notification. Only people with access to this space can see the note.", style = MaterialTheme.typography.bodySmall)
                people.data.orEmpty().forEach { p ->
                    Row(Modifier.fillMaxWidth().clickable { text = (text.trimEnd() + " @[${p.displayName}](${p.id}) ").trimStart(); mentioning = false }.padding(vertical = 10.dp), verticalAlignment = Alignment.CenterVertically) {
                        Initials(p.displayName, 28.dp)
                        Spacer(Modifier.width(10.dp))
                        Text(p.displayName)
                    }
                }
            }
        },
        confirmButton = { TextButton({ mentioning = false }) { Text("Close") } },
    )
}

@Composable
private fun TextTab(doc: Document) {
    val c = LocalContainer.current
    val ctx = LocalContext.current
    val pages = rememberLoader(doc.id, doc.version, doc.status) { c.repo.pages(doc.id) }
    Loaded(pages) { list ->
        if (list.isEmpty() || list.all { it.text.isBlank() }) EmptyState(Icons.AutoMirrored.Outlined.TextSnippet, "No text yet", "Text appears here once Docveta has read the document.")
        else Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            OutlinedButton({ copyText(ctx, "Document text", list.joinToString("\n\n") { it.text }) }) { Icon(Icons.Outlined.ContentCopy, null, Modifier.size(18.dp)); Spacer(Modifier.width(8.dp)); Text("Copy all") }
            list.forEach { p ->
                Text("Page ${p.pageNo}" + (p.confidence?.let { " · ${(it * 100).toInt()}% confidence" } ?: ""), style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
                SelectionContainer { Text(p.text.ifBlank { "(no text)" }, Modifier.fillMaxWidth().clip(RoundedCornerShape(10.dp)).background(MaterialTheme.colorScheme.surfaceContainer).padding(12.dp), style = MaterialTheme.typography.bodyMedium) }
            }
        }
    }
}

@Composable
private fun VersionsTab(vm: DocViewModel, doc: Document, canEdit: Boolean) {
    val c = LocalContainer.current
    val ctx = LocalContext.current
    val me = LocalMe.current
    val scope = rememberCoroutineScope()
    val confirm = rememberConfirmation()
    val versions = rememberLoader(doc.id, doc.version) { c.repo.versions(doc.id) }
    val pick = rememberLauncherForActivityResult(ActivityResultContracts.OpenDocument()) { uri ->
        if (uri != null) scope.launch {
            runCatching { copyToCache(ctx, uri) }.onSuccess { (f, name, mime) -> vm.uploadVersion(f, mime, name) { versions.reload() } }.onFailure { toast(ctx, "Couldn't read that file") }
        }
    }
    Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(vertical = 8.dp)) {
        Hint("Every file this document has had. A new upload or a page change adds one; restoring brings an old one back.")
        if (canEdit) FilledTonalButton({ pick.launch(arrayOf("*/*")) }, Modifier.padding(horizontal = 16.dp, vertical = 6.dp), enabled = !vm.busy) {
            Icon(Icons.Outlined.Upload, null, Modifier.size(18.dp)); Spacer(Modifier.width(8.dp)); Text(if (vm.busy) "Uploading…" else "Upload a new version")
        }
        Loaded(versions) { list ->
            list.forEach { v ->
                ItemRow("Version ${v.versionNo}", listOfNotNull(formatDateTime(v.createdAt, me.dateFormat), formatBytes(v.sizeBytes), v.createdBy?.name, v.note.ifBlank { null }).joinToString(" · "), badges = { if (v.current) StatusBadge("Current", "accent") }) {
                    IconButton({ scope.launch { vm.downloaded("original", v.versionNo)?.let { shareFile(ctx, it, v.mimeType.ifBlank { doc.mimeType }) } } }) { Icon(Icons.Outlined.Share, "Share version ${v.versionNo}") }
                    if (canEdit && !v.current) TextButton({ confirm.ask("Go back to version ${v.versionNo}?", "It becomes the current file. Nothing is lost: the current one stays in this list.", "Restore") { vm.restoreVersion(v.versionNo) { versions.reload() } } }) { Text("Restore") }
                }
            }
        }
    }
}

@Composable
private fun SimilarTab(doc: Document, onOpenDoc: (String) -> Unit) {
    val c = LocalContainer.current
    val me = LocalMe.current
    val similar = rememberLoader(doc.id) { c.repo.similar(doc.id) }
    when {
        similar.loading && similar.data == null -> LoadingBox()
        similar.data.isNullOrEmpty() -> EmptyState(Icons.Outlined.AutoAwesome, "Nothing similar yet", if (similar.error != null) "Similar documents need an AI provider with an embedding model (Administration, AI)." else "Once more documents are added, related ones show up here.")
        else -> LazyColumn(Modifier.fillMaxSize()) {
            items(similar.data!!, key = { it.document.id }) { s ->
                Column {
                    DocumentRow(s.document, me.spaces, me.dateFormat, { c.repo.thumbnailUrl(it) }, onClick = { onOpenDoc(s.document.id) })
                    Text(if (s.reason == "meaning") "Similar content · ${(s.score * 100).toInt()}%" else "Same sender or type", Modifier.padding(start = 82.dp, bottom = 6.dp), style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                }
            }
        }
    }
}

private val historyLabels = mapOf(
    "created" to "Uploaded", "updated" to "Edited", "trashed" to "Moved to Trash", "restored" to "Restored from Trash", "ocr_queued" to "Waiting for text recognition",
    "text_extracted" to "Text recognised", "auto_classified" to "Organised automatically", "archive_created" to "Searchable PDF created", "processing_failed" to "Processing failed",
    "reprocess_requested" to "Reprocessing requested", "asn_assigned" to "Archive number assigned",
)

@Composable
private fun HistoryTab(doc: Document) {
    val c = LocalContainer.current
    val me = LocalMe.current
    val h = rememberLoader(doc.id, doc.version) { c.repo.history(doc.id) }
    Loaded(h) { list ->
        if (list.isEmpty()) EmptyState(Icons.Outlined.History, "No history")
        else LazyColumn(Modifier.fillMaxSize(), contentPadding = androidx.compose.foundation.layout.PaddingValues(vertical = 8.dp)) {
            items(list, key = { it.id }) { e ->
                val d = e.details
                val detail = when (e.action) {
                    "updated" -> d.keys.joinToString(", ") { it.removeSuffix("_ids").removeSuffix("_id").replace('_', ' ') }
                    "text_extracted" -> "${d["pages"]?.let { (it as? JsonPrimitive)?.content } ?: "?"} page(s) via ${(d["engine"] as? JsonPrimitive)?.content ?: "?"}"
                    "auto_classified" -> d.entries.joinToString(" · ") { (k, v) -> "${k.replace('_', ' ')}: ${v.display()}" }
                    "processing_failed" -> (d["error"] as? JsonPrimitive)?.content.orEmpty()
                    "created" -> (d["filename"] as? JsonPrimitive)?.content.orEmpty()
                    else -> ""
                }
                Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp)) {
                    Text((historyLabels[e.action] ?: e.action) + (e.actor?.let { " · ${it.name}" } ?: ""), style = MaterialTheme.typography.bodyMedium)
                    if (detail.isNotBlank()) Text(detail, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                    Text(formatDateTime(e.createdAt, me.dateFormat), style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.outline)
                }
            }
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
                SecretBox("Link", link!!)
                Button({ shareText(ctx, link!!, "Share link") }, Modifier.fillMaxWidth()) { Icon(Icons.Outlined.Share, null); Spacer(Modifier.width(8.dp)); Text("Send the link") }
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
        options.forEach { (v, l) -> FilterChip(selected == v, { onSelect(v) }, label = { Text(l) }) }
    }
}

/** Copies a picked file into the cache so it can be sent; returns the file, its name and type. */
suspend fun copyToCache(ctx: Context, uri: Uri): Triple<File, String, String> = kotlinx.coroutines.withContext(kotlinx.coroutines.Dispatchers.IO) {
    var name = "file"
    ctx.contentResolver.query(uri, null, null, null, null)?.use { c ->
        if (c.moveToFirst()) {
            val i = c.getColumnIndex(android.provider.OpenableColumns.DISPLAY_NAME)
            if (i >= 0) name = c.getString(i) ?: name
        }
    }
    val mime = ctx.contentResolver.getType(uri) ?: "application/octet-stream"
    val f = File(ctx.cacheDir, "outgoing/${System.currentTimeMillis()}-${name.replace(Regex("[^A-Za-z0-9._-]"), "_")}")
    f.parentFile?.mkdirs()
    ctx.contentResolver.openInputStream(uri)?.use { i -> f.outputStream().use { o -> i.copyTo(o) } } ?: throw java.io.IOException("unreadable")
    Triple(f, name, mime)
}

fun shareFile(ctx: Context, f: File, mime: String) {
    val uri = FileProvider.getUriForFile(ctx, ctx.packageName + ".files", f)
    ctx.startActivity(Intent.createChooser(Intent(Intent.ACTION_SEND).apply { type = mime.ifBlank { "*/*" }; putExtra(Intent.EXTRA_STREAM, uri); addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION) }, "Share"))
}

/** Shares several files at once (one goes through [shareFile]). */
fun shareFiles(ctx: Context, files: List<Pair<File, String>>) {
    if (files.size == 1) return shareFile(ctx, files[0].first, files[0].second)
    val uris = ArrayList(files.map { FileProvider.getUriForFile(ctx, ctx.packageName + ".files", it.first) })
    val types = files.map { it.second }.distinct()
    val type = if (types.size == 1) types[0] else if (types.all { it.startsWith("image/") }) "image/*" else "*/*"
    ctx.startActivity(Intent.createChooser(Intent(Intent.ACTION_SEND_MULTIPLE).apply { this.type = type; putParcelableArrayListExtra(Intent.EXTRA_STREAM, uris); addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION) }, "Share"))
}

fun openFile(ctx: Context, f: File, mime: String) {
    val uri = FileProvider.getUriForFile(ctx, ctx.packageName + ".files", f)
    try {
        ctx.startActivity(Intent(Intent.ACTION_VIEW).apply { setDataAndType(uri, mime.ifBlank { "*/*" }); addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION or Intent.FLAG_ACTIVITY_NEW_TASK) })
    } catch (e: android.content.ActivityNotFoundException) {
        android.widget.Toast.makeText(ctx, "No app can open this file", android.widget.Toast.LENGTH_LONG).show()
    }
}
