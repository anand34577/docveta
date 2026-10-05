package app.docveta.android.ui

import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import android.content.Intent
import android.widget.Toast
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.KeyboardArrowRight
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.outlined.ContentCopy
import androidx.compose.material.icons.outlined.Share
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.ExposedDropdownMenuBox
import androidx.compose.material3.ExposedDropdownMenuDefaults
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.MenuAnchorType
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.Stable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import app.docveta.android.data.ApiException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.launch

/** The tag and space colours the server knows, in the web app's order. */
val colorNames = listOf("slate", "red", "orange", "amber", "lime", "green", "teal", "cyan", "blue", "indigo", "violet", "pink")

fun toast(ctx: Context, msg: String) = Toast.makeText(ctx, msg, Toast.LENGTH_SHORT).show()

fun copyText(ctx: Context, label: String, text: String) {
    (ctx.getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager).setPrimaryClip(ClipData.newPlainText(label, text))
    toast(ctx, "Copied")
}

fun shareText(ctx: Context, text: String, title: String = "Share") {
    ctx.startActivity(Intent.createChooser(Intent(Intent.ACTION_SEND).apply { type = "text/plain"; putExtra(Intent.EXTRA_TEXT, text) }, title))
}

/** Runs server calls from a screen: one at a time, errors and an optional success message as a toast. */
@Stable
class Runner(private val scope: CoroutineScope, private val ctx: Context) {
    var busy by mutableStateOf(false)
        private set

    fun run(success: String? = null, onError: ((ApiException) -> Boolean)? = null, block: suspend () -> Unit) {
        if (busy) return
        busy = true
        scope.launch {
            try {
                block()
                if (success != null) toast(ctx, success)
            } catch (e: ApiException) {
                if (onError?.invoke(e) != true) toast(ctx, e.message)
            } catch (e: Exception) {
                if (e is kotlinx.coroutines.CancellationException) throw e
                toast(ctx, e.friendly())
            } finally {
                busy = false
            }
        }
    }
}

@Composable
fun rememberRunner(): Runner {
    val scope = rememberCoroutineScope()
    val ctx = LocalContext.current
    return remember { Runner(scope, ctx) }
}

/** Something loaded from the server for a screen, with reload. */
@Stable
class Loader<T>(private val scope: CoroutineScope, private val fetch: suspend () -> T) {
    var data by mutableStateOf<T?>(null)
    var error by mutableStateOf<String?>(null)
        private set
    var loading by mutableStateOf(true)
        private set
    private var generation = 0

    fun reload() {
        val g = ++generation
        loading = true
        scope.launch {
            try {
                val v = fetch()
                if (g == generation) { data = v; error = null }
            } catch (e: Exception) {
                if (e is kotlinx.coroutines.CancellationException) throw e
                if (g == generation) error = e.friendly()
            } finally {
                if (g == generation) loading = false
            }
        }
    }
}

@Composable
fun <T> rememberLoader(vararg keys: Any?, fetch: suspend () -> T): Loader<T> {
    val scope = rememberCoroutineScope()
    val l = remember(*keys) { Loader(scope, fetch) }
    LaunchedEffect(l) { l.reload() }
    return l
}

/** Shows a spinner, an error with Try again, or the content. */
@Composable
fun <T> Loaded(l: Loader<T>, content: @Composable (T) -> Unit) {
    val d = l.data
    when {
        d != null -> content(d)
        l.error != null -> ErrorState(l.error!!) { l.reload() }
        else -> LoadingBox()
    }
}

/** A screen with a top bar and a scrolling column of sections. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun Page(title: String, onBack: () -> Unit, subtitle: String? = null, actions: @Composable () -> Unit = {}, scroll: Boolean = true, content: @Composable ColumnScope.() -> Unit) {
    Column(Modifier.fillMaxSize().navigationBarsPadding()) {
        TopAppBar(
            title = {
                Column {
                    Text(title, maxLines = 1, overflow = TextOverflow.Ellipsis)
                    if (subtitle != null) Text(subtitle, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1, overflow = TextOverflow.Ellipsis)
                }
            },
            navigationIcon = { BackButton(onBack) }, actions = { actions() },
        )
        if (scroll) Column(Modifier.fillMaxSize().imePadding().verticalScroll(rememberScrollState()).padding(bottom = 32.dp)) { content() }
        else Column(Modifier.fillMaxSize().imePadding()) { content() }
    }
}

/** A row that opens something. */
@Composable
fun NavRow(title: String, subtitle: String? = null, icon: ImageVector? = null, trailing: (@Composable () -> Unit)? = null, onClick: () -> Unit) {
    Row(Modifier.fillMaxWidth().clickable(onClick = onClick).padding(horizontal = 16.dp, vertical = 13.dp), verticalAlignment = Alignment.CenterVertically) {
        if (icon != null) {
            Icon(icon, null, tint = MaterialTheme.colorScheme.onSurfaceVariant)
            Spacer(Modifier.width(16.dp))
        }
        Column(Modifier.weight(1f)) {
            Text(title, style = MaterialTheme.typography.bodyLarge)
            if (!subtitle.isNullOrBlank()) Text(subtitle, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
        if (trailing != null) trailing() else Icon(Icons.AutoMirrored.Outlined.KeyboardArrowRight, null, tint = MaterialTheme.colorScheme.outline)
    }
}

@Composable
fun ToggleRow(title: String, subtitle: String?, checked: Boolean, enabled: Boolean = true, onChange: (Boolean) -> Unit) {
    Row(Modifier.fillMaxWidth().clickable(enabled = enabled) { onChange(!checked) }.padding(horizontal = 16.dp, vertical = 10.dp), verticalAlignment = Alignment.CenterVertically) {
        Column(Modifier.weight(1f)) {
            Text(title, style = MaterialTheme.typography.bodyLarge, color = if (enabled) MaterialTheme.colorScheme.onSurface else MaterialTheme.colorScheme.onSurfaceVariant)
            if (!subtitle.isNullOrBlank()) Text(subtitle, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
        Spacer(Modifier.width(12.dp))
        Switch(checked, onChange, enabled = enabled)
    }
}

/** A short explanation under a section label. */
@Composable
fun Hint(text: String, modifier: Modifier = Modifier) {
    Text(text, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = modifier.padding(horizontal = 16.dp, vertical = 4.dp))
}

/** Padding for form fields inside a [Page]. */
val FieldPad = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 6.dp)

@Composable
fun SectionDivider() = HorizontalDivider(Modifier.padding(vertical = 8.dp))

@Composable
fun ConfirmDialog(title: String, body: String? = null, confirm: String, destructive: Boolean = false, onDismiss: () -> Unit, onConfirm: () -> Unit) {
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(title) },
        text = body?.let { { Text(it) } },
        confirmButton = { TextButton({ onDismiss(); onConfirm() }) { Text(confirm, color = if (destructive) MaterialTheme.colorScheme.error else MaterialTheme.colorScheme.primary) } },
        dismissButton = { TextButton(onDismiss) { Text("Cancel") } },
    )
}

/** Holds "are you sure?" state: call ask(), render once. */
class Confirmation {
    var pending by mutableStateOf<ConfirmRequest?>(null)
    fun ask(title: String, body: String? = null, confirm: String = "OK", destructive: Boolean = false, action: () -> Unit) {
        pending = ConfirmRequest(title, body, confirm, destructive, action)
    }
}

data class ConfirmRequest(val title: String, val body: String?, val confirm: String, val destructive: Boolean, val action: () -> Unit)

@Composable
fun rememberConfirmation(): Confirmation {
    val c = remember { Confirmation() }
    c.pending?.let { p -> ConfirmDialog(p.title, p.body, p.confirm, p.destructive, onDismiss = { c.pending = null }, onConfirm = p.action) }
    return c
}

@Composable
fun TextInputDialog(title: String, label: String, initial: String = "", confirm: String = "Save", message: String? = null, keyboard: KeyboardType = KeyboardType.Text, onDismiss: () -> Unit, onDone: (String) -> Unit) {
    var text by remember { mutableStateOf(initial) }
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(title) },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
                if (message != null) Text(message, style = MaterialTheme.typography.bodyMedium)
                OutlinedTextField(text, { text = it }, label = { Text(label) }, singleLine = true, modifier = Modifier.fillMaxWidth(), keyboardOptions = KeyboardOptions(keyboardType = keyboard))
            }
        },
        confirmButton = { TextButton({ onDismiss(); onDone(text.trim()) }, enabled = text.isNotBlank()) { Text(confirm) } },
        dismissButton = { TextButton(onDismiss) { Text("Cancel") } },
    )
}

/** A drop-down choice, like a <select>. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun <T> SelectField(label: String, options: List<Pair<T, String>>, value: T, modifier: Modifier = FieldPad, enabled: Boolean = true, supporting: String? = null, onChange: (T) -> Unit) {
    var open by remember { mutableStateOf(false) }
    ExposedDropdownMenuBox(open, { if (enabled) open = it }, modifier) {
        OutlinedTextField(
            options.firstOrNull { it.first == value }?.second ?: value?.toString().orEmpty(), {}, readOnly = true, enabled = enabled, label = { Text(label) },
            trailingIcon = { ExposedDropdownMenuDefaults.TrailingIcon(open) }, modifier = Modifier.fillMaxWidth().menuAnchor(MenuAnchorType.PrimaryNotEditable, enabled),
            supportingText = supporting?.let { { Text(it) } },
        )
        ExposedDropdownMenu(open, { open = false }) {
            options.forEach { (v, l) ->
                DropdownMenuItem(text = { Text(l) }, trailingIcon = { if (v == value) Icon(Icons.Filled.Check, null) }, onClick = { open = false; onChange(v) })
            }
        }
    }
}

@Composable
fun Field(value: String, onChange: (String) -> Unit, label: String, modifier: Modifier = FieldPad, enabled: Boolean = true, placeholder: String? = null, supporting: String? = null, error: String? = null, password: Boolean = false, singleLine: Boolean = true, keyboard: KeyboardType = KeyboardType.Text, mono: Boolean = false) {
    OutlinedTextField(
        value, onChange, modifier, enabled = enabled, singleLine = singleLine, minLines = if (singleLine) 1 else 3, label = { Text(label) },
        placeholder = placeholder?.let { { Text(it) } },
        supportingText = (error ?: supporting)?.let { { Text(it) } }, isError = error != null,
        visualTransformation = if (password) PasswordVisualTransformation() else androidx.compose.ui.text.input.VisualTransformation.None,
        keyboardOptions = KeyboardOptions(keyboardType = if (password) KeyboardType.Password else keyboard),
        textStyle = if (mono) MaterialTheme.typography.bodyLarge.copy(fontFamily = FontFamily.Monospace) else MaterialTheme.typography.bodyLarge,
    )
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
fun ColorPicker(selected: String?, enabled: Boolean = true, onPick: (String) -> Unit) {
    FlowRow(Modifier.padding(horizontal = 16.dp, vertical = 6.dp), horizontalArrangement = Arrangement.spacedBy(10.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
        colorNames.forEach { c ->
            Box(
                Modifier.size(32.dp).clip(CircleShape).background(colorFor(c)).clickable(enabled = enabled) { onPick(c) }
                    .let { if (selected == c) it.border(3.dp, MaterialTheme.colorScheme.onSurface, CircleShape) else it },
                contentAlignment = Alignment.Center,
            ) { if (selected == c) Icon(Icons.Filled.Check, c, tint = Color.White, modifier = Modifier.size(18.dp)) }
        }
    }
}

/** A secret shown once (token, link, recovery codes): copy and share buttons. */
@Composable
fun SecretBox(label: String, value: String, modifier: Modifier = Modifier) {
    val ctx = LocalContext.current
    Column(modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Text(label, style = MaterialTheme.typography.labelLarge)
        Text(value, style = MaterialTheme.typography.bodyMedium.copy(fontFamily = FontFamily.Monospace), modifier = Modifier.fillMaxWidth().clip(RoundedCornerShape(10.dp)).background(MaterialTheme.colorScheme.surfaceVariant).padding(12.dp))
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            OutlinedButton({ copyText(ctx, label, value) }) { Icon(Icons.Outlined.ContentCopy, null, Modifier.size(18.dp)); Spacer(Modifier.width(6.dp)); Text("Copy") }
            OutlinedButton({ shareText(ctx, value, label) }) { Icon(Icons.Outlined.Share, null, Modifier.size(18.dp)); Spacer(Modifier.width(6.dp)); Text("Share") }
        }
    }
}

@Composable
fun SecretDialog(title: String, label: String, value: String, note: String? = null, onDismiss: () -> Unit) {
    AlertDialog(
        onDismissRequest = onDismiss, title = { Text(title) },
        text = { Column(verticalArrangement = Arrangement.spacedBy(10.dp)) { SecretBox(label, value); if (note != null) Text(note, style = MaterialTheme.typography.bodySmall) } },
        confirmButton = { TextButton(onDismiss) { Text("Done") } },
    )
}

/**
 * Asks for the account password (and, when the server wants it, the two-step code), then runs
 * [action] with them. Used for changes the server only accepts after a fresh sign-in.
 */
@Composable
fun PasswordConfirmDialog(title: String, message: String, confirm: String, extra: (@Composable ColumnScope.() -> Unit)? = null, canConfirm: Boolean = true, onDismiss: () -> Unit, action: suspend (password: String, code: String?) -> Unit) {
    var password by remember { mutableStateOf("") }
    var code by remember { mutableStateOf("") }
    var needCode by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    var busy by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()
    AlertDialog(
        onDismissRequest = { if (!busy) onDismiss() },
        title = { Text(title) },
        text = {
            Column(Modifier.verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                Text(message, style = MaterialTheme.typography.bodyMedium)
                extra?.invoke(this)
                OutlinedTextField(password, { password = it }, label = { Text("Your password") }, singleLine = true, modifier = Modifier.fillMaxWidth(), visualTransformation = PasswordVisualTransformation(), keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password))
                if (needCode) OutlinedTextField(code, { code = it }, label = { Text("Code from your authenticator app") }, singleLine = true, modifier = Modifier.fillMaxWidth(), keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Ascii))
                if (error != null) Text(error!!, color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodySmall)
            }
        },
        confirmButton = {
            TextButton({
                busy = true
                error = null
                scope.launch {
                    try {
                        action(password, code.ifBlank { null })
                        onDismiss()
                    } catch (e: ApiException) {
                        if (e.code == "two_factor_required") needCode = true
                        error = e.message
                    } catch (e: Exception) {
                        if (e is kotlinx.coroutines.CancellationException) throw e
                        error = e.friendly()
                    } finally {
                        busy = false
                    }
                }
            }, enabled = !busy && canConfirm && password.isNotEmpty() && (!needCode || code.isNotBlank())) { Text(if (busy) "Please wait…" else confirm) }
        },
        dismissButton = { TextButton(onDismiss, enabled = !busy) { Text("Cancel") } },
    )
}

/** Small counters in a row (admin and AI statistics). */
@Composable
fun StatTiles(items: List<Pair<String, String>>) {
    Row(Modifier.fillMaxWidth().padding(horizontal = 12.dp, vertical = 6.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        items.forEach { (label, value) ->
            Column(Modifier.weight(1f).clip(RoundedCornerShape(14.dp)).background(MaterialTheme.colorScheme.surfaceContainer).padding(12.dp)) {
                Text(value, style = MaterialTheme.typography.titleLarge)
                Text(label, style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 2)
            }
        }
    }
}

/** Round avatar with initials. */
@Composable
fun Initials(name: String, size: androidx.compose.ui.unit.Dp = 36.dp) {
    Box(Modifier.size(size).clip(CircleShape).background(MaterialTheme.colorScheme.primaryContainer), contentAlignment = Alignment.Center) {
        Text(name.split(" ").mapNotNull { it.firstOrNull()?.uppercase() }.take(2).joinToString(""), color = MaterialTheme.colorScheme.onPrimaryContainer, style = MaterialTheme.typography.labelLarge)
    }
}

/** A small rounded label, e.g. "Admin", "Disabled". */
@Composable
fun StatusBadge(text: String, tone: String = "neutral") {
    val cs = MaterialTheme.colorScheme
    val (bg, fg) = when (tone) {
        "accent" -> cs.primaryContainer to cs.onPrimaryContainer
        "danger" -> cs.errorContainer to cs.error
        "success" -> Color(0x3322C55E) to Color(0xFF15803D)
        else -> cs.surfaceVariant to cs.onSurfaceVariant
    }
    Pill(text, bg, fg)
}

/** A list item with a title, a detail line and buttons on the right. */
@Composable
fun ItemRow(title: String, detail: String? = null, error: String? = null, leading: (@Composable () -> Unit)? = null, badges: (@Composable () -> Unit)? = null, onClick: (() -> Unit)? = null, actions: @Composable () -> Unit = {}) {
    Row(Modifier.fillMaxWidth().let { if (onClick != null) it.clickable(onClick = onClick) else it }.padding(start = 16.dp, end = 4.dp, top = 8.dp, bottom = 8.dp), verticalAlignment = Alignment.CenterVertically) {
        if (leading != null) {
            leading()
            Spacer(Modifier.width(12.dp))
        }
        Column(Modifier.weight(1f)) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                Text(title, style = MaterialTheme.typography.bodyLarge, maxLines = 2, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f, fill = false))
                badges?.invoke()
            }
            if (!detail.isNullOrBlank()) Text(detail, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 3, overflow = TextOverflow.Ellipsis)
            if (!error.isNullOrBlank()) Text(error, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.error, maxLines = 2, overflow = TextOverflow.Ellipsis)
        }
        actions()
    }
}

/** "3 documents", "1 document". */
fun plural(n: Int, one: String, many: String = one + "s") = "$n " + if (n == 1) one else many

/** Full date and time for an ISO timestamp. */
fun formatDateTime(iso: String?, format: String = "DD/MM/YYYY"): String {
    if (iso.isNullOrBlank()) return ""
    return runCatching {
        val t = java.time.OffsetDateTime.parse(iso).atZoneSameInstant(java.time.ZoneId.systemDefault())
        formatDate(t.toLocalDate().toString(), format) + " " + t.toLocalTime().withSecond(0).withNano(0).toString()
    }.getOrDefault(iso)
}

@Composable
fun SpacerH(h: Int) = Spacer(Modifier.height(h.dp))
