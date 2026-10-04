package app.docveta.android.ui

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
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.Send
import androidx.compose.material.icons.outlined.AddComment
import androidx.compose.material.icons.outlined.AutoAwesome
import androidx.compose.material.icons.outlined.DeleteOutline
import androidx.compose.material.icons.outlined.History
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateListOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.withStyle
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
}

private val examples = listOf("When does my car insurance expire?", "How much was my last electricity bill?", "Where is the laptop warranty?")

/** Ask a question and get an answer written from your own documents, with every claim pointing at its page. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun AskScreen(onOpenDoc: (String, Int) -> Unit) {
    val c = LocalContainer.current
    val me = LocalMe.current
    val scope = rememberCoroutineScope()
    val turns = remember { mutableStateListOf<Turn>() }
    var convId by remember { mutableStateOf<String?>(null) }
    var text by remember { mutableStateOf("") }
    var busy by remember { mutableStateOf(false) }
    var history by remember { mutableStateOf(false) }
    var conversations by remember { mutableStateOf<List<Conversation>>(emptyList()) }
    var job by remember { mutableStateOf<Job?>(null) }
    val listState = rememberLazyListState()
    LaunchedEffect(turns.size, turns.lastOrNull()?.text) { if (turns.isNotEmpty()) listState.animateScrollToItem(turns.lastIndex) }

    fun ask(q: String) {
        val question = q.trim()
        if (question.isEmpty() || busy) return
        text = ""
        busy = true
        turns.add(Turn(true, question))
        val answer = Turn(false, "", pending = true).also { turns.add(it) }
        job = scope.launch {
            c.repo.ask(question, convId, null).collect { e ->
                when (e) {
                    is AskEvent.Citations -> answer.citations = e.list
                    is AskEvent.Delta -> answer.text += e.text
                    is AskEvent.Done -> if (e.conversationId.isNotBlank()) convId = e.conversationId
                    is AskEvent.Failed -> { answer.text = e.message; answer.error = true }
                }
            }
            answer.pending = false
            busy = false
        }
    }

    Column(Modifier.fillMaxSize().imePadding()) {
        Row(Modifier.fillMaxWidth().padding(start = 16.dp, end = 4.dp, top = 12.dp), verticalAlignment = Alignment.CenterVertically) {
            Text("Ask", Modifier.weight(1f), style = MaterialTheme.typography.headlineMedium)
            IconButton({ job?.cancel(); turns.clear(); convId = null; busy = false }) { Icon(Icons.Outlined.AddComment, "New question") }
            IconButton({ scope.launch { conversations = runCatching { c.repo.conversations() }.getOrDefault(emptyList()); history = true } }) { Icon(Icons.Outlined.History, "Earlier questions") }
        }
        LazyColumn(Modifier.weight(1f), state = listState, contentPadding = androidx.compose.foundation.layout.PaddingValues(16.dp), verticalArrangement = Arrangement.spacedBy(16.dp)) {
            if (turns.isEmpty()) item {
                Column(Modifier.fillMaxWidth().padding(top = 32.dp), horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.spacedBy(10.dp)) {
                    Box(Modifier.size(52.dp).clip(RoundedCornerShape(16.dp)).background(MaterialTheme.colorScheme.primaryContainer), contentAlignment = Alignment.Center) { Icon(Icons.Outlined.AutoAwesome, null, tint = MaterialTheme.colorScheme.onPrimaryContainer) }
                    Text("Ask your documents", style = MaterialTheme.typography.titleLarge)
                    Text("Answers come only from your own documents, and show where they were found.", style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(horizontal = 12.dp))
                    Spacer(Modifier.height(6.dp))
                    examples.forEach { e -> Text(e, Modifier.fillMaxWidth().clip(RoundedCornerShape(12.dp)).clickable { ask(e) }.background(MaterialTheme.colorScheme.surfaceContainer).padding(14.dp), style = MaterialTheme.typography.bodyMedium) }
                }
            }
            items(turns) { t ->
                if (t.user) Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.End) {
                    Text(t.text, Modifier.widthIn(max = 300.dp).clip(RoundedCornerShape(18.dp, 18.dp, 4.dp, 18.dp)).background(MaterialTheme.colorScheme.primary).padding(horizontal = 14.dp, vertical = 10.dp), color = MaterialTheme.colorScheme.onPrimary)
                } else Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
                    if (t.pending && t.text.isEmpty()) Row(verticalAlignment = Alignment.CenterVertically) { CircularProgressIndicator(Modifier.size(16.dp), strokeWidth = 2.dp); Spacer(Modifier.size(8.dp)); Text("Reading your documents…", style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant) }
                    else Text(withCitationMarks(t.text), color = if (t.error) MaterialTheme.colorScheme.error else MaterialTheme.colorScheme.onSurface, style = MaterialTheme.typography.bodyLarge)
                    if (t.citations.isNotEmpty()) {
                        Text("Sources", style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
                        t.citations.forEach { ci ->
                            Row(Modifier.fillMaxWidth().clip(RoundedCornerShape(12.dp)).clickable { onOpenDoc(ci.documentId, ci.page) }.background(MaterialTheme.colorScheme.surfaceContainer).padding(10.dp), verticalAlignment = Alignment.Top) {
                                Text("${ci.n}", Modifier.clip(RoundedCornerShape(6.dp)).background(MaterialTheme.colorScheme.primaryContainer).padding(horizontal = 7.dp, vertical = 2.dp), style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onPrimaryContainer)
                                Spacer(Modifier.size(10.dp))
                                Column {
                                    Text("${ci.title} · p.${ci.page}", style = MaterialTheme.typography.labelLarge, maxLines = 1, overflow = TextOverflow.Ellipsis)
                                    if (ci.snippet.isNotBlank()) Text(ci.snippet, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 2, overflow = TextOverflow.Ellipsis)
                                }
                            }
                        }
                    }
                }
            }
        }
        Row(Modifier.fillMaxWidth().padding(12.dp), verticalAlignment = Alignment.Bottom) {
            OutlinedTextField(text, { text = it }, Modifier.weight(1f), placeholder = { Text("Ask anything…") }, maxLines = 4, shape = RoundedCornerShape(24.dp))
            Spacer(Modifier.size(8.dp))
            IconButton({ ask(text) }, enabled = text.isNotBlank() && !busy, modifier = Modifier.size(52.dp).clip(RoundedCornerShape(26.dp)).background(if (text.isNotBlank() && !busy) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.surfaceVariant)) {
                Icon(Icons.AutoMirrored.Outlined.Send, "Ask", tint = if (text.isNotBlank() && !busy) MaterialTheme.colorScheme.onPrimary else MaterialTheme.colorScheme.onSurfaceVariant)
            }
        }
    }
    if (history) ModalBottomSheet({ history = false }) {
        Column(Modifier.padding(bottom = 24.dp)) {
            Text("Earlier questions", style = MaterialTheme.typography.titleLarge, modifier = Modifier.padding(16.dp))
            if (conversations.isEmpty()) Text("Nothing yet.", Modifier.padding(16.dp), color = MaterialTheme.colorScheme.onSurfaceVariant)
            conversations.forEach { cv ->
                Row(Modifier.fillMaxWidth().clickable {
                    history = false
                    scope.launch {
                        job?.cancel()
                        busy = false
                        turns.clear()
                        convId = cv.id
                        runCatching { c.repo.conversation(cv.id) }.getOrNull()?.forEach { m -> turns.add(Turn(m.role == "user", m.content, m.citations)) }
                    }
                }.padding(horizontal = 16.dp, vertical = 12.dp), verticalAlignment = Alignment.CenterVertically) {
                    Text(cv.title.ifBlank { "Untitled" }, Modifier.weight(1f), maxLines = 1, overflow = TextOverflow.Ellipsis)
                    IconButton({ scope.launch { runCatching { c.repo.deleteConversation(cv.id) }; conversations = conversations - cv; if (convId == cv.id) { turns.clear(); convId = null } } }) { Icon(Icons.Outlined.DeleteOutline, "Delete") }
                }
            }
        }
    }
}

/** "[2]" in an answer becomes a small bold marker matching the Sources list. */
private fun withCitationMarks(text: String): AnnotatedString = buildAnnotatedString {
    var last = 0
    for (m in Regex("\\[(\\d{1,2})]").findAll(text)) {
        append(text.substring(last, m.range.first))
        withStyle(SpanStyle(fontWeight = FontWeight.Bold, color = androidx.compose.ui.graphics.Color(0xFF4F6BED))) { append("[${m.groupValues[1]}]") }
        last = m.range.last + 1
    }
    append(text.substring(last))
}
