package app.docveta.android.ui

import android.graphics.RectF
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Close
import androidx.compose.material.icons.outlined.KeyboardArrowDown
import androidx.compose.material.icons.outlined.KeyboardArrowUp
import androidx.compose.material.icons.outlined.Search
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextField
import androidx.compose.material3.TextFieldDefaults
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.unit.dp

/** One match: its page (from 0) and where it is on the page, in points from the top left (empty when only the page is known). */
data class PdfMatch(val page: Int, val rects: List<RectF>)

/** Find in document: what's typed, what was found, and which match is shown. */
class PdfFindState {
    var open by mutableStateOf(false)
    var query by mutableStateOf("")
    var matches by mutableStateOf<List<PdfMatch>>(emptyList())
    var index by mutableIntStateOf(0)
    var searching by mutableStateOf(false)
    var searched by mutableStateOf(false)
    var noText by mutableStateOf(false)

    /** Changes on every move, so the viewer scrolls even to the same match again. */
    var seq by mutableIntStateOf(0)

    val current: PdfMatch? get() = matches.getOrNull(index)

    fun move(d: Int) {
        if (matches.isEmpty()) return
        index = (index + d + matches.size) % matches.size
        seq++
    }

    fun show(results: List<PdfMatch>, anyText: Boolean) {
        matches = results
        index = 0
        noText = !anyText
        searched = true
        seq++
    }

    fun close() {
        open = false
        matches = emptyList()
        searched = false
    }
}

/**
 * Finds text in pages without boxes (the server's text of each page): ignores case and spaces,
 * like the web app, so "amount due" also finds "Amountdue".
 */
fun findInPageTexts(texts: List<Pair<Int, String>>, query: String): List<PdfMatch> {
    val q = query.lowercase().filterNot { it.isWhitespace() }
    if (q.isEmpty()) return emptyList()
    val out = ArrayList<PdfMatch>()
    for ((page, text) in texts) {
        val t = text.lowercase().filterNot { it.isWhitespace() }
        var i = t.indexOf(q)
        while (i >= 0) {
            out.add(PdfMatch(page, emptyList()))
            i = t.indexOf(q, i + q.length)
        }
    }
    return out
}

@Composable
fun FindBar(state: PdfFindState, modifier: Modifier = Modifier) {
    val focus = remember { FocusRequester() }
    LaunchedEffect(Unit) { runCatching { focus.requestFocus() } }
    val n = state.matches.size
    val label = when {
        state.searching -> "Searching…"
        !state.searched || state.query.isBlank() -> ""
        state.noText -> "No text yet"
        n == 0 -> "No matches"
        else -> "${state.index + 1} of $n"
    }
    Surface(modifier.fillMaxWidth().padding(8.dp), shape = MaterialTheme.shapes.extraLarge, tonalElevation = 3.dp, shadowElevation = 4.dp) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            TextField(
                state.query, { state.query = it },
                Modifier.weight(1f).focusRequester(focus),
                placeholder = { Text("Find in document") },
                leadingIcon = { Icon(Icons.Outlined.Search, null) },
                singleLine = true,
                keyboardOptions = KeyboardOptions(imeAction = ImeAction.Search),
                keyboardActions = KeyboardActions(onSearch = { state.move(1) }),
                colors = TextFieldDefaults.colors(
                    focusedContainerColor = Color.Transparent, unfocusedContainerColor = Color.Transparent,
                    focusedIndicatorColor = Color.Transparent, unfocusedIndicatorColor = Color.Transparent,
                ),
            )
            Text(label, style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
            IconButton({ state.move(-1) }, enabled = n > 0) { Icon(Icons.Outlined.KeyboardArrowUp, "Previous match") }
            IconButton({ state.move(1) }, enabled = n > 0) { Icon(Icons.Outlined.KeyboardArrowDown, "Next match") }
            IconButton(state::close) { Icon(Icons.Outlined.Close, "Close find") }
        }
    }
}
