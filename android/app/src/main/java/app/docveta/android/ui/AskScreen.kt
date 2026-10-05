package app.docveta.android.ui

import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import android.widget.Toast
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
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
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.Send
import androidx.compose.material.icons.outlined.AddComment
import androidx.compose.material.icons.outlined.AutoAwesome
import androidx.compose.material.icons.outlined.ContentCopy
import androidx.compose.material.icons.outlined.DeleteOutline
import androidx.compose.material.icons.outlined.Edit
import androidx.compose.material.icons.outlined.History
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material.icons.outlined.Stop
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.derivedStateOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateListOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import app.docveta.android.data.AskEvent
import app.docveta.android.data.Citation
import app.docveta.android.data.Conversation
import kotlinx.coroutines.Job
import kotlinx.coroutines.launch

private class Turn(val user: Boolean, text: String, citations: List<Citation> = emptyList(), pending: Boolean = false, error: Boolean = false) {
    var text by mutableStateOf(text)
    var citations by mutableStateOf(citations)
    var pending by mutableStateOf(pending)
    var error by mutableStateOf(error)
    var stage by mutableStateOf("searching")
    var stopped by mutableStateOf(false)
}

private val examples = listOf("When does my car insurance expire?", "How much was my last electricity bill?", "Where is the laptop warranty?")
private val citeRe = Regex("""\[(\d{1,2})]""")

/** Ask a question and get an answer written from your own documents, with every claim pointing at its page. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun AskScreen(onOpenDoc: (String, Int) -> Unit, documentId: String? = null, onBack: (() -> Unit)? = null) {
    val c = LocalContainer.current
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    val turns = remember { mutableStateListOf<Turn>() }
    var convId by remember { mutableStateOf<String?>(null) }
    var text by remember { mutableStateOf("") }
    var busy by remember { mutableStateOf(false) }
    var history by remember { mutableStateOf(false) }
    var conversations by remember { mutableStateOf<List<Conversation>>(emptyList()) }
    var historyLoading by remember { mutableStateOf(false) }
    var renaming by remember { mutableStateOf<Conversation?>(null) }
    var job by remember { mutableStateOf<Job?>(null) }
    val listState = rememberLazyListState()
    // Follow the answer while it streams, unless the person scrolled up to read.
    val atBottom by remember { derivedStateOf { val l = listState.layoutInfo; l.visibleItemsInfo.lastOrNull()?.index?.let { it >= l.totalItemsCount - 1 } ?: true } }
    LaunchedEffect(turns.size, turns.lastOrNull()?.text?.length) { if (turns.isNotEmpty() && atBottom) listState.scrollToItem(turns.lastIndex, Int.MAX_VALUE / 2) }

    fun toast(msg: String) = Toast.makeText(context, msg, Toast.LENGTH_SHORT).show()

    fun ask(q: String) {
        val question = q.trim()
        if (question.isEmpty() || busy) return
        text = ""
        busy = true
        turns.add(Turn(true, question))
        val answer = Turn(false, "", pending = true).also { turns.add(it) }
        job = scope.launch {
            try {
                c.repo.ask(question, convId, null, documentId).collect { e ->
                    when (e) {
                        is AskEvent.Status -> answer.stage = e.stage
                        is AskEvent.Citations -> answer.citations = e.list
                        is AskEvent.Delta -> answer.text += e.text
                        is AskEvent.Done -> if (e.conversationId.isNotBlank()) convId = e.conversationId
                        is AskEvent.Failed -> { answer.text = if (answer.text.isBlank()) e.message else answer.text + "\n\n" + e.message; answer.error = true }
                    }
                }
            } finally {
                answer.pending = false
                busy = false
            }
        }
    }
    fun stop() {
        job?.cancel()
        turns.lastOrNull()?.takeIf { !it.user }?.let { it.stopped = true; it.pending = false }
        busy = false
    }
    fun retry() {
        val q = turns.lastOrNull { it.user }?.text ?: return
        repeat(2) { if (turns.isNotEmpty()) turns.removeAt(turns.lastIndex) }
        ask(q)
    }
    fun loadHistory() = scope.launch {
        historyLoading = true
        runCatching { c.repo.conversations() }.onSuccess { conversations = it }.onFailure { toast(it.friendly()) }
        historyLoading = false
    }

    Column(Modifier.fillMaxSize().imePadding()) {
        Row(Modifier.fillMaxWidth().padding(start = if (onBack != null) 4.dp else 16.dp, end = 4.dp, top = 12.dp), verticalAlignment = Alignment.CenterVertically) {
            if (onBack != null) BackButton(onBack)
            Text(if (documentId != null) "Ask about this document" else "Ask", Modifier.weight(1f), style = if (documentId != null) MaterialTheme.typography.titleLarge else MaterialTheme.typography.headlineMedium)
            IconButton({ stop(); turns.clear(); convId = null }) { Icon(Icons.Outlined.AddComment, "New conversation") }
            IconButton({ history = true; loadHistory() }) { Icon(Icons.Outlined.History, "Earlier conversations") }
        }
        LazyColumn(Modifier.weight(1f), state = listState, contentPadding = androidx.compose.foundation.layout.PaddingValues(16.dp), verticalArrangement = Arrangement.spacedBy(16.dp)) {
            if (turns.isEmpty()) item {
                Column(Modifier.fillMaxWidth().padding(top = 32.dp), horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.spacedBy(10.dp)) {
                    Box(Modifier.size(52.dp).clip(RoundedCornerShape(16.dp)).background(MaterialTheme.colorScheme.primaryContainer), contentAlignment = Alignment.Center) { Icon(Icons.Outlined.AutoAwesome, null, tint = MaterialTheme.colorScheme.onPrimaryContainer) }
                    Text(if (documentId != null) "Ask this document" else "Ask your documents", style = MaterialTheme.typography.titleLarge)
                    Text(if (documentId != null) "Answers come only from this document, and show the page they were found on." else "Answers come only from your own documents, and show where they were found.", style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(horizontal = 12.dp))
                    Spacer(Modifier.height(6.dp))
                    (if (documentId != null) listOf("Summarise this document", "What are the important dates?", "How much has to be paid, and by when?") else examples).forEach { e -> Text(e, Modifier.fillMaxWidth().clip(RoundedCornerShape(12.dp)).clickable { ask(e) }.background(MaterialTheme.colorScheme.surfaceContainer).padding(14.dp), style = MaterialTheme.typography.bodyMedium) }
                }
            }
            itemsIndexed(turns) { i, t ->
                if (t.user) Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.End) {
                    Text(t.text, Modifier.widthIn(max = 300.dp).clip(RoundedCornerShape(18.dp, 18.dp, 4.dp, 18.dp)).background(MaterialTheme.colorScheme.primary).padding(horizontal = 14.dp, vertical = 10.dp), color = MaterialTheme.colorScheme.onPrimary)
                } else AnswerView(t, isLast = i == turns.lastIndex, onOpenDoc = onOpenDoc, onRetry = ::retry, onCopy = {
                    val cm = context.getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager
                    cm.setPrimaryClip(ClipData.newPlainText("Answer", t.text.replace(Regex("""\s?\[\d{1,2}]"""), "")))
                    toast("Copied")
                })
            }
        }
        Row(Modifier.fillMaxWidth().padding(12.dp), verticalAlignment = Alignment.Bottom) {
            OutlinedTextField(text, { if (it.length <= 2000) text = it }, Modifier.weight(1f), placeholder = { Text("Ask anything…") }, maxLines = 5, shape = RoundedCornerShape(24.dp))
            Spacer(Modifier.size(8.dp))
            if (busy) {
                IconButton(::stop, modifier = Modifier.size(52.dp).clip(RoundedCornerShape(26.dp)).background(MaterialTheme.colorScheme.secondaryContainer)) {
                    Icon(Icons.Outlined.Stop, "Stop answering", tint = MaterialTheme.colorScheme.onSecondaryContainer)
                }
            } else {
                val ready = text.isNotBlank()
                IconButton({ ask(text) }, enabled = ready, modifier = Modifier.size(52.dp).clip(RoundedCornerShape(26.dp)).background(if (ready) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.surfaceVariant)) {
                    Icon(Icons.AutoMirrored.Outlined.Send, "Ask", tint = if (ready) MaterialTheme.colorScheme.onPrimary else MaterialTheme.colorScheme.onSurfaceVariant)
                }
            }
        }
    }
    if (history) ModalBottomSheet({ history = false }) {
        Column(Modifier.padding(bottom = 24.dp)) {
            Text("Earlier conversations", style = MaterialTheme.typography.titleLarge, modifier = Modifier.padding(16.dp))
            when {
                historyLoading && conversations.isEmpty() -> Box(Modifier.fillMaxWidth().padding(24.dp), contentAlignment = Alignment.Center) { CircularProgressIndicator(Modifier.size(24.dp)) }
                conversations.isEmpty() -> Text("Your questions and answers will appear here.", Modifier.padding(16.dp), color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
            conversations.forEach { cv ->
                Row(Modifier.fillMaxWidth().clickable {
                    history = false
                    scope.launch {
                        stop()
                        turns.clear()
                        convId = cv.id
                        runCatching { c.repo.conversation(cv.id) }
                            .onSuccess { msgs -> msgs.forEach { m -> turns.add(Turn(m.role == "user", m.content, m.citations)) } }
                            .onFailure { convId = null; toast(it.friendly()) }
                    }
                }.padding(start = 16.dp, end = 4.dp, top = 6.dp, bottom = 6.dp), verticalAlignment = Alignment.CenterVertically) {
                    Text(cv.title.ifBlank { "Untitled" }, Modifier.weight(1f), maxLines = 2, overflow = TextOverflow.Ellipsis)
                    IconButton({ renaming = cv }) { Icon(Icons.Outlined.Edit, "Rename") }
                    IconButton({
                        scope.launch {
                            runCatching { c.repo.deleteConversation(cv.id) }
                                .onSuccess { conversations = conversations - cv; if (convId == cv.id) { turns.clear(); convId = null } }
                                .onFailure { toast(it.friendly()) }
                        }
                    }) { Icon(Icons.Outlined.DeleteOutline, "Delete") }
                }
            }
        }
    }
    renaming?.let { cv ->
        var title by remember(cv.id) { mutableStateOf(cv.title) }
        AlertDialog(
            onDismissRequest = { renaming = null },
            title = { Text("Rename conversation") },
            text = { OutlinedTextField(title, { if (it.length <= 200) title = it }, singleLine = true) },
            confirmButton = {
                TextButton({
                    val t = title.trim()
                    renaming = null
                    if (t.isNotEmpty()) scope.launch {
                        runCatching { c.repo.renameConversation(cv.id, t) }.onSuccess { loadHistory() }.onFailure { toast(it.friendly()) }
                    }
                }) { Text("Rename") }
            },
            dismissButton = { TextButton({ renaming = null }) { Text("Cancel") } },
        )
    }
}

@Composable
private fun AnswerView(t: Turn, isLast: Boolean, onOpenDoc: (String, Int) -> Unit, onRetry: () -> Unit, onCopy: () -> Unit) {
    var allSources by remember { mutableStateOf(false) }
    Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
        if (t.pending && t.text.isEmpty()) Row(verticalAlignment = Alignment.CenterVertically) {
            CircularProgressIndicator(Modifier.size(16.dp), strokeWidth = 2.dp)
            Spacer(Modifier.size(8.dp))
            Text(if (t.stage == "answering") "Writing the answer…" else "Searching your documents…", style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
        else MarkdownText(t.text, color = if (t.error) MaterialTheme.colorScheme.error else MaterialTheme.colorScheme.onSurface) { n ->
            t.citations.firstOrNull { it.n == n }?.let { onOpenDoc(it.documentId, it.page) }
        }
        if (t.stopped) Text("Stopped.", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        if (!t.pending) Row {
            if (t.text.isNotBlank() && !t.error) TextButton(onCopy) { Icon(Icons.Outlined.ContentCopy, null, Modifier.size(16.dp)); Spacer(Modifier.size(6.dp)); Text("Copy") }
            if (isLast && (t.error || t.stopped)) TextButton(onRetry) { Icon(Icons.Outlined.Refresh, null, Modifier.size(16.dp)); Spacer(Modifier.size(6.dp)); Text("Try again") }
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
