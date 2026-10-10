package app.docveta.android.ui

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.ArrowBack
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.CheckCircle
import androidx.compose.material.icons.outlined.AutoAwesome
import androidx.compose.material.icons.outlined.Bookmark
import androidx.compose.material.icons.outlined.Category
import androidx.compose.material.icons.outlined.BookmarkAdd
import androidx.compose.material.icons.automirrored.outlined.CallMerge
import androidx.compose.material.icons.outlined.Clear
import androidx.compose.material.icons.outlined.Close
import androidx.compose.material.icons.outlined.Delete
import androidx.compose.material.icons.outlined.DeleteForever
import androidx.compose.material.icons.outlined.Description
import androidx.compose.material.icons.automirrored.outlined.HelpOutline
import androidx.compose.material.icons.outlined.DoneAll
import androidx.compose.material.icons.automirrored.outlined.DriveFileMove
import androidx.compose.material.icons.outlined.Edit
import androidx.compose.material.icons.outlined.FolderZip
import androidx.compose.material.icons.outlined.Inbox
import androidx.compose.material.icons.automirrored.outlined.Label
import androidx.compose.material.icons.outlined.MoreVert
import androidx.compose.material.icons.outlined.RestoreFromTrash
import androidx.compose.material.icons.outlined.Search
import androidx.compose.material.icons.outlined.SearchOff
import androidx.compose.material.icons.outlined.SelectAll
import androidx.compose.material.icons.outlined.Share
import androidx.compose.material.icons.automirrored.outlined.Sort
import androidx.compose.material.icons.outlined.UploadFile
import androidx.compose.material.icons.outlined.Warning
import androidx.compose.material.icons.outlined.History
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.Checkbox
import androidx.compose.material3.DatePicker
import androidx.compose.material3.DatePickerDialog
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.InputChip
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.RadioButton
import androidx.compose.material3.SnackbarDuration
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.SnackbarResult
import androidx.compose.material3.SwipeToDismissBox
import androidx.compose.material3.SwipeToDismissBoxValue
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.material3.rememberDatePickerState
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.material3.rememberSwipeToDismissBoxState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.derivedStateOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.zIndex
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.text.withStyle
import androidx.compose.ui.unit.dp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.compose.LifecycleEventEffect
import androidx.lifecycle.viewModelScope
import app.docveta.android.AppContainer
import app.docveta.android.data.BulkResult
import app.docveta.android.data.DocQuery
import app.docveta.android.data.Document
import app.docveta.android.data.SavedView
import app.docveta.android.data.Space
import app.docveta.android.data.Taxonomy
import app.docveta.android.data.bulk
import app.docveta.android.data.createView
import app.docveta.android.data.customFields
import app.docveta.android.data.datePresets
import app.docveta.android.data.deleteView
import app.docveta.android.data.merge
import app.docveta.android.data.savedViews
import app.docveta.android.data.updateView
import app.docveta.android.data.zip
import java.time.Instant
import java.time.LocalDate
import java.time.ZoneOffset
import kotlinx.coroutines.FlowPreview
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.debounce
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.launch
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put

/** A paged list of documents for one query (the library, the Inbox, the Trash, a saved view), with selection. */
class DocsViewModel(private val c: AppContainer, initial: DocQuery) : ViewModel() {
    var query by mutableStateOf(initial)
        private set
    var items by mutableStateOf<List<Document>>(emptyList())
        private set
    var total by mutableStateOf<Int?>(null)
        private set
    var totalCapped by mutableStateOf(false)
        private set
    var loading by mutableStateOf(true)
        private set
    var refreshing by mutableStateOf(false)
        private set
    var loadingMore by mutableStateOf(false)
        private set
    var error by mutableStateOf<String?>(null)
        private set
    var foundByMeaning by mutableStateOf(false)
        private set

    /** A new search is on its way while the previous results are still on screen. */
    var searching by mutableStateOf(false)
        private set

    /** Goes up when the list shows the results of a different query (the list then scrolls to the top). */
    var generation by mutableStateOf(0)
        private set

    /** Selected documents, in the order they were picked (merge keeps that order). */
    var selected by mutableStateOf<List<String>>(emptyList())
    val selecting get() = selected.isNotEmpty()

    private var cursor: String? = null
    private var job: Job? = null
    private var moreJob: Job? = null
    private var request = 0 // the newest request; answers to older ones are dropped
    private var shown: DocQuery? = null // the query whose results are on screen
    private val search = MutableStateFlow(initial.q.trim())
    private var loadedQ = initial.q.trim() // the words the list on screen was searched for

    init {
        @OptIn(FlowPreview::class)
        viewModelScope.launch {
            // Compared with what was loaded, not with query.q: typing() has already set that.
            search.debounce(300).distinctUntilChanged().collect { if (it != loadedQ) reload() }
        }
        reload()
    }

    /** Called on every keystroke; the list reloads once typing pauses. */
    fun typing(text: String) {
        query = query.copy(q = text)
        search.value = text.trim()
    }

    /** The keyboard's Search key: no need to wait for the pause. */
    fun searchNow() {
        c.session.rememberSearch(query.q)
        if (query.q.trim() != loadedQ) reload()
    }

    /** A document opened from search results: the search was worth remembering. */
    fun opened() = c.session.rememberSearch(query.q)

    @JvmName("applyQuery") // the property's private setter is already setQuery on the JVM
    fun setQuery(q: DocQuery) {
        query = q
        search.value = q.q.trim()
        reload()
    }

    fun reload(pull: Boolean = false) {
        job?.cancel()
        moreJob?.cancel() // a next page of the old query must not land in the new list
        loadingMore = false
        cursor = null
        val q = query
        loadedQ = q.q.trim()
        val id = ++request
        if (pull) refreshing = true else if (items.isEmpty()) loading = true else searching = true
        job = viewModelScope.launch {
            try {
                val r = c.repo.documents(q)
                if (id != request) return@launch
                items = r.items
                total = r.total
                totalCapped = r.totalCapped
                cursor = r.nextCursor
                foundByMeaning = r.mode == "hybrid" || r.mode == "semantic"
                error = null
                val ids = r.items.mapTo(HashSet()) { it.id }
                selected = selected.filter { it in ids }
                if (shown != null && shown != q) generation++
                shown = q
            } catch (e: Exception) {
                if (e is kotlinx.coroutines.CancellationException) throw e
                if (id == request) error = e.friendly()
            } finally {
                // A cancelled request finishes after its replacement started: leave the flags to that one.
                if (id == request) {
                    loading = false
                    refreshing = false
                    searching = false
                }
            }
        }
    }

    fun loadMore() {
        val cur = cursor ?: return
        if (loadingMore || loading || searching) return
        loadingMore = true
        val id = request
        val q = query
        moreJob = viewModelScope.launch {
            try {
                val r = c.repo.documents(q, cur)
                if (id != request) return@launch
                val have = items.mapTo(HashSet()) { it.id }
                items = items + r.items.filter { it.id !in have }
                cursor = r.nextCursor
            } catch (e: Exception) {
                if (e is kotlinx.coroutines.CancellationException) throw e
                if (id == request) error = e.friendly()
            } finally {
                if (id == request) loadingMore = false
            }
        }
    }

    fun toggle(id: String) {
        selected = if (id in selected) selected - id else selected + id
    }

    fun selectAll() {
        selected = selected + items.map { it.id }.filter { it !in selected }
    }

    /** Removes a row at once (swipe) and asks the server; returns false if the server refused, so the caller can bring it back. */
    suspend fun review(d: Document): Boolean = try {
        items = items.filterNot { it.id == d.id }
        total = total?.minus(1)
        c.repo.markReviewed(d.id)
        true
    } catch (e: Exception) {
        if (e is kotlinx.coroutines.CancellationException) throw e
        error = e.friendly()
        false
    }

    suspend fun undoReview(d: Document) {
        runCatching { c.repo.markReviewed(d.id, reviewed = false) }
        reload()
    }

    /** Everything in the Inbox, not only the loaded page; the server works through it in batches. */
    suspend fun reviewWholeInbox(): Int {
        var done = 0
        do {
            val r = c.repo.bulk(emptyList(), "update", buildJsonObject { put("inbox", false) }, select = "inbox")
            done += r.succeeded
        } while (r.remaining > 0 && r.succeeded > 0)
        reload()
        return done
    }

    /** Runs a bulk action on the selection, then clears it and reloads. */
    suspend fun bulk(action: String, update: kotlinx.serialization.json.JsonObject = kotlinx.serialization.json.JsonObject(emptyMap())): BulkResult {
        val r = c.repo.bulk(selected, action, update)
        selected = emptyList()
        reload()
        return r
    }

    suspend fun emptyTrash() {
        c.repo.emptyTrash()
        reload()
    }
}

/** "3 done" or "2 done, 1 failed: <reason>". */
fun BulkResult.describe(success: String): String =
    if (failed.isEmpty()) success else "$succeeded done, ${failed.size} failed: ${failed.first().message}"

@OptIn(ExperimentalFoundationApi::class)
@Composable
fun DocumentRow(doc: Document, spaces: List<Space>, dateFormat: String, thumb: (Document) -> String, onClick: () -> Unit, modifier: Modifier = Modifier, selected: Boolean = false, onLongClick: (() -> Unit)? = null, trailing: @Composable (() -> Unit)? = null) {
    val space = spaces.firstOrNull { it.id == doc.space.id }
    Row(
        modifier.fillMaxWidth().background(if (selected) MaterialTheme.colorScheme.primaryContainer.copy(alpha = 0.5f) else androidx.compose.ui.graphics.Color.Transparent)
            .combinedClickable(onClick = onClick, onLongClick = onLongClick).padding(horizontal = 16.dp, vertical = 10.dp),
        verticalAlignment = Alignment.Top,
    ) {
        Box {
            Thumbnail(doc, thumb(doc), Modifier.size(width = 52.dp, height = 68.dp))
            if (selected) Icon(Icons.Filled.CheckCircle, "Selected", Modifier.align(Alignment.TopEnd).padding(2.dp).size(20.dp).clip(CircleShape).background(MaterialTheme.colorScheme.surface), tint = MaterialTheme.colorScheme.primary)
        }
        Spacer(Modifier.width(14.dp))
        Column(Modifier.weight(1f)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(doc.title, style = MaterialTheme.typography.titleSmall, maxLines = 2, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f, fill = false))
                if (doc.inbox && doc.status == "ready") {
                    Spacer(Modifier.width(6.dp))
                    Pill("New", MaterialTheme.colorScheme.primaryContainer, MaterialTheme.colorScheme.onPrimaryContainer)
                }
            }
            val sub = listOfNotNull(doc.correspondent?.name, formatDate(doc.documentDate, dateFormat).ifBlank { null }, doc.documentType?.name).joinToString(" · ")
            if (sub.isNotBlank()) Text(sub, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1, overflow = TextOverflow.Ellipsis)
            if (doc.snippet.isNotEmpty()) {
                val hitBack = MaterialTheme.colorScheme.tertiaryContainer
                val hitText = MaterialTheme.colorScheme.onTertiaryContainer
                Text(
                    androidx.compose.ui.text.buildAnnotatedString {
                        doc.snippet.forEach { s ->
                            if (s.hit) withStyle(androidx.compose.ui.text.SpanStyle(fontWeight = FontWeight.SemiBold, color = hitText, background = hitBack)) { append(s.text) } else append(s.text)
                        }
                    },
                    style = MaterialTheme.typography.bodySmall, maxLines = 2, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(top = 2.dp),
                )
            }
            Spacer(Modifier.height(4.dp))
            Row(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalAlignment = Alignment.CenterVertically) {
                StatusPill(doc)
                if (doc.suggestionCount > 0) Pill("${doc.suggestionCount}", MaterialTheme.colorScheme.primaryContainer, MaterialTheme.colorScheme.onPrimaryContainer, icon = Icons.Outlined.AutoAwesome)
                doc.tags.take(2).forEach { TagChip(it) }
                if (doc.tags.size > 2) Text("+${doc.tags.size - 2}", style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                if (spaces.size > 1) {
                    Spacer(Modifier.weight(1f))
                    SpaceDot(space)
                    Text(space?.label ?: doc.space.name, style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1)
                }
            }
        }
        trailing?.invoke()
    }
}

private fun sortOptions(q: DocQuery): List<Pair<String?, String>> = buildList {
    add(null to if (q.q.isBlank()) "Newest added" else "Best match")
    if (q.q.isNotBlank()) add("-added" to "Newest added")
    add("added" to "Oldest added")
    add("-date" to "Document date (newest)")
    add("date" to "Document date (oldest)")
    add("title" to "Title A–Z")
    add("-updated" to "Recently changed")
}

/** The library: search, filters, saved views, selection and bulk actions. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun DocumentsScreen(spaceFilter: String? = null, onOpen: (String) -> Unit, onScan: () -> Unit, onOpenView: (String) -> Unit) {
    val c = LocalContainer.current
    val vm = container("docs") { DocsViewModel(it, DocQuery(spaceId = spaceFilter)) }
    val views = rememberLoader { runCatching { c.repo.savedViews() }.getOrDefault(emptyList()) }
    var saving by remember { mutableStateOf(false) }
    LifecycleEventEffect(Lifecycle.Event.ON_RESUME) { views.reload() }
    DocumentBrowser(
        vm, onOpen, onScan, title = null,
        chipsStart = {
            views.data.orEmpty().filter { it.pinned }.forEach { v ->
                item(key = "view-${v.id}") { FilterChip(false, { onOpenView(v.id) }, label = { Text(v.name) }, leadingIcon = { Icon(Icons.Outlined.Bookmark, null, Modifier.size(16.dp)) }) }
            }
        },
        chipsEnd = {
            if (vm.query.q.isNotBlank() || vm.query.filterCount > 0) item(key = "save") {
                FilterChip(false, { saving = true }, label = { Text("Save view") }, leadingIcon = { Icon(Icons.Outlined.BookmarkAdd, null, Modifier.size(16.dp)) })
            }
        },
    )
    if (saving) SaveViewDialog(vm.query, onDismiss = { saving = false }) { views.reload() }
}

/** A saved view: its documents, with the filters it was saved with (change and save them again, rename, delete). */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun SavedViewScreen(id: String, onBack: () -> Unit, onOpen: (String) -> Unit) {
    val c = LocalContainer.current
    val run = rememberRunner()
    val confirm = rememberConfirmation()
    val view = rememberLoader(id) { c.repo.savedViews().firstOrNull { it.id == id } }
    val v = view.data
    if (v == null) {
        Page("Saved view", onBack) {
            when {
                view.error != null -> ErrorState(view.error!!) { view.reload() }
                view.loading -> LoadingBox(Modifier.fillMaxWidth().padding(48.dp))
                else -> EmptyState(Icons.Outlined.Bookmark, "This view is gone", modifier = Modifier.fillMaxWidth())
            }
        }
        return
    }
    val vm = container("view-$id") { DocsViewModel(it, DocQuery.fromJson(v.query)) }
    var renaming by remember { mutableStateOf(false) }
    var menu by remember { mutableStateOf(false) }
    val changed = vm.query.copy(q = vm.query.q.trim()).toJson() != DocQuery.fromJson(v.query).toJson()
    Box(Modifier.fillMaxSize().navigationBarsPadding()) {
        DocumentBrowser(vm, onOpen, onScan = {}, title = v.name, onBack = onBack, titleActions = {
            if (changed && v.canEdit) TextButton({ run.run("View saved") { view.data = c.repo.updateView(v.id, buildJsonObject { put("query", vm.query.toJson()) }) } }) { Text("Save") }
            if (v.canEdit) Box {
                IconButton({ menu = true }) { Icon(Icons.Outlined.MoreVert, "More") }
                DropdownMenu(menu, { menu = false }) {
                    DropdownMenuItem(text = { Text("Rename") }, leadingIcon = { Icon(Icons.Outlined.Edit, null) }, onClick = { menu = false; renaming = true })
                    DropdownMenuItem(text = { Text(if (v.pinned) "Unpin from Documents" else "Pin to Documents") }, leadingIcon = { Icon(Icons.Outlined.Bookmark, null) }, onClick = {
                        menu = false
                        run.run { view.data = c.repo.updateView(v.id, buildJsonObject { put("pinned", !v.pinned) }) }
                    })
                    DropdownMenuItem(text = { Text("Delete view") }, leadingIcon = { Icon(Icons.Outlined.Delete, null) }, onClick = {
                        menu = false
                        confirm.ask("Delete “${v.name}”?", "Only the saved view goes; no documents are deleted.", "Delete view", true) { run.run { c.repo.deleteView(v.id); onBack() } }
                    })
                }
            }
        })
    }
    if (renaming) TextInputDialog("Rename view", "Name", v.name, onDismiss = { renaming = false }) { n -> run.run { view.data = c.repo.updateView(v.id, buildJsonObject { put("name", n) }) } }
}

@Composable
private fun SaveViewDialog(q: DocQuery, onDismiss: () -> Unit, onSaved: (SavedView) -> Unit) {
    val c = LocalContainer.current
    val me = LocalMe.current
    val run = rememberRunner()
    var name by remember { mutableStateOf(q.q.trim().take(40)) }
    val space = me.spaces.firstOrNull { it.id == q.spaceId && !it.isPersonal }
    var share by remember { mutableStateOf(false) }
    AlertDialog(
        onDismissRequest = onDismiss, title = { Text("Save view") },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Text("Keeps this search and its filters. Pinned views show at the top of Documents.", style = MaterialTheme.typography.bodyMedium)
                OutlinedTextField(name, { name = it }, label = { Text("Name") }, singleLine = true, modifier = Modifier.fillMaxWidth())
                if (space != null) Row(verticalAlignment = Alignment.CenterVertically) { Checkbox(share, { share = it }); Text("Share with everyone in ${space.name}") }
            }
        },
        confirmButton = { TextButton({ run.run("View saved") { onSaved(c.repo.createView(name.trim(), q, if (share) space?.id else null)); onDismiss() } }, enabled = name.isNotBlank() && !run.busy) { Text("Save") } },
        dismissButton = { TextButton(onDismiss) { Text("Cancel") } },
    )
}

/**
 * The searchable, filterable, selectable document list used by Documents and saved views.
 * [title] null shows the big search bar (Documents tab); otherwise a top bar with Back.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun DocumentBrowser(
    vm: DocsViewModel,
    onOpen: (String) -> Unit,
    onScan: () -> Unit,
    title: String?,
    onBack: (() -> Unit)? = null,
    titleActions: @Composable () -> Unit = {},
    chipsStart: androidx.compose.foundation.lazy.LazyListScope.() -> Unit = {},
    chipsEnd: androidx.compose.foundation.lazy.LazyListScope.() -> Unit = {},
) {
    val me = LocalMe.current
    val pickFiles = LocalPickFiles.current
    val ai by produceAi()
    var filters by remember { mutableStateOf(false) }
    var sortMenu by remember { mutableStateOf(false) }
    val listState = rememberLazyListState()
    val container = LocalContainer.current
    val session = container.session
    var recent by remember { mutableStateOf(session.recentSearches) }
    var recentDocs by remember { mutableStateOf(session.recentDocs) }
    LifecycleEventEffect(Lifecycle.Event.ON_RESUME) { recentDocs = session.recentDocs } // back from a document
    var tips by remember { mutableStateOf(false) }
    // Document types that have documents, most used first. The same name can exist in several
    // spaces: it is one chip that filters by all of them.
    val typeLoader = rememberLoader(vm.query.spaceId) { runCatching { container.repo.taxonomy("document-types", vm.query.spaceId) }.getOrDefault(emptyList()) }
    LifecycleEventEffect(Lifecycle.Event.ON_RESUME) { typeLoader.reload() } // a document may have got a new type meanwhile
    val types = remember(typeLoader.data) {
        typeLoader.data.orEmpty().filter { it.documentCount > 0 }.groupBy { it.name.lowercase() }
            .map { (_, same) -> Triple(same.first().name, same.map { it.id }, same.sumOf { it.documentCount }) }
            .sortedWith(compareByDescending<Triple<String, List<String>, Int>> { it.third }.thenBy { it.first.lowercase() }).take(12)
    }
    val openDoc: (String) -> Unit = { id -> vm.opened(); recent = session.recentSearches; onOpen(id) }
    val nearEnd by remember { derivedStateOf { val last = listState.layoutInfo.visibleItemsInfo.lastOrNull()?.index ?: 0; last >= vm.items.size - 6 } }
    LaunchedEffect(nearEnd, vm.items.size) { if (nearEnd && vm.items.isNotEmpty()) vm.loadMore() }
    BackHandler(vm.selecting) { vm.selected = emptyList() }
    // Back with a search typed clears it before it leaves the screen (or the app).
    BackHandler(!vm.selecting && title == null && vm.query.q.isNotEmpty()) { vm.typing(""); vm.searchNow() }
    // Reading results needs the space the keyboard takes: put it away once the list is scrolled.
    val keyboard = androidx.compose.ui.platform.LocalSoftwareKeyboardController.current
    LaunchedEffect(listState.isScrollInProgress) { if (listState.isScrollInProgress) keyboard?.hide() }

    val undo = remember { SnackbarHostState() }
    val undoScope = rememberCoroutineScope()
    Column(Modifier.fillMaxSize()) {
        if (vm.selecting) SelectionBar(vm, onTrashed = { ids, msg -> undoScope.launch { offerUndoTrash(undo, container, vm, ids, msg) } })
        else {
            if (title != null) TopAppBar(title = { Text(title, maxLines = 1, overflow = TextOverflow.Ellipsis) }, navigationIcon = { if (onBack != null) BackButton(onBack) }, actions = { titleActions() })
            SearchBar(vm.query.q, vm::typing, "Search documents", onSearch = { vm.searchNow(); recent = session.recentSearches }, trailing = {
                Box {
                    IconButton({ sortMenu = true }) { Icon(Icons.AutoMirrored.Outlined.Sort, "Sort") }
                    DropdownMenu(sortMenu, { sortMenu = false }) {
                        sortOptions(vm.query).forEach { (v, l) ->
                            DropdownMenuItem(text = { Text(l) }, trailingIcon = { if (vm.query.sort == v) Icon(Icons.Filled.Check, null) }, onClick = { sortMenu = false; vm.setQuery(vm.query.copy(sort = v)) })
                        }
                    }
                }
                if (title == null) IconButton(pickFiles) { Icon(Icons.Outlined.UploadFile, "Upload files") }
            })
            // With nothing typed: the way back to what was opened or searched for last.
            if (title == null && vm.query.q.isBlank() && (recent.isNotEmpty() || recentDocs.isNotEmpty())) LazyRow(
                contentPadding = PaddingValues(horizontal = 16.dp), horizontalArrangement = Arrangement.spacedBy(8.dp),
                modifier = Modifier.padding(bottom = 4.dp),
            ) {
                items(recentDocs.take(4), key = { "opened-${it.first}" }) { (id, name) ->
                    androidx.compose.material3.AssistChip({ onOpen(id) }, label = { Text(name, Modifier.widthIn(max = 180.dp), maxLines = 1, overflow = TextOverflow.Ellipsis) },
                        leadingIcon = { Icon(Icons.Outlined.Description, "Recently opened", Modifier.size(16.dp)) })
                }
                items(recent, key = { "recent-$it" }) { r ->
                    androidx.compose.material3.AssistChip({ vm.setQuery(vm.query.copy(q = r)) }, label = { Text(r, maxLines = 1, overflow = TextOverflow.Ellipsis) },
                        leadingIcon = { Icon(Icons.Outlined.History, null, Modifier.size(16.dp)) })
                }
                item(key = "recent-clear") { TextButton({ session.recentSearches = emptyList(); session.recentDocs = emptyList(); recent = emptyList(); recentDocs = emptyList() }) { Text("Clear") } }
            }
            LazyRow(contentPadding = PaddingValues(horizontal = 16.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                chipsStart()
                item(key = "filters") { FilterChip(vm.query.filterCount > 0, { filters = true }, label = { Text(if (vm.query.filterCount > 0) "Filters · ${vm.query.filterCount}" else "Filters") }) }
                if (ai.embeddings && vm.query.q.isNotBlank()) item(key = "meaning") {
                    FilterChip(vm.query.semantic, { vm.setQuery(vm.query.copy(semantic = !vm.query.semantic)) }, label = { Text("By meaning") }, leadingIcon = { Icon(Icons.Outlined.AutoAwesome, null, Modifier.size(16.dp)) })
                }
                item(key = "attention") {
                    FilterChip(vm.query.status == "failed,needs_password", { vm.setQuery(vm.query.copy(status = if (vm.query.status == "failed,needs_password") null else "failed,needs_password")) }, label = { Text("Needs attention") }, leadingIcon = { Icon(Icons.Outlined.Warning, null, Modifier.size(16.dp)) })
                }
                chipsEnd()
                if (vm.query.filterCount > 0) item(key = "clear") { TextButton({ vm.setQuery(vm.query.clearedFilters()) }) { Text("Clear") } }
                item(key = "tips") { androidx.compose.material3.AssistChip({ tips = true }, label = { Text("Search tips") }, leadingIcon = { Icon(Icons.AutoMirrored.Outlined.HelpOutline, null, Modifier.size(16.dp)) }) }
            }
            // Browse by what documents are: one tap shows all of a type, another shows everything again.
            if (title == null && types.isNotEmpty()) LazyRow(contentPadding = PaddingValues(horizontal = 16.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                items(types, key = { "type-${it.first}" }) { (name, ids, count) ->
                    val on = vm.query.typeIds.isNotEmpty() && ids.containsAll(vm.query.typeIds)
                    FilterChip(on, { vm.setQuery(vm.query.copy(typeIds = if (on) emptyList() else ids)) },
                        label = { Text("$name · $count", Modifier.widthIn(max = 200.dp), maxLines = 1, overflow = TextOverflow.Ellipsis) },
                        leadingIcon = { Icon(Icons.Outlined.Category, "Document type", Modifier.size(16.dp)) })
                }
            }
        }
        Spacer(Modifier.height(4.dp))
        StatsBanner()
        DocumentList(vm, me.spaces, me.dateFormat, listState, openDoc, selectable = true, empty = {
            if (vm.query.q.isNotBlank() || vm.query.filterCount > 0) EmptyState(
                Icons.Outlined.SearchOff, "No matching documents",
                if (vm.query.q.isNotBlank()) "Nothing has “${vm.query.q.trim()}”" + (if (vm.query.filterCount > 0) " with these filters" else "") +
                    ". Try fewer or other words, or the start of a word. Search reads inside scans too." else "Try removing a filter.",
            ) {
                if (vm.query.filterCount > 0) OutlinedButton({ vm.setQuery(vm.query.clearedFilters()) }) { Text("Remove filters") }
            }
            else EmptyState(Icons.Outlined.Inbox, "No documents yet", "Scan a paper document or upload a file to get started.") {
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    Button(onScan) { Text("Scan a document") }
                    OutlinedButton(pickFiles) { Text("Upload files") }
                }
            }
        }, header = {
            if (vm.total != null) Text(
                vm.total!!.toString() + (if (vm.totalCapped) "+" else "") + " document" + (if (vm.total == 1 && !vm.totalCapped) "" else "s") + if (vm.foundByMeaning) " · found by meaning" else "",
                style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(horizontal = 16.dp, vertical = 6.dp),
            )
        })
    }
    Box(Modifier.fillMaxSize()) { SnackbarHost(undo, Modifier.align(Alignment.BottomCenter).padding(bottom = 8.dp)) }
    if (filters) FilterSheet(vm.query, me.spaces, onDismiss = { filters = false }, onApply = { vm.setQuery(it); filters = false })
    if (tips) SearchTipsDialog(onDismiss = { tips = false }) { term -> tips = false; vm.setQuery(vm.query.copy(q = listOf(vm.query.q.trim(), term).filter { it.isNotEmpty() }.joinToString(" "))) }
}

/** What the search box understands beyond plain words; the same list as the web app's. */
private val searchTips = listOf(
    "tag:tax" to "has the tag",
    "from:hdfc" to "from this sender",
    "type:invoice" to "of this type",
    "date:2026" to "dated in a year, a month (2026-03) or a range (2026-01..2026-06)",
    "added:7d" to "added in the last 7 days (also 4w, 6m, 1y)",
    "is:inbox" to "not reviewed yet",
    "untagged" to "without any tag",
    "\"due date\"" to "these exact words together",
    "-draft" to "without this word (also -tag:old)",
)

@Composable
private fun SearchTipsDialog(onDismiss: () -> Unit, onPick: (String) -> Unit) {
    AlertDialog(
        onDismissRequest = onDismiss, title = { Text("Search tips") },
        text = {
            Column(Modifier.verticalScroll(rememberScrollState())) {
                Text("Tap one to add it to your search.", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                searchTips.forEach { (term, what) ->
                    Column(Modifier.fillMaxWidth().clip(RoundedCornerShape(8.dp)).clickable { onPick(term) }.padding(vertical = 8.dp)) {
                        Text(term, style = MaterialTheme.typography.bodyMedium.copy(fontFamily = androidx.compose.ui.text.font.FontFamily.Monospace), color = MaterialTheme.colorScheme.primary)
                        Text(what, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                }
                Text("Names with spaces go in quotes: from:\"State Bank\". Search also reads the text inside scans.", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(top = 8.dp))
            }
        },
        confirmButton = { TextButton(onDismiss) { Text("Close") } },
    )
}

/** After documents were moved to Trash: says so, and brings them back on Undo. */
private suspend fun offerUndoTrash(snack: SnackbarHostState, c: AppContainer, vm: DocsViewModel, ids: List<String>, message: String) {
    if (snack.showSnackbar(message, "Undo", duration = SnackbarDuration.Long) != SnackbarResult.ActionPerformed) return
    runCatching { c.repo.bulk(ids, "restore") }
    vm.reload()
}

/** Replaces the search bar while documents are selected: what can be done with all of them. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun SelectionBar(vm: DocsViewModel, trash: Boolean = false, onTrashed: ((ids: List<String>, message: String) -> Unit)? = null) {
    val c = LocalContainer.current
    val ctx = LocalContext.current
    val run = rememberRunner()
    val confirm = rememberConfirmation()
    var dialog by remember { mutableStateOf<String?>(null) }
    var menu by remember { mutableStateOf(false) }
    val n = vm.selected.size
    val docs = vm.items.filter { it.id in vm.selected }
    TopAppBar(
        title = { Text("$n selected") },
        navigationIcon = { IconButton({ vm.selected = emptyList() }) { Icon(Icons.Outlined.Close, "Clear selection") } },
        actions = {
            if (vm.items.size > n) IconButton(vm::selectAll) { Icon(Icons.Outlined.SelectAll, "Select all") }
            if (trash) {
                IconButton({ run.run { toast(ctx, vm.bulk("restore").describe("Restored")) } }) { Icon(Icons.Outlined.RestoreFromTrash, "Restore") }
                IconButton({ confirm.ask("Delete ${plural(n, "document")} forever?", "This can't be undone.", "Delete forever", true) { run.run { toast(ctx, vm.bulk("purge").describe("Deleted permanently")) } } }) {
                    Icon(Icons.Outlined.DeleteForever, "Delete forever", tint = MaterialTheme.colorScheme.error)
                }
            } else {
                IconButton({ run.run { toast(ctx, vm.bulk("update", buildJsonObject { put("inbox", false) }).describe("Marked as reviewed")) } }) { Icon(Icons.Outlined.DoneAll, "Mark as reviewed") }
                IconButton({ dialog = "tags" }) { Icon(Icons.AutoMirrored.Outlined.Label, "Tags") }
                IconButton({
                    val ids = vm.selected
                    run.run {
                        val r = vm.bulk("trash")
                        val msg = r.describe("Moved ${plural(n, "document")} to Trash")
                        // The one bulk action that's easy to hit by mistake: offer the way back.
                        if (onTrashed != null && r.succeeded > 0) onTrashed(ids.filter { id -> r.failed.none { it.id == id } }, msg) else toast(ctx, msg)
                    }
                }) { Icon(Icons.Outlined.Delete, "Move to Trash") }
                Box {
                    IconButton({ menu = true }) { Icon(Icons.Outlined.MoreVert, "More") }
                    DropdownMenu(menu, { menu = false }) {
                        DropdownMenuItem(text = { Text("Move to another space") }, leadingIcon = { Icon(Icons.AutoMirrored.Outlined.DriveFileMove, null) }, onClick = { menu = false; dialog = "move" })
                        if (n > 1 && docs.size == n && docs.all { it.isPdf || it.isImage }) DropdownMenuItem(text = { Text("Merge into one PDF") }, leadingIcon = { Icon(Icons.AutoMirrored.Outlined.CallMerge, null) }, onClick = { menu = false; dialog = "merge" })
                        DropdownMenuItem(text = { Text("Share the files") }, leadingIcon = { Icon(Icons.Outlined.Share, null) }, onClick = {
                            menu = false
                            run.run { shareFiles(ctx, docs.map { d -> c.repo.originalFile(d) to d.mimeType }) }
                        })
                        if (n > 1) DropdownMenuItem(text = { Text("Share as one ZIP file") }, leadingIcon = { Icon(Icons.Outlined.FolderZip, null) }, onClick = {
                            menu = false
                            val ids = vm.selected
                            if (ids.size > 500) toast(ctx, "Choose up to 500 documents for one ZIP")
                            else { toast(ctx, "Preparing the ZIP…"); run.run { shareFile(ctx, c.repo.zip(ids), "application/zip") } }
                        })
                        DropdownMenuItem(text = { Text("Process again") }, onClick = { menu = false; run.run { toast(ctx, vm.bulk("reprocess").describe("Processing again")) } })
                        if (LocalAi.current.value.chat) DropdownMenuItem(text = { Text("Suggest tags & type with AI") }, onClick = { menu = false; run.run { toast(ctx, vm.bulk("suggest").describe("Asked AI for suggestions")) } })
                    }
                }
            }
        },
    )
    when (dialog) {
        "tags" -> BulkTagsDialog(onDismiss = { dialog = null }) { add, remove ->
            run.run { toast(ctx, vm.bulk("update", buildJsonObject { put("add_tag_names", JsonArray(add.map { JsonPrimitive(it) })); put("remove_tag_names", JsonArray(remove.map { JsonPrimitive(it) })) }).describe("Tags changed")) }
        }
        "move" -> MoveDialog(onDismiss = { dialog = null }) { space -> run.run { toast(ctx, vm.bulk("update", buildJsonObject { put("space_id", space) }).describe("Moved")) } }
        "merge" -> {
            var title by remember { mutableStateOf("${docs.firstOrNull()?.title.orEmpty()} (merged)") }
            var trashOriginals by remember { mutableStateOf(false) }
            AlertDialog(
                onDismissRequest = { dialog = null }, title = { Text("Merge ${plural(n, "document")}") },
                text = {
                    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                        Text("Creates one new PDF with all their pages, in the order you selected them.", style = MaterialTheme.typography.bodyMedium)
                        OutlinedTextField(title, { title = it }, label = { Text("Title of the new document") }, singleLine = true, modifier = Modifier.fillMaxWidth())
                        Row(verticalAlignment = Alignment.CenterVertically) { Checkbox(trashOriginals, { trashOriginals = it }); Text("Move the originals to Trash afterwards") }
                    }
                },
                confirmButton = {
                    TextButton({
                        val ids = vm.selected
                        dialog = null
                        run.run("Merged ${plural(ids.size, "document")}") { c.repo.merge(ids, title.trim(), trashOriginals); vm.selected = emptyList(); vm.reload() }
                    }, enabled = title.isNotBlank()) { Text("Merge") }
                },
                dismissButton = { TextButton({ dialog = null }) { Text("Cancel") } },
            )
        }
    }
}

/** Tag names work across spaces: each document gets the tag in its own space (created if missing). */
@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun BulkTagsDialog(onDismiss: () -> Unit, onApply: (List<String>, List<String>) -> Unit) {
    val c = LocalContainer.current
    var add by remember { mutableStateOf<List<String>>(emptyList()) }
    var remove by remember { mutableStateOf<List<String>>(emptyList()) }
    val names = rememberLoader { runCatching { c.repo.taxonomy("tags", null) }.getOrDefault(emptyList()).map { it.name }.distinctBy { it.lowercase() }.sortedBy { it.lowercase() } }
    AlertDialog(
        onDismissRequest = onDismiss, title = { Text("Change tags") },
        text = {
            Column(Modifier.verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                Text("Works across spaces: each document gets the tag in its own space.", style = MaterialTheme.typography.bodySmall)
                TagNamesInput("Add tags", add, names.data.orEmpty()) { add = it }
                TagNamesInput("Remove tags", remove, names.data.orEmpty()) { remove = it }
            }
        },
        confirmButton = { TextButton({ onDismiss(); onApply(add, remove) }, enabled = add.isNotEmpty() || remove.isNotEmpty()) { Text("Apply") } },
        dismissButton = { TextButton(onDismiss) { Text("Cancel") } },
    )
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun TagNamesInput(label: String, value: List<String>, suggestions: List<String>, onChange: (List<String>) -> Unit) {
    var text by remember { mutableStateOf("") }
    fun commit(n: String = text) {
        val t = n.trim().trimEnd(',').trim()
        if (t.isNotEmpty() && value.none { it.equals(t, ignoreCase = true) }) onChange(value + t)
        text = ""
    }
    Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
        Text(label, style = MaterialTheme.typography.labelLarge)
        if (value.isNotEmpty()) FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
            value.forEach { v -> InputChip(true, { onChange(value - v) }, label = { Text(v) }, trailingIcon = { Icon(Icons.Outlined.Close, "Remove", Modifier.size(16.dp)) }) }
        }
        OutlinedTextField(text, { if (it.endsWith(",")) commit(it) else text = it }, placeholder = { Text("Type a tag, then Add") }, singleLine = true, modifier = Modifier.fillMaxWidth(),
            trailingIcon = { if (text.isNotBlank()) TextButton({ commit() }) { Text("Add") } },
            keyboardOptions = androidx.compose.foundation.text.KeyboardOptions(imeAction = androidx.compose.ui.text.input.ImeAction.Done), keyboardActions = androidx.compose.foundation.text.KeyboardActions(onDone = { commit() }))
        val matches = if (text.isBlank()) emptyList() else suggestions.filter { it.contains(text.trim(), ignoreCase = true) && value.none { v -> v.equals(it, true) } }.take(6)
        if (matches.isNotEmpty()) FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp)) { matches.forEach { m -> FilterChip(false, { commit(m) }, label = { Text(m) }) } }
    }
}

@Composable
fun MoveDialog(currentSpace: String? = null, onDismiss: () -> Unit, onApply: (String) -> Unit) {
    val me = LocalMe.current
    val writable = me.spaces.filter { it.canWrite && it.id != currentSpace }
    var space by remember { mutableStateOf(writable.firstOrNull()?.id) }
    AlertDialog(
        onDismissRequest = onDismiss, title = { Text("Move to another space") },
        text = {
            Column {
                Text("Tags, sender and type are matched by name in the new space (created if missing).", style = MaterialTheme.typography.bodySmall)
                if (writable.isEmpty()) Text("There is no other space you can add documents to.", Modifier.padding(top = 8.dp))
                writable.forEach { s ->
                    Row(Modifier.fillMaxWidth().clip(RoundedCornerShape(8.dp)).clickable { space = s.id }.padding(vertical = 6.dp), verticalAlignment = Alignment.CenterVertically) {
                        RadioButton(space == s.id, { space = s.id })
                        SpaceDot(s)
                        Spacer(Modifier.width(8.dp))
                        Text(s.label)
                    }
                }
            }
        },
        confirmButton = { TextButton({ val s = space ?: return@TextButton; onDismiss(); onApply(s) }, enabled = space != null) { Text("Move") } },
        dismissButton = { TextButton(onDismiss) { Text("Cancel") } },
    )
}

@Composable
fun SearchBar(text: String, onChange: (String) -> Unit, hint: String, onSearch: () -> Unit = {}, trailing: @Composable (() -> Unit)? = null) {
    val keyboard = androidx.compose.ui.platform.LocalSoftwareKeyboardController.current
    Row(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp), verticalAlignment = Alignment.CenterVertically) {
        OutlinedTextField(
            text, onChange, Modifier.weight(1f), singleLine = true, placeholder = { Text(hint) },
            leadingIcon = { Icon(Icons.Outlined.Search, null) },
            trailingIcon = { if (text.isNotEmpty()) IconButton({ onChange(""); onSearch() }) { Icon(Icons.Outlined.Clear, "Clear search") } },
            shape = RoundedCornerShape(28.dp),
            keyboardOptions = androidx.compose.foundation.text.KeyboardOptions(imeAction = androidx.compose.ui.text.input.ImeAction.Search),
            keyboardActions = androidx.compose.foundation.text.KeyboardActions(onSearch = { keyboard?.hide(); onSearch() }),
        )
        trailing?.invoke()
    }
}

/** Warns that scans won't be read until a text-reading worker is connected. */
@Composable
fun StatsBanner() {
    val stats = LocalStats.current ?: return
    if (stats.ocrAvailable) return
    Row(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 4.dp).background(androidx.compose.ui.graphics.Color(0x22F59E0B), RoundedCornerShape(12.dp)).padding(12.dp), verticalAlignment = Alignment.Top) {
        Icon(Icons.Outlined.Warning, null, tint = androidx.compose.ui.graphics.Color(0xFFB77900), modifier = Modifier.size(18.dp))
        Spacer(Modifier.width(10.dp))
        val phone = LocalContainer.current.session.phoneOcr != "off" && app.docveta.android.scan.PhoneOcr.supported(androidx.compose.ui.platform.LocalContext.current)
        Text(
            "No text-reading worker is connected. Scans and photos wait until one is; PDFs with real text are searchable already." +
                if (phone) " Scans from this phone are read here." else "",
            style = MaterialTheme.typography.bodySmall,
        )
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun DocumentList(vm: DocsViewModel, spaces: List<Space>, dateFormat: String, state: androidx.compose.foundation.lazy.LazyListState, onOpen: (String) -> Unit, empty: @Composable () -> Unit, header: @Composable (() -> Unit)? = null, selectable: Boolean = false, rowContent: (@Composable (Document, @Composable () -> Unit) -> Unit)? = null) {
    val c = LocalContainer.current
    // Results of another search start at the top, not where the old list was scrolled to.
    LaunchedEffect(vm.generation) { if (vm.generation > 0 && vm.items.isNotEmpty()) state.scrollToItem(0) }
    PullToRefreshBox(isRefreshing = vm.refreshing, onRefresh = { vm.reload(pull = true) }, modifier = Modifier.fillMaxSize()) {
        // The old results stay while new ones load: say that something is happening.
        if (vm.searching) androidx.compose.material3.LinearProgressIndicator(Modifier.fillMaxWidth().height(2.dp).align(Alignment.TopCenter).zIndex(1f))
        when {
            vm.loading -> LoadingBox()
            vm.error != null && vm.items.isEmpty() -> ErrorState(vm.error!!) { vm.reload() }
            vm.items.isEmpty() -> empty()
            else -> LazyColumn(Modifier.fillMaxSize(), state = state, contentPadding = PaddingValues(bottom = 96.dp)) {
                vm.error?.let { err ->
                    item(key = "error") {
                        // The list below is from before: don't fail silently.
                        Row(
                            Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 6.dp)
                                .background(MaterialTheme.colorScheme.errorContainer, RoundedCornerShape(12.dp)).padding(start = 12.dp),
                            verticalAlignment = Alignment.CenterVertically,
                        ) {
                            Icon(Icons.Outlined.Warning, null, Modifier.size(18.dp), tint = MaterialTheme.colorScheme.onErrorContainer)
                            Spacer(Modifier.width(10.dp))
                            Text(err, Modifier.weight(1f), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onErrorContainer)
                            TextButton({ vm.reload() }) { Text("Retry") }
                        }
                    }
                }
                if (header != null) item { header() }
                items(vm.items, key = { it.id }) { d ->
                    val row = @Composable {
                        DocumentRow(
                            d, spaces, dateFormat, { c.repo.thumbnailUrl(it) },
                            onClick = { if (vm.selecting) vm.toggle(d.id) else onOpen(d.id) },
                            selected = d.id in vm.selected,
                            onLongClick = if (selectable) ({ vm.toggle(d.id) }) else null,
                        )
                    }
                    if (rowContent != null && !vm.selecting) rowContent(d, row) else row()
                }
                if (vm.loadingMore) item { LoadingBox(Modifier.fillMaxWidth().height(64.dp)) }
            }
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class, ExperimentalFoundationApi::class)
@Composable
fun InboxScreen(onOpen: (String) -> Unit, onScan: () -> Unit) {
    val me = LocalMe.current
    val ctx = LocalContext.current
    val vm = container("inbox") { DocsViewModel(it, DocQuery(inbox = true)) }
    val snack = remember { SnackbarHostState() }
    val undoScope = rememberCoroutineScope()
    val container = LocalContainer.current
    val run = rememberRunner()
    var confirmAll by remember { mutableStateOf(false) }
    val stats = LocalStats.current
    // Coming back from a document that was reviewed there: refresh.
    LifecycleEventEffect(Lifecycle.Event.ON_RESUME) { vm.reload() }
    BackHandler(vm.selecting) { vm.selected = emptyList() }
    val inboxTotal = vm.total ?: stats?.inbox ?: vm.items.size

    Box(Modifier.fillMaxSize()) {
        Column(Modifier.fillMaxSize()) {
            if (vm.selecting) SelectionBar(vm, onTrashed = { ids, msg -> undoScope.launch { offerUndoTrash(snack, container, vm, ids, msg) } })
            else Row(Modifier.fillMaxWidth().padding(start = 16.dp, end = 8.dp, top = 16.dp, bottom = 4.dp), verticalAlignment = Alignment.CenterVertically) {
                Column(Modifier.weight(1f)) {
                    Text("Inbox", style = MaterialTheme.typography.headlineMedium)
                    Text(if (vm.items.isEmpty()) "Nothing to review" else "Swipe right when a document looks right. Hold one to select several.", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                }
                if (vm.items.size > 1) TextButton({ confirmAll = true }) { Icon(Icons.Outlined.DoneAll, null, Modifier.size(18.dp)); Spacer(Modifier.width(6.dp)); Text("All reviewed") }
            }
            StatsBanner()
            DocumentList(vm, me.spaces, me.dateFormat, rememberLazyListState(), onOpen, selectable = true, empty = {
                EmptyState(Icons.Outlined.DoneAll, "Inbox zero", "New documents appear here so you can check what Docveta filled in.") { Button(onScan) { Text("Scan a document") } }
            }, rowContent = { d, row ->
                val state = rememberSwipeToDismissBoxState(confirmValueChange = { it == SwipeToDismissBoxValue.StartToEnd })
                LaunchedEffect(state.currentValue) {
                    if (state.currentValue == SwipeToDismissBoxValue.StartToEnd) {
                        if (vm.review(d)) {
                            val r = snack.showSnackbar("Marked as reviewed", "Undo", duration = SnackbarDuration.Short)
                            if (r == SnackbarResult.ActionPerformed) vm.undoReview(d)
                        } else vm.reload()
                    }
                }
                SwipeToDismissBox(state, enableDismissFromEndToStart = false, backgroundContent = {
                    Row(Modifier.fillMaxSize().background(MaterialTheme.colorScheme.primaryContainer).padding(start = 24.dp), verticalAlignment = Alignment.CenterVertically) {
                        Icon(Icons.Outlined.DoneAll, null, tint = MaterialTheme.colorScheme.onPrimaryContainer)
                        Spacer(Modifier.width(10.dp))
                        Text("Reviewed", color = MaterialTheme.colorScheme.onPrimaryContainer, style = MaterialTheme.typography.labelLarge)
                    }
                }) { Box(Modifier.background(MaterialTheme.colorScheme.background)) { row() } }
            })
        }
        SnackbarHost(snack, Modifier.align(Alignment.BottomCenter).padding(bottom = 8.dp))
    }
    if (confirmAll) ConfirmDialog("Mark all ${plural(inboxTotal, "document")} as reviewed?", "They leave the Inbox but stay in your documents.", "Mark all reviewed", onDismiss = { confirmAll = false }) {
        run.run { val n = vm.reviewWholeInbox(); toast(ctx, "Marked ${plural(n, "document")} as reviewed") }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun TrashScreen(onBack: () -> Unit, onOpen: (String) -> Unit) {
    val me = LocalMe.current
    val run = rememberRunner()
    val vm = container("trash") { DocsViewModel(it, DocQuery(trash = true)) }
    var confirm by remember { mutableStateOf(false) }
    LifecycleEventEffect(Lifecycle.Event.ON_RESUME) { vm.reload() }
    BackHandler(vm.selecting) { vm.selected = emptyList() }
    Column(Modifier.fillMaxSize()) {
        if (vm.selecting) SelectionBar(vm, trash = true)
        else TopAppBar(title = { Text("Trash") }, navigationIcon = { BackButton(onBack) }, actions = {
            if (vm.items.isNotEmpty()) IconButton({ confirm = true }) { Icon(Icons.Outlined.DeleteForever, "Empty Trash") }
        })
        val days = LocalStats.current?.trashRetentionDays ?: 30
        Text("Documents here are deleted for good after $days days. Open one to restore it, or hold to select several.", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(horizontal = 16.dp, vertical = 4.dp))
        DocumentList(vm, me.spaces, me.dateFormat, rememberLazyListState(), onOpen, selectable = true, empty = { EmptyState(Icons.Outlined.DeleteForever, "Trash is empty") })
    }
    if (confirm) ConfirmDialog("Empty the Trash?", "These documents will be deleted forever. This can't be undone.", "Delete forever", destructive = true, onDismiss = { confirm = false }) { run.run("Trash emptied") { vm.emptyTrash() } }
}

/* ------------------------------------------------------------------ filters */

@OptIn(ExperimentalMaterial3Api::class, ExperimentalLayoutApi::class)
@Composable
private fun FilterSheet(q: DocQuery, spaces: List<Space>, onDismiss: () -> Unit, onApply: (DocQuery) -> Unit) {
    val c = LocalContainer.current
    val me = LocalMe.current
    var draft by remember { mutableStateOf(q) }
    var tags by remember { mutableStateOf<List<Taxonomy>>(emptyList()) }
    var senders by remember { mutableStateOf<List<Taxonomy>>(emptyList()) }
    var types by remember { mutableStateOf<List<Taxonomy>>(emptyList()) }
    var picker by remember { mutableStateOf<String?>(null) }
    var datePick by remember { mutableStateOf<String?>(null) }
    LaunchedEffect(draft.spaceId) {
        tags = runCatching { c.repo.taxonomy("tags", draft.spaceId) }.getOrDefault(emptyList())
        senders = runCatching { c.repo.taxonomy("correspondents", draft.spaceId) }.getOrDefault(emptyList())
        types = runCatching { c.repo.taxonomy("document-types", draft.spaceId) }.getOrDefault(emptyList())
    }
    fun names(all: List<Taxonomy>, ids: List<String>) = ids.map { id -> all.firstOrNull { it.id == id }?.name ?: "…" }
    val presets = remember { datePresets(LocalDate.now()) }
    ModalBottomSheet(onDismissRequest = onDismiss, sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true)) {
        Column(Modifier.verticalScroll(rememberScrollState()).padding(horizontal = 20.dp).padding(bottom = 24.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            Text("Filters", style = MaterialTheme.typography.titleLarge)
            if (spaces.size > 1) {
                Text("Space", style = MaterialTheme.typography.labelLarge)
                FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    FilterChip(draft.spaceId == null, { draft = draft.copy(spaceId = null, tagIds = emptyList(), correspondentIds = emptyList(), typeIds = emptyList()) }, label = { Text("All spaces") })
                    spaces.forEach { s -> FilterChip(draft.spaceId == s.id, { draft = draft.copy(spaceId = s.id, tagIds = emptyList(), correspondentIds = emptyList(), typeIds = emptyList()) }, label = { Text(s.label) }, leadingIcon = { SpaceDot(s) }) }
                }
            }
            Column(Modifier.padding(horizontal = 0.dp)) {
                PickerField("Tags", names(tags, draft.tagIds), "Any", onClear = { draft = draft.copy(tagIds = emptyList()) }) { picker = "tags" }
                PickerField("From", names(senders, draft.correspondentIds), "Anyone", onClear = { draft = draft.copy(correspondentIds = emptyList()) }) { picker = "correspondents" }
                PickerField("Type", names(types, draft.typeIds), "Any type", onClear = { draft = draft.copy(typeIds = emptyList()) }) { picker = "document-types" }
            }
            Text("Date on the document", style = MaterialTheme.typography.labelLarge)
            FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                presets.forEach { (l, f, t) -> FilterChip(draft.dateFrom == f && draft.dateTo == t, { draft = if (draft.dateFrom == f && draft.dateTo == t) draft.copy(dateFrom = null, dateTo = null) else draft.copy(dateFrom = f, dateTo = t) }, label = { Text(l) }) }
            }
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                OutlinedButton({ datePick = "from" }, Modifier.weight(1f)) { Text(draft.dateFrom?.let { "From " + formatDate(it, me.dateFormat) } ?: "From…") }
                OutlinedButton({ datePick = "to" }, Modifier.weight(1f)) { Text(draft.dateTo?.let { "To " + formatDate(it, me.dateFormat) } ?: "To…") }
            }
            if (draft.dateFrom != null || draft.dateTo != null) TextButton({ draft = draft.copy(dateFrom = null, dateTo = null) }) { Text("Any date") }
            Text("More", style = MaterialTheme.typography.labelLarge)
            Row(verticalAlignment = Alignment.CenterVertically) { Checkbox(draft.untagged, { draft = draft.copy(untagged = it) }); Text("Without tags") }
            Text("Status", style = MaterialTheme.typography.labelLarge)
            FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                listOf(null to "Any", "processing" to "Processing", "failed,needs_password" to "Needs attention").forEach { (v, l) -> FilterChip(draft.status == v, { draft = draft.copy(status = v) }, label = { Text(l) }) }
            }
            Row(horizontalArrangement = Arrangement.spacedBy(12.dp), modifier = Modifier.fillMaxWidth().padding(top = 8.dp)) {
                OutlinedButton({ onApply(draft.clearedFilters()) }, Modifier.weight(1f)) { Text("Clear") }
                Button({ onApply(draft) }, Modifier.weight(1f)) { Text("Show results") }
            }
        }
    }
    picker?.let { k ->
        TaxonomyPicker(k, draft.spaceId, when (k) { "tags" -> draft.tagIds; "correspondents" -> draft.correspondentIds; else -> draft.typeIds }, multi = true, allowCreate = false, onDismiss = { picker = null }) { picked ->
            val ids = picked.map { it.id }
            draft = when (k) { "tags" -> draft.copy(tagIds = ids); "correspondents" -> draft.copy(correspondentIds = ids); else -> draft.copy(typeIds = ids) }
            picker = null
        }
    }
    datePick?.let { which ->
        val cur = if (which == "from") draft.dateFrom else draft.dateTo
        val st = rememberDatePickerState(initialSelectedDateMillis = cur?.let { runCatching { LocalDate.parse(it).atStartOfDay().toInstant(ZoneOffset.UTC).toEpochMilli() }.getOrNull() })
        DatePickerDialog(onDismissRequest = { datePick = null }, confirmButton = {
            TextButton({
                datePick = null
                st.selectedDateMillis?.let { ms -> val d = Instant.ofEpochMilli(ms).atZone(ZoneOffset.UTC).toLocalDate().toString(); draft = if (which == "from") draft.copy(dateFrom = d) else draft.copy(dateTo = d) }
            }) { Text("OK") }
        }, dismissButton = { TextButton({ datePick = null }) { Text("Cancel") } }) { DatePicker(st) }
    }
}

@Composable
fun BackButton(onBack: () -> Unit) {
    IconButton(onBack) { Icon(Icons.AutoMirrored.Outlined.ArrowBack, "Back") }
}
