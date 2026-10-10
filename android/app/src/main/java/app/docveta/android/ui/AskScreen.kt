package app.docveta.android.ui

import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import android.widget.Toast
import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.Send
import androidx.compose.material.icons.outlined.AddComment
import androidx.compose.material.icons.outlined.AutoAwesome
import androidx.compose.material.icons.outlined.Close
import androidx.compose.material.icons.outlined.ContentCopy
import androidx.compose.material.icons.outlined.DeleteOutline
import androidx.compose.material.icons.outlined.Description
import androidx.compose.material.icons.outlined.Edit
import androidx.compose.material.icons.outlined.History
import androidx.compose.material.icons.outlined.KeyboardArrowDown
import androidx.compose.material.icons.outlined.MoreVert
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material.icons.outlined.Search
import androidx.compose.material.icons.outlined.Stop
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.SmallFloatingActionButton
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.derivedStateOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateListOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.produceState
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshotFlow
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import app.docveta.android.data.Conversation
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.launch
import java.time.LocalDate
import java.time.OffsetDateTime
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.time.temporal.ChronoUnit

private val examples = listOf("When does my car insurance expire?", "How much was my last electricity bill?", "Where is the laptop warranty?")
private val docExamples = listOf("Summarise this document", "What are the important dates?", "How much has to be paid, and by when?")
private val citeRe = Regex("""\[(\d{1,2})]""")
private const val maxQuestion = 2000

/** Ask a question and get an answer written from your own documents, with every claim pointing at its page. */
@Composable
fun AskScreen(onOpenDoc: (String, Int) -> Unit, documentId: String? = null, onBack: (() -> Unit)? = null) {
    val c = LocalContainer.current
    val context = LocalContext.current
    val store = c.ask
    remember(c.session.tokenId) { store.claim(c.session.tokenId); true } // another account: start clean
    // From a document's "Ask about this document": continue a chat about it, or start one.
    LaunchedEffect(documentId) { if (documentId != null && store.current.docId != documentId) store.newChat(documentId) }
    LaunchedEffect(store.notice) { store.notice?.let { Toast.makeText(context, it, Toast.LENGTH_SHORT).show(); store.notice = null } }

    val s = store.current
    var history by remember { mutableStateOf(false) }
    var titles by remember { mutableStateOf(mapOf<String, String>()) } // conversation id → title, from the history list
    var renaming by remember { mutableStateOf<Conversation?>(null) }
    val confirm = rememberConfirmation()
    val scope = rememberCoroutineScope()
    val docTitle by produceState<String?>(null, s.docId) { value = s.docId?.let { id -> runCatching { c.repo.document(id).title }.getOrNull() } }
    val listState = rememberLazyListState()
    fun toast(msg: String) = Toast.makeText(context, msg, Toast.LENGTH_SHORT).show()

    // Follow the answer while it streams, unless the person scrolled up to read.
    val atBottom by remember { derivedStateOf { val l = listState.layoutInfo; l.visibleItemsInfo.lastOrNull()?.let { it.index >= l.totalItemsCount - 1 } ?: true } }
    LaunchedEffect(s) { if (s.turns.isNotEmpty()) listState.scrollToItem(s.turns.lastIndex, Int.MAX_VALUE / 2) }
    LaunchedEffect(s.turns.size, s.turns.lastOrNull()?.text?.length) { if (s.turns.isNotEmpty() && atBottom) listState.scrollToItem(s.turns.lastIndex, Int.MAX_VALUE / 2) }

    fun send(text: String) {
        val q = text.trim()
        if (q.isEmpty() || s.busy || s.loading) return
        store.draft = ""
        store.ask(q)
        scope.launch { if (s.turns.isNotEmpty()) listState.animateScrollToItem(s.turns.lastIndex) }
    }
    fun delete(id: String, title: String) = confirm.ask("Delete this conversation?", "\"$title\" is removed. Your documents aren't touched.", "Delete", true) {
        scope.launch { runCatching { c.repo.deleteConversation(id) }.onSuccess { store.forget(id) }.onFailure { toast(it.friendly()) } }
    }

    val convId = s.convId
    val title = when {
        convId == null -> if (documentId != null || s.docId != null) "Ask about this document" else "Ask"
        else -> titles[convId] ?: s.turns.firstOrNull { it.user }?.text ?: "Conversation"
    }
    Box(Modifier.fillMaxSize().imePadding()) {
        Column(Modifier.fillMaxSize()) {
            Row(Modifier.fillMaxWidth().padding(start = if (onBack != null) 4.dp else 16.dp, end = 4.dp, top = 8.dp, bottom = 4.dp), verticalAlignment = Alignment.CenterVertically) {
                if (onBack != null) BackButton(onBack)
                Column(Modifier.weight(1f)) {
                    Text(title, style = if (convId == null && onBack == null) MaterialTheme.typography.headlineSmall else MaterialTheme.typography.titleMedium, maxLines = 1, overflow = TextOverflow.Ellipsis)
                    if (s.docId != null) Text("About ${docTitle ?: "this document"}", style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1, overflow = TextOverflow.Ellipsis)
                }
                IconButton({ history = true }) { Icon(Icons.Outlined.History, "Conversations") }
                IconButton({ store.newChat(documentId) }) { Icon(Icons.Outlined.AddComment, "New chat") }
                if (convId != null) Box {
                    var menu by remember { mutableStateOf(false) }
                    IconButton({ menu = true }) { Icon(Icons.Outlined.MoreVert, "Conversation options") }
                    DropdownMenu(menu, { menu = false }) {
                        DropdownMenuItem(text = { Text("Rename") }, leadingIcon = { Icon(Icons.Outlined.Edit, null) }, onClick = { menu = false; renaming = Conversation(convId, title) })
                        DropdownMenuItem(text = { Text("Delete", color = MaterialTheme.colorScheme.error) }, leadingIcon = { Icon(Icons.Outlined.DeleteOutline, null, tint = MaterialTheme.colorScheme.error) }, onClick = { menu = false; delete(convId, title) })
                    }
                }
            }
            HorizontalDivider()
            Box(Modifier.weight(1f)) {
                when {
                    s.loading -> Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) { CircularProgressIndicator(Modifier.size(28.dp)) }
                    s.turns.isEmpty() -> EmptyAsk(s.docId != null, docTitle, ::send)
                    else -> LazyColumn(Modifier.fillMaxSize(), state = listState, contentPadding = PaddingValues(16.dp), verticalArrangement = Arrangement.spacedBy(18.dp)) {
                        itemsIndexed(s.turns, key = { _, t -> t.id }) { i, t ->
                            if (t.user) QuestionBubble(t.text) { copy(context, t.text); toast("Copied") }
                            else AnswerView(t, isLast = i == s.turns.lastIndex, busy = s.busy, onOpenDoc = onOpenDoc, onRetry = store::regenerate, onCopy = {
                                copy(context, t.text.replace(Regex("""\s?\[\d{1,2}]"""), "")); toast("Copied")
                            })
                        }
                    }
                }
                if (!atBottom && s.turns.isNotEmpty() && !s.loading) SmallFloatingActionButton({ scope.launch { listState.animateScrollToItem(s.turns.lastIndex, Int.MAX_VALUE / 2) } }, Modifier.align(Alignment.BottomCenter).padding(bottom = 8.dp)) {
                    Icon(Icons.Outlined.KeyboardArrowDown, "Scroll to the newest message")
                }
            }
            Composer(store, s, docTitle, onClearScope = { store.newChat(null) }, onSend = ::send)
        }
    }
    if (history) HistorySheet(store, onDismiss = { history = false }, onTitles = { titles = titles + it }, onRename = { renaming = it }, onDelete = { delete(it.id, it.title.ifBlank { "Untitled" }) }, onDeleteAll = {
        confirm.ask("Delete all conversations?", "Your questions and answers are removed. Your documents aren't touched.", "Delete all", true) {
            scope.launch { runCatching { c.repo.deleteAllConversations() }.onSuccess { store.forget(null); history = false }.onFailure { toast(it.friendly()) } }
        }
    })
    renaming?.let { cv ->
        TextInputDialog("Rename conversation", "Title", cv.title, "Rename", onDismiss = { renaming = null }) { t ->
            if (t.isNotEmpty() && t.length <= 200 && t != cv.title) scope.launch {
                runCatching { c.repo.renameConversation(cv.id, t) }.onSuccess { titles = titles + (cv.id to t); store.historyChanged() }.onFailure { toast(it.friendly()) }
            }
        }
    }
}

private fun copy(context: Context, text: String) {
    val cm = context.getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager
    cm.setPrimaryClip(ClipData.newPlainText("Docveta", text))
}

@Composable
private fun EmptyAsk(scoped: Boolean, docTitle: String?, onAsk: (String) -> Unit) {
    LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(16.dp)) {
        item {
            Column(Modifier.fillMaxWidth().padding(top = 32.dp), horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.spacedBy(10.dp)) {
                Box(Modifier.size(52.dp).clip(RoundedCornerShape(16.dp)).background(MaterialTheme.colorScheme.primaryContainer), contentAlignment = Alignment.Center) { Icon(Icons.Outlined.AutoAwesome, null, tint = MaterialTheme.colorScheme.onPrimaryContainer) }
                Text(if (scoped) "Ask this document" else "Ask your documents", style = MaterialTheme.typography.titleLarge)
                Text(
                    if (scoped) "Answers come only from ${docTitle ?: "this document"}, and show the page they were found on." else "Answers come only from your own documents, and show where they were found.",
                    style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(horizontal = 12.dp),
                )
                Spacer(Modifier.height(6.dp))
                (if (scoped) docExamples else examples).forEach { e -> Text(e, Modifier.fillMaxWidth().clip(RoundedCornerShape(12.dp)).clickable { onAsk(e) }.background(MaterialTheme.colorScheme.surfaceContainer).padding(14.dp), style = MaterialTheme.typography.bodyMedium) }
            }
        }
    }
}

@OptIn(ExperimentalFoundationApi::class)
@Composable
private fun QuestionBubble(text: String, onCopy: () -> Unit) {
    Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.End) {
        Text(
            text,
            Modifier.widthIn(max = 300.dp).clip(RoundedCornerShape(18.dp, 18.dp, 4.dp, 18.dp)).combinedClickable(onClick = {}, onLongClick = onCopy, onLongClickLabel = "Copy question")
                .background(MaterialTheme.colorScheme.primary).padding(horizontal = 14.dp, vertical = 10.dp),
            color = MaterialTheme.colorScheme.onPrimary,
        )
    }
}

@Composable
private fun Composer(store: AskStore, s: AskSession, docTitle: String?, onClearScope: () -> Unit, onSend: (String) -> Unit) {
    Column(Modifier.fillMaxWidth().padding(horizontal = 12.dp, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
        if (s.docId != null) Row(
            Modifier.clip(RoundedCornerShape(50)).background(MaterialTheme.colorScheme.surfaceContainer).padding(start = 10.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Icon(Icons.Outlined.Description, null, Modifier.size(14.dp), tint = MaterialTheme.colorScheme.onSurfaceVariant)
            Spacer(Modifier.size(6.dp))
            Text("Asking about: ${docTitle ?: "this document"}", Modifier.widthIn(max = 240.dp), style = MaterialTheme.typography.labelMedium, maxLines = 1, overflow = TextOverflow.Ellipsis)
            IconButton(onClearScope, Modifier.size(32.dp)) { Icon(Icons.Outlined.Close, "New chat about all documents", Modifier.size(16.dp)) }
        }
        Row(verticalAlignment = Alignment.Bottom) {
            OutlinedTextField(
                store.draft, { if (it.length <= maxQuestion) store.draft = it }, Modifier.weight(1f), enabled = !s.loading,
                placeholder = { Text(if (s.turns.isEmpty()) "Ask anything…" else "Ask a follow-up…") }, maxLines = 5, shape = RoundedCornerShape(24.dp),
            )
            Spacer(Modifier.size(8.dp))
            if (s.busy) {
                IconButton({ store.stop(s) }, modifier = Modifier.size(52.dp).clip(CircleShape).background(MaterialTheme.colorScheme.secondaryContainer)) {
                    Icon(Icons.Outlined.Stop, "Stop answering", tint = MaterialTheme.colorScheme.onSecondaryContainer)
                }
            } else {
                val ready = store.draft.isNotBlank() && !s.loading
                IconButton({ onSend(store.draft) }, enabled = ready, modifier = Modifier.size(52.dp).clip(CircleShape).background(if (ready) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.surfaceVariant)) {
                    Icon(Icons.AutoMirrored.Outlined.Send, "Send", tint = if (ready) MaterialTheme.colorScheme.onPrimary else MaterialTheme.colorScheme.onSurfaceVariant)
                }
            }
        }
        if (store.draft.length > maxQuestion - 200) Text("${store.draft.length} / $maxQuestion", Modifier.padding(start = 16.dp), style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
    }
}

/** "Today", "Yesterday", "Previous 7 days", … like any chat app. */
private fun dayGroup(iso: String, today: LocalDate = LocalDate.now()): String {
    val d = runCatching { OffsetDateTime.parse(iso).atZoneSameInstant(ZoneId.systemDefault()).toLocalDate() }.getOrNull() ?: return "Older"
    val days = ChronoUnit.DAYS.between(d, today)
    return when {
        days <= 0 -> "Today"
        days == 1L -> "Yesterday"
        days < 7 -> "Previous 7 days"
        days < 30 -> "Previous 30 days"
        d.year == today.year -> d.format(DateTimeFormatter.ofPattern("MMMM"))
        else -> d.format(DateTimeFormatter.ofPattern("MMMM yyyy"))
    }
}

/** Earlier conversations: searchable, grouped by day, loaded a page at a time. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun HistorySheet(store: AskStore, onDismiss: () -> Unit, onTitles: (Map<String, String>) -> Unit, onRename: (Conversation) -> Unit, onDelete: (Conversation) -> Unit, onDeleteAll: () -> Unit) {
    val c = LocalContainer.current
    var query by remember { mutableStateOf("") }
    val items = remember { mutableStateListOf<Conversation>() }
    var loading by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    var hasMore by remember { mutableStateOf(false) }
    var generation by remember { mutableStateOf(0) }
    val scope = rememberCoroutineScope()
    val pageSize = 40
    fun load(reset: Boolean) {
        if (loading && !reset) return
        val g = ++generation
        loading = true
        error = null
        val before = if (reset) null else items.lastOrNull()?.updatedAt
        scope.launch {
            runCatching { c.repo.conversations(query.trim(), before, pageSize) }
                .onSuccess { page -> if (g == generation) { if (reset) items.clear(); items.addAll(page.filter { p -> items.none { it.id == p.id } }); hasMore = page.size == pageSize; onTitles(page.associate { it.id to it.title }) } }
                .onFailure { if (g == generation) error = it.friendly() }
            if (g == generation) loading = false
        }
    }
    // Searching waits for a pause in typing; deletes and renames reload the list.
    LaunchedEffect(query, store.historyVersion) { if (query.isNotBlank()) delay(300); load(true) }
    val list = rememberLazyListState()
    LaunchedEffect(list) {
        snapshotFlow { list.layoutInfo.visibleItemsInfo.lastOrNull()?.index ?: 0 }.distinctUntilChanged().collect { last ->
            if (hasMore && !loading && last >= list.layoutInfo.totalItemsCount - 3) load(false)
        }
    }
    ModalBottomSheet(onDismiss, sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true)) {
        Column(Modifier.fillMaxHeight(0.9f)) {
            Row(Modifier.fillMaxWidth().padding(start = 16.dp, end = 8.dp), verticalAlignment = Alignment.CenterVertically) {
                Text("Conversations", Modifier.weight(1f), style = MaterialTheme.typography.titleLarge)
                TextButton({ store.newChat(null); onDismiss() }) { Icon(Icons.Outlined.AddComment, null, Modifier.size(18.dp)); Spacer(Modifier.size(6.dp)); Text("New chat") }
            }
            OutlinedTextField(query, { query = it }, Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp), placeholder = { Text("Search conversations") }, singleLine = true,
                leadingIcon = { Icon(Icons.Outlined.Search, null) }, trailingIcon = { if (query.isNotEmpty()) IconButton({ query = "" }) { Icon(Icons.Outlined.Close, "Clear search") } }, shape = RoundedCornerShape(24.dp))
            LazyColumn(Modifier.weight(1f), state = list) {
                when {
                    items.isEmpty() && loading -> item { Box(Modifier.fillMaxWidth().padding(24.dp), contentAlignment = Alignment.Center) { CircularProgressIndicator(Modifier.size(24.dp)) } }
                    items.isEmpty() && error != null -> item { ErrorState(error!!) { load(true) } }
                    items.isEmpty() -> item { Text(if (query.isNotBlank()) "No conversation has that in its title." else "Your questions and answers will appear here.", Modifier.padding(16.dp), color = MaterialTheme.colorScheme.onSurfaceVariant) }
                }
                var lastGroup = ""
                items.forEachIndexed { i, cv ->
                    val g = dayGroup(cv.updatedAt)
                    if (g != lastGroup) {
                        lastGroup = g
                        item(key = "h$i$g") { Text(g, Modifier.padding(start = 16.dp, top = 14.dp, bottom = 4.dp), style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant, fontWeight = FontWeight.Medium) }
                    }
                    item(key = cv.id) { HistoryRow(cv, active = cv.id == store.current.convId, answering = store.isAnswering(cv.id), onOpen = { store.open(cv.id, cv.documentIds.firstOrNull()); onDismiss() }, onRename = { onRename(cv) }, onDelete = { onDelete(cv) }) }
                }
                if (items.isNotEmpty() && loading) item { Box(Modifier.fillMaxWidth().padding(16.dp), contentAlignment = Alignment.Center) { CircularProgressIndicator(Modifier.size(20.dp)) } }
                if (items.isNotEmpty() && error != null) item { TextButton({ load(false) }, Modifier.padding(horizontal = 8.dp)) { Text("Couldn't load more. Try again") } }
                if (items.isNotEmpty() && query.isBlank() && !hasMore) item {
                    TextButton(onDeleteAll, Modifier.padding(horizontal = 8.dp, vertical = 8.dp)) { Text("Delete all conversations…", color = MaterialTheme.colorScheme.error) }
                }
            }
        }
    }
}

@Composable
private fun HistoryRow(cv: Conversation, active: Boolean, answering: Boolean, onOpen: () -> Unit, onRename: () -> Unit, onDelete: () -> Unit) {
    Row(
        Modifier.fillMaxWidth().padding(horizontal = 8.dp).clip(RoundedCornerShape(12.dp)).background(if (active) MaterialTheme.colorScheme.secondaryContainer else MaterialTheme.colorScheme.surface)
            .clickable(onClick = onOpen).padding(start = 12.dp, end = 0.dp, top = 2.dp, bottom = 2.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        if (cv.documentIds.isNotEmpty()) { Icon(Icons.Outlined.Description, "About one document", Modifier.size(16.dp), tint = MaterialTheme.colorScheme.onSurfaceVariant); Spacer(Modifier.size(8.dp)) }
        Text(cv.title.ifBlank { "Untitled" }, Modifier.weight(1f), maxLines = 1, overflow = TextOverflow.Ellipsis, fontWeight = if (active) FontWeight.Medium else null)
        if (answering) CircularProgressIndicator(Modifier.size(14.dp), strokeWidth = 2.dp)
        Box {
            var menu by remember { mutableStateOf(false) }
            IconButton({ menu = true }) { Icon(Icons.Outlined.MoreVert, "Options for ${cv.title}") }
            DropdownMenu(menu, { menu = false }) {
                DropdownMenuItem(text = { Text("Rename") }, leadingIcon = { Icon(Icons.Outlined.Edit, null) }, onClick = { menu = false; onRename() })
                DropdownMenuItem(text = { Text("Delete", color = MaterialTheme.colorScheme.error) }, leadingIcon = { Icon(Icons.Outlined.DeleteOutline, null, tint = MaterialTheme.colorScheme.error) }, onClick = { menu = false; onDelete() })
            }
        }
    }
}

@Composable
private fun AnswerView(t: AskTurn, isLast: Boolean, busy: Boolean, onOpenDoc: (String, Int) -> Unit, onRetry: () -> Unit, onCopy: () -> Unit) {
    var allSources by remember { mutableStateOf(false) }
    Row {
        Box(Modifier.padding(top = 2.dp).size(28.dp).clip(CircleShape).background(MaterialTheme.colorScheme.primaryContainer), contentAlignment = Alignment.Center) {
            Icon(Icons.Outlined.AutoAwesome, null, Modifier.size(16.dp), tint = MaterialTheme.colorScheme.onPrimaryContainer)
        }
        Spacer(Modifier.size(10.dp))
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            if (t.pending && t.text.isEmpty()) Row(Modifier.padding(top = 4.dp), verticalAlignment = Alignment.CenterVertically) {
                CircularProgressIndicator(Modifier.size(16.dp), strokeWidth = 2.dp)
                Spacer(Modifier.size(8.dp))
                Text(if (t.stage == "answering") "Writing the answer…" else "Searching your documents…", style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
            else if (t.error && !t.text.contains("\n\n")) Text(t.text, Modifier.fillMaxWidth().clip(RoundedCornerShape(10.dp)).background(MaterialTheme.colorScheme.errorContainer).padding(12.dp), color = MaterialTheme.colorScheme.onErrorContainer, style = MaterialTheme.typography.bodyMedium)
            else MarkdownText(t.text, color = if (t.error) MaterialTheme.colorScheme.error else MaterialTheme.colorScheme.onSurface) { n ->
                t.citations.firstOrNull { it.n == n }?.let { onOpenDoc(it.documentId, it.page) }
            }
            if (t.stopped) Text("Stopped.", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            if (!t.pending) Row {
                if (t.text.isNotBlank() && !t.error) TextButton(onCopy) { Icon(Icons.Outlined.ContentCopy, null, Modifier.size(16.dp)); Spacer(Modifier.size(6.dp)); Text("Copy") }
                if (isLast && !busy) TextButton(onRetry) { Icon(Icons.Outlined.Refresh, null, Modifier.size(16.dp)); Spacer(Modifier.size(6.dp)); Text(if (t.error || t.stopped) "Try again" else "Regenerate") }
            }
            // Cited sources first; the others were searched but not used.
            val cited = citeRe.findAll(t.text).map { it.groupValues[1].toInt() }.toSet()
            val used = t.citations.filter { it.n in cited }
            val other = t.citations.filter { it.n !in cited }
            val shown = if (t.pending || used.isEmpty()) t.citations else if (allSources) used + other else used
            if (shown.isNotEmpty()) {
                Text(if (t.pending) "Looking at" else "Sources", style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
                shown.forEach { ci ->
                    Row(Modifier.fillMaxWidth().clip(RoundedCornerShape(12.dp)).clickable { onOpenDoc(ci.documentId, ci.page) }.background(MaterialTheme.colorScheme.surfaceContainer).padding(10.dp), verticalAlignment = Alignment.Top) {
                        Text("${ci.n}", Modifier.clip(RoundedCornerShape(6.dp)).background(MaterialTheme.colorScheme.primaryContainer).padding(horizontal = 7.dp, vertical = 2.dp), style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onPrimaryContainer)
                        Spacer(Modifier.size(10.dp))
                        Column {
                            Text("${ci.title} · p.${ci.page}", style = MaterialTheme.typography.labelLarge, maxLines = 1, overflow = TextOverflow.Ellipsis)
                            if (ci.snippet.isNotBlank()) Text(ci.snippet, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 2, overflow = TextOverflow.Ellipsis)
                        }
                    }
                }
                if (!t.pending && used.isNotEmpty() && other.isNotEmpty()) TextButton({ allSources = !allSources }) {
                    Text(if (allSources) "Show only cited sources" else "Also searched ${other.size} more passage${if (other.size == 1) "" else "s"}")
                }
            }
        }
    }
}
