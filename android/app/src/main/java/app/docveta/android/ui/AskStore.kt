package app.docveta.android.ui

import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateListOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import app.docveta.android.data.AskEvent
import app.docveta.android.data.Citation
import app.docveta.android.data.Repository
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.launch

class AskTurn(val id: Long, val user: Boolean, text: String, citations: List<Citation> = emptyList(), pending: Boolean = false, saved: Boolean = false) {
    var text by mutableStateOf(text)
    var citations by mutableStateOf(citations)
    var pending by mutableStateOf(pending)
    var error by mutableStateOf(false)
    var stage by mutableStateOf("searching")
    var stopped by mutableStateOf(false)
    /** The server kept this answer, so asking again replaces it rather than adding to it. */
    var saved by mutableStateOf(saved)
}

/** One open chat. */
class AskSession(docId: String?) {
    var convId by mutableStateOf<String?>(null)
    var docId by mutableStateOf(docId)
    val turns = mutableStateListOf<AskTurn>()
    var busy by mutableStateOf(false)
    var loading by mutableStateOf(false)
    internal var job: Job? = null
}

/**
 * The Ask chats. It lives in the app container, not in the screen, so leaving the screen (opening a
 * cited page, switching tabs) keeps the chat, and an answer being written keeps arriving.
 */
class AskStore(private val repo: Repository, private val scope: CoroutineScope) {
    private var owner: String? = null
    private var seq = 0L
    private val live = mutableListOf<AskSession>() // shown, or still answering
    var current by mutableStateOf(AskSession(null))
        private set
    var draft by mutableStateOf("")
    /** A message for the screen to show once (then set back to null). */
    var notice by mutableStateOf<String?>(null)
    /** Bumped when conversations change on the server, so the history list reloads. */
    var historyVersion by mutableStateOf(0)
        private set

    init { live += current }

    private fun nextId() = ++seq

    /** Forgets everything when a different account signs in. */
    fun claim(account: String?) {
        if (account == owner) return
        owner = account
        live.forEach { it.job?.cancel() }
        live.clear()
        current = AskSession(null).also { live += it }
        draft = ""
    }

    private fun show(s: AskSession) {
        val prev = current
        if (prev !== s && !prev.busy) live.remove(prev)
        if (s !in live) live += s
        current = s
    }

    fun historyChanged() { historyVersion++ }

    fun isAnswering(convId: String) = live.any { it.busy && it.convId == convId }

    fun newChat(docId: String? = null) {
        val cur = current
        if (!cur.busy && !cur.loading && cur.turns.isEmpty() && cur.convId == null) { cur.docId = docId; return }
        show(AskSession(docId))
    }

    /** Opens a saved conversation; one still answering is shown as it is. */
    fun open(id: String, docId: String? = null) {
        live.firstOrNull { it.convId == id }?.let { show(it); return }
        val s = AskSession(docId).apply { convId = id; loading = true }
        show(s)
        scope.launch {
            runCatching { repo.conversation(id) }
                .onSuccess { msgs -> s.turns.addAll(msgs.map { m -> AskTurn(nextId(), m.role == "user", m.content, m.citations, saved = true) }) }
                .onFailure { e ->
                    if (e is CancellationException) throw e
                    notice = e.friendly()
                    if (current === s) newChat(null) else live.remove(s)
                }
            s.loading = false
        }
    }

    fun stop(s: AskSession = current) {
        s.job?.cancel()
    }

    /** The conversation [id] was deleted (null: all of them). */
    fun forget(id: String?) {
        live.filter { id == null || it.convId == id }.forEach { it.job?.cancel(); live.remove(it) }
        if (current !in live) current = AskSession(null).also { live += it }
        historyVersion++
    }

    /** Asks the last question again; the last answer is replaced, here and in the saved conversation. */
    fun regenerate() {
        val s = current
        if (s.busy || s.turns.size < 2 || s.turns.last().user) return
        val q = s.turns[s.turns.lastIndex - 1].text
        val replace = s.convId != null && s.turns.last().saved
        repeat(2) { s.turns.removeAt(s.turns.lastIndex) }
        ask(q, replace)
    }

    fun ask(question: String, replace: Boolean = false) {
        val q = question.trim()
        val s = current
        if (q.isEmpty() || s.busy || s.loading) return
        s.busy = true
        s.turns.add(AskTurn(nextId(), true, q))
        val answer = AskTurn(nextId(), false, "", pending = true).also { s.turns.add(it) }
        var started = ""
        s.job = scope.launch {
            try {
                repo.ask(q, s.convId, null, s.docId.takeIf { s.convId == null }, replace).collect { e ->
                    when (e) {
                        is AskEvent.Status -> { answer.stage = e.stage; if (e.conversationId.isNotBlank()) started = e.conversationId }
                        is AskEvent.Citations -> answer.citations = e.list
                        is AskEvent.Delta -> answer.text += e.text
                        is AskEvent.Done -> { if (e.conversationId.isNotBlank()) s.convId = e.conversationId; answer.saved = true }
                        is AskEvent.Failed -> {
                            if (e.gone && s.convId != null) s.convId = null // deleted elsewhere: the next question starts a new one
                            answer.text = if (answer.text.isBlank()) (if (e.gone) "This conversation was deleted. Ask again to start a new one." else e.message) else answer.text + "\n\n" + e.message
                            answer.error = true
                        }
                    }
                }
                if (!answer.saved && !answer.error) { answer.error = answer.text.isBlank(); if (answer.text.isBlank()) answer.text = "The connection closed before the answer finished. Try again." }
            } catch (e: CancellationException) {
                answer.stopped = true
                // The server keeps what was written so far.
                if (answer.text.isNotBlank()) { answer.saved = true; if (s.convId == null && started.isNotBlank()) s.convId = started }
            } finally {
                answer.pending = false
                s.busy = false
                s.job = null
                if (s !== current) live.remove(s)
                historyVersion++
            }
        }
    }
}
