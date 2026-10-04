package app.docveta.android.ui

import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
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
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.ArrowBack
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.outlined.AutoAwesome
import androidx.compose.material.icons.outlined.Clear
import androidx.compose.material.icons.outlined.DeleteForever
import androidx.compose.material.icons.outlined.DoneAll
import androidx.compose.material.icons.outlined.Inbox
import androidx.compose.material.icons.outlined.Search
import androidx.compose.material.icons.outlined.SearchOff
import androidx.compose.material.icons.outlined.Sort
import androidx.compose.material.icons.outlined.Warning
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedTextField
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
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.withStyle
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.compose.LifecycleEventEffect
import androidx.lifecycle.viewModelScope
import app.docveta.android.AppContainer
import app.docveta.android.data.AiStatus
import app.docveta.android.data.DocQuery
import app.docveta.android.data.Document
import app.docveta.android.data.Space
import app.docveta.android.data.Taxonomy
import kotlinx.coroutines.FlowPreview
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.debounce
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.launch

/** A paged list of documents for one query: the library, the Inbox or the Trash. */
class DocsViewModel(private val c: AppContainer, initial: DocQuery) : ViewModel() {
    var query by mutableStateOf(initial)
        private set
    var items by mutableStateOf<List<Document>>(emptyList())
        private set
    var total by mutableStateOf<Int?>(null)
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

    private var cursor: String? = null
    private var job: Job? = null
    private val search = MutableStateFlow(initial.q)

    init {
        @OptIn(FlowPreview::class)
        viewModelScope.launch {
            search.debounce(300).distinctUntilChanged().collect { if (it != query.q.trim()) setQuery(query.copy(q = it), debounced = true) }
        }
        reload()
    }

    /** Called on every keystroke; the list reloads once typing pauses. */
    fun typing(text: String) {
        query = query.copy(q = text)
        search.value = text.trim()
    }

    fun setQuery(q: DocQuery, debounced: Boolean = false) {
        query = if (debounced) query.copy(q = q.q) else q
        reload()
    }

    fun reload(pull: Boolean = false) {
        job?.cancel()
        cursor = null
        if (pull) refreshing = true else loading = items.isEmpty() || loading
        job = viewModelScope.launch {
            try {
                val r = c.repo.documents(query)
                items = r.items
                total = r.total
                cursor = r.nextCursor
                foundByMeaning = r.mode == "hybrid" || r.mode == "semantic"
                error = null
            } catch (e: Exception) {
                error = e.friendly()
            } finally {
                loading = false
                refreshing = false
            }
        }
    }

    fun loadMore() {
        val cur = cursor ?: return
        if (loadingMore || loading) return
        loadingMore = true
        viewModelScope.launch {
            try {
                val r = c.repo.documents(query, cur)
                items = items + r.items.filter { n -> items.none { it.id == n.id } }
                cursor = r.nextCursor
            } catch (e: Exception) {
                error = e.friendly()
            } finally {
                loadingMore = false
            }
        }
    }

    /** Removes a row at once (swipe) and asks the server; returns false if the server refused, so the caller can bring it back. */
    suspend fun review(d: Document): Boolean = try {
        items = items.filterNot { it.id == d.id }
        total = total?.minus(1)
        c.repo.markReviewed(d.id)
        true
    } catch (e: Exception) {
        error = e.friendly()
        false
    }

    suspend fun undoReview(d: Document) {
        runCatching { c.repo.markReviewed(d.id, reviewed = false) }
        reload()
    }

    fun reviewAll() {
        val ids = items.map { it.id }
        viewModelScope.launch {
            ids.forEach { runCatching { c.repo.markReviewed(it) } }
            reload()
        }
    }

    fun emptyTrash() {
        viewModelScope.launch {
            runCatching { c.repo.emptyTrash() }.onFailure { error = it.friendly() }
            reload()
        }
    }

    fun restore(d: Document) {
        viewModelScope.launch {
            runCatching { c.repo.restore(d.id) }.onFailure { error = it.friendly() }
            reload()
        }
    }
}

@Composable
fun DocumentRow(doc: Document, spaces: List<Space>, dateFormat: String, thumb: (Document) -> String, onClick: () -> Unit, modifier: Modifier = Modifier, trailing: @Composable (() -> Unit)? = null) {
    val space = spaces.firstOrNull { it.id == doc.space.id }
    Row(modifier.fillMaxWidth().clickable(onClick = onClick).padding(horizontal = 16.dp, vertical = 10.dp), verticalAlignment = Alignment.Top) {
        Thumbnail(doc, thumb(doc), Modifier.size(width = 52.dp, height = 68.dp))
        Spacer(Modifier.width(14.dp))
        Column(Modifier.weight(1f)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(doc.title, style = MaterialTheme.typography.titleSmall, maxLines = 2, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f, fill = false))
                if (doc.inbox && doc.status == "ready") {
                    Spacer(Modifier.width(6.dp))
                    Pill("New", MaterialTheme.colorScheme.primaryContainer, MaterialTheme.colorScheme.onPrimaryContainer)
                }
            }
            val sub = listOfNotNull(doc.correspondent?.name, formatDate(doc.documentDate, dateFormat).ifBlank { null }).joinToString(" · ")
            if (sub.isNotBlank()) Text(sub, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1, overflow = TextOverflow.Ellipsis)
            if (doc.snippet.isNotEmpty()) {
                Text(
                    androidx.compose.ui.text.buildAnnotatedString {
                        doc.snippet.forEach { s ->
                            if (s.hit) withStyle(androidx.compose.ui.text.SpanStyle(fontWeight = FontWeight.SemiBold, background = androidx.compose.ui.graphics.Color(0x55FFC107))) { append(s.text) } else append(s.text)
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

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun DocumentsScreen(spaceFilter: String? = null, onOpen: (String) -> Unit, onScan: () -> Unit) {
    val c = LocalContainer.current
    val me = LocalMe.current
    val vm = container("docs") { DocsViewModel(it, DocQuery(spaceId = spaceFilter)) }
    val ai by produceAi()
    var filters by remember { mutableStateOf(false) }
    var sortMenu by remember { mutableStateOf(false) }
    val listState = rememberLazyListState()
    val nearEnd by remember { derivedStateOf { val last = listState.layoutInfo.visibleItemsInfo.lastOrNull()?.index ?: 0; last >= vm.items.size - 6 } }
    LaunchedEffect(nearEnd, vm.items.size) { if (nearEnd && vm.items.isNotEmpty()) vm.loadMore() }

    val activeFilters = vm.query.tagIds.size + (if (vm.query.spaceId != null) 1 else 0) + (if (vm.query.status != null) 1 else 0) + (if (vm.query.correspondentId != null) 1 else 0) + (if (vm.query.typeId != null) 1 else 0)

    Column(Modifier.fillMaxSize()) {
        SearchBar(vm.query.q, vm::typing, "Search documents", trailing = {
            IconButton({ sortMenu = true }) { Icon(Icons.Outlined.Sort, "Sort") }
            DropdownMenu(sortMenu, { sortMenu = false }) {
                listOf(null to (if (vm.query.q.isBlank()) "Newest added" else "Best match"), "added" to "Oldest added", "-date" to "Document date (newest)", "date" to "Document date (oldest)", "title" to "Title A–Z", "-updated" to "Recently changed").forEach { (v, l) ->
                    DropdownMenuItem(text = { Text(l) }, trailingIcon = { if (vm.query.sort == v) Icon(Icons.Filled.Check, null) }, onClick = { sortMenu = false; vm.setQuery(vm.query.copy(sort = v)) })
                }
            }
        })
        LazyRow(contentPadding = PaddingValues(horizontal = 16.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            item {
                FilterChip(activeFilters > 0, { filters = true }, label = { Text(if (activeFilters > 0) "Filters · $activeFilters" else "Filters") })
            }
            if (ai.embeddings && vm.query.q.isNotBlank()) item {
                FilterChip(vm.query.semantic, { vm.setQuery(vm.query.copy(semantic = !vm.query.semantic)) }, label = { Text("By meaning") }, leadingIcon = { Icon(Icons.Outlined.AutoAwesome, null, Modifier.size(16.dp)) })
            }
            item {
                FilterChip(vm.query.status != null, { vm.setQuery(vm.query.copy(status = if (vm.query.status == null) "failed,needs_password" else null)) }, label = { Text("Needs attention") }, leadingIcon = { Icon(Icons.Outlined.Warning, null, Modifier.size(16.dp)) })
            }
            if (activeFilters > 0) item { TextButton({ vm.setQuery(DocQuery(q = vm.query.q)) }) { Text("Clear") } }
        }
        Spacer(Modifier.height(4.dp))
        StatsBanner()
        DocumentList(vm, me.spaces, me.dateFormat, listState, onOpen, empty = {
            if (vm.query.q.isNotBlank() || activeFilters > 0) EmptyState(Icons.Outlined.SearchOff, "No matching documents", "Try other words or remove a filter. Search also reads inside scans.")
            else EmptyState(Icons.Outlined.Inbox, "No documents yet", "Scan a paper document or upload a file to get started.") { androidx.compose.material3.Button(onScan) { Text("Scan a document") } }
        }, header = {
            if (vm.total != null) Text(
                (vm.total!!).toString() + " document" + (if (vm.total == 1) "" else "s") + if (vm.foundByMeaning) " · found by meaning" else "",
                style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(horizontal = 16.dp, vertical = 6.dp),
            )
        })
    }
    if (filters) FilterSheet(vm.query, me.spaces, onDismiss = { filters = false }, onApply = { vm.setQuery(it); filters = false })
}

@Composable
fun SearchBar(text: String, onChange: (String) -> Unit, hint: String, trailing: @Composable (() -> Unit)? = null) {
    Row(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp), verticalAlignment = Alignment.CenterVertically) {
        OutlinedTextField(
            text, onChange, Modifier.weight(1f), singleLine = true, placeholder = { Text(hint) },
            leadingIcon = { Icon(Icons.Outlined.Search, null) },
            trailingIcon = { if (text.isNotEmpty()) IconButton({ onChange("") }) { Icon(Icons.Outlined.Clear, "Clear search") } },
            shape = RoundedCornerShape(28.dp),
            keyboardOptions = androidx.compose.foundation.text.KeyboardOptions(imeAction = androidx.compose.ui.text.input.ImeAction.Search),
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
        Text("No text-reading worker is connected. Scans and photos wait until one is; PDFs with real text are searchable already.", style = MaterialTheme.typography.bodySmall)
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun DocumentList(vm: DocsViewModel, spaces: List<Space>, dateFormat: String, state: androidx.compose.foundation.lazy.LazyListState, onOpen: (String) -> Unit, empty: @Composable () -> Unit, header: @Composable (() -> Unit)? = null, rowContent: (@Composable (Document, @Composable () -> Unit) -> Unit)? = null) {
    val c = LocalContainer.current
    PullToRefreshBox(isRefreshing = vm.refreshing, onRefresh = { vm.reload(pull = true) }, modifier = Modifier.fillMaxSize()) {
        when {
            vm.loading -> LoadingBox()
            vm.error != null && vm.items.isEmpty() -> ErrorState(vm.error!!) { vm.reload() }
            vm.items.isEmpty() -> empty()
            else -> LazyColumn(Modifier.fillMaxSize(), state = state, contentPadding = PaddingValues(bottom = 96.dp)) {
                if (header != null) item { header() }
                items(vm.items, key = { it.id }) { d ->
                    val row = @Composable { DocumentRow(d, spaces, dateFormat, { c.repo.thumbnailUrl(it) }, { onOpen(d.id) }) }
                    if (rowContent != null) rowContent(d, row) else row()
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
    val vm = container("inbox") { DocsViewModel(it, DocQuery(inbox = true)) }
    val snack = remember { SnackbarHostState() }
    val scope = rememberCoroutineScope()
    var confirmAll by remember { mutableStateOf(false) }
    // Coming back from a document that was reviewed there: refresh.
    LifecycleEventEffect(Lifecycle.Event.ON_RESUME) { vm.reload() }

    Box(Modifier.fillMaxSize()) {
        Column(Modifier.fillMaxSize()) {
            Row(Modifier.fillMaxWidth().padding(start = 16.dp, end = 8.dp, top = 16.dp, bottom = 4.dp), verticalAlignment = Alignment.CenterVertically) {
                Column(Modifier.weight(1f)) {
                    Text("Inbox", style = MaterialTheme.typography.headlineMedium)
                    Text(if (vm.items.isEmpty()) "Nothing to review" else "Swipe right when a document looks right", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                }
                if (vm.items.size > 1) TextButton({ confirmAll = true }) { Icon(Icons.Outlined.DoneAll, null, Modifier.size(18.dp)); Spacer(Modifier.width(6.dp)); Text("All reviewed") }
            }
            StatsBanner()
            DocumentList(vm, me.spaces, me.dateFormat, rememberLazyListState(), onOpen, empty = {
                EmptyState(Icons.Outlined.DoneAll, "Inbox zero", "New documents appear here so you can check what Docveta filled in.") { androidx.compose.material3.Button(onScan) { Text("Scan a document") } }
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
    if (confirmAll) AlertDialog(
        onDismissRequest = { confirmAll = false },
        title = { Text("Mark all ${vm.items.size} as reviewed?") },
        text = { Text("They leave the Inbox but stay in your documents.") },
        confirmButton = { TextButton({ confirmAll = false; vm.reviewAll() }) { Text("Mark all reviewed") } },
        dismissButton = { TextButton({ confirmAll = false }) { Text("Cancel") } },
    )
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun TrashScreen(onBack: () -> Unit, onOpen: (String) -> Unit) {
    val me = LocalMe.current
    val vm = container("trash") { DocsViewModel(it, DocQuery(trash = true)) }
    var confirm by remember { mutableStateOf(false) }
    Column(Modifier.fillMaxSize()) {
        TopAppBar(title = { Text("Trash") }, navigationIcon = { BackButton(onBack) }, actions = {
            if (vm.items.isNotEmpty()) IconButton({ confirm = true }) { Icon(Icons.Outlined.DeleteForever, "Empty Trash") }
        })
        val days = LocalStats.current?.trashRetentionDays ?: 30
        Text("Documents here are deleted for good after $days days. Tap one to restore it.", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(horizontal = 16.dp, vertical = 4.dp))
        DocumentList(vm, me.spaces, me.dateFormat, rememberLazyListState(), onOpen, empty = { EmptyState(Icons.Outlined.DeleteForever, "Trash is empty") })
    }
    if (confirm) AlertDialog(
        onDismissRequest = { confirm = false },
        title = { Text("Empty the Trash?") }, text = { Text("These documents will be deleted forever. This can't be undone.") },
        confirmButton = { TextButton({ confirm = false; vm.emptyTrash() }) { Text("Delete forever", color = MaterialTheme.colorScheme.error) } },
        dismissButton = { TextButton({ confirm = false }) { Text("Cancel") } },
    )
}

@OptIn(ExperimentalMaterial3Api::class, ExperimentalLayoutApi::class)
@Composable
private fun FilterSheet(q: DocQuery, spaces: List<Space>, onDismiss: () -> Unit, onApply: (DocQuery) -> Unit) {
    val c = LocalContainer.current
    var draft by remember { mutableStateOf(q) }
    var tags by remember { mutableStateOf<List<Taxonomy>>(emptyList()) }
    var senders by remember { mutableStateOf<List<Taxonomy>>(emptyList()) }
    LaunchedEffect(draft.spaceId) {
        tags = runCatching { c.repo.taxonomy("tags", draft.spaceId) }.getOrDefault(emptyList()).distinctBy { it.name.lowercase() }
        senders = runCatching { c.repo.taxonomy("correspondents", draft.spaceId) }.getOrDefault(emptyList())
    }
    ModalBottomSheet(onDismissRequest = onDismiss, sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true)) {
        Column(Modifier.padding(horizontal = 20.dp).padding(bottom = 24.dp), verticalArrangement = Arrangement.spacedBy(14.dp)) {
            Text("Filters", style = MaterialTheme.typography.titleLarge)
            if (spaces.size > 1) {
                Text("Space", style = MaterialTheme.typography.labelLarge)
                LazyRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    item { FilterChip(draft.spaceId == null, { draft = draft.copy(spaceId = null, tagIds = emptyList(), correspondentId = null) }, label = { Text("All spaces") }) }
                    items(spaces) { s -> FilterChip(draft.spaceId == s.id, { draft = draft.copy(spaceId = s.id, tagIds = emptyList(), correspondentId = null) }, label = { Text(s.label) }, leadingIcon = { SpaceDot(s) }) }
                }
            }
            if (tags.isNotEmpty()) {
                Text("Tags", style = MaterialTheme.typography.labelLarge)
                FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                    tags.forEach { t ->
                        FilterChip(draft.tagIds.contains(t.id), { draft = draft.copy(tagIds = if (draft.tagIds.contains(t.id)) draft.tagIds - t.id else draft.tagIds + t.id) }, label = { Text(t.name) }, leadingIcon = { Box(Modifier.size(8.dp).background(colorFor(t.color), androidx.compose.foundation.shape.CircleShape)) })
                    }
                }
            }
            if (senders.isNotEmpty()) {
                Text("From", style = MaterialTheme.typography.labelLarge)
                LazyRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    items(senders.take(40)) { s -> FilterChip(draft.correspondentId == s.id, { draft = draft.copy(correspondentId = if (draft.correspondentId == s.id) null else s.id) }, label = { Text(s.name) }) }
                }
            }
            Row(horizontalArrangement = Arrangement.spacedBy(12.dp), modifier = Modifier.fillMaxWidth().padding(top = 8.dp)) {
                androidx.compose.material3.OutlinedButton({ onApply(draft.copy(spaceId = null, tagIds = emptyList(), correspondentId = null, typeId = null, status = null)) }, Modifier.weight(1f)) { Text("Clear") }
                androidx.compose.material3.Button({ onApply(draft) }, Modifier.weight(1f)) { Text("Show results") }
            }
        }
    }
}

@Composable
fun BackButton(onBack: () -> Unit) {
    IconButton(onBack) { Icon(Icons.AutoMirrored.Outlined.ArrowBack, "Back") }
}

