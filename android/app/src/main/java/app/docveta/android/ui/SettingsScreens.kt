package app.docveta.android.ui

import android.content.ActivityNotFoundException
import android.content.Intent
import android.graphics.BitmapFactory
import android.net.Uri
import android.util.Base64
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.Logout
import androidx.compose.material.icons.outlined.ArrowDownward
import androidx.compose.material.icons.outlined.ArrowUpward
import androidx.compose.material.icons.outlined.Bookmark
import androidx.compose.material.icons.outlined.Computer
import androidx.compose.material.icons.outlined.DeleteOutline
import androidx.compose.material.icons.outlined.Edit
import androidx.compose.material.icons.outlined.Key
import androidx.compose.material.icons.outlined.Link
import androidx.compose.material.icons.outlined.Notifications
import androidx.compose.material.icons.outlined.Person
import androidx.compose.material.icons.outlined.PhoneAndroid
import androidx.compose.material.icons.outlined.PushPin
import androidx.compose.material.icons.outlined.Security
import androidx.compose.material.icons.outlined.Smartphone
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.Checkbox
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilledTonalButton
import androidx.compose.material3.FilterChip
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TimePicker
import androidx.compose.material3.rememberTimePickerState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.key
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import app.docveta.android.BuildConfig
import app.docveta.android.data.ApiToken
import app.docveta.android.data.Channel
import app.docveta.android.data.SavedView
import app.docveta.android.data.TwoFactorSetup
import app.docveta.android.data.WebSession
import app.docveta.android.data.changePassword
import app.docveta.android.data.channels
import app.docveta.android.data.createToken
import app.docveta.android.data.deleteChannel
import app.docveta.android.data.deleteView
import app.docveta.android.data.disableTwoFactor
import app.docveta.android.data.identities
import app.docveta.android.data.newRecoveryCodes
import app.docveta.android.data.notificationPrefs
import app.docveta.android.data.revokeSession
import app.docveta.android.data.revokeToken
import app.docveta.android.data.saveChannel
import app.docveta.android.data.saveNotificationPrefs
import app.docveta.android.data.savedViews
import app.docveta.android.data.sessions
import app.docveta.android.data.setChannelEnabled
import app.docveta.android.data.testChannel
import app.docveta.android.data.tokens
import app.docveta.android.data.twoFactor
import app.docveta.android.data.unlinkIdentity
import app.docveta.android.data.updateProfile
import app.docveta.android.data.updateView
import app.docveta.android.data.RecoveryCodes
import app.docveta.android.scan.OcrModels
import app.docveta.android.scan.PhoneOcr
import app.docveta.android.scan.PpOcr
import kotlinx.coroutines.launch
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put

/* ------------------------------------------------------------------ index */

@Composable
fun SettingsScreen(onBack: () -> Unit, onNavigate: (String) -> Unit, onSignedOut: () -> Unit) {
    val c = LocalContainer.current
    val me = LocalMe.current
    val scope = rememberCoroutineScope()
    var confirmOut by remember { mutableStateOf(false) }
    Page("Settings", onBack) {
        Row(Modifier.padding(16.dp), verticalAlignment = Alignment.CenterVertically) {
            Initials(me.displayName, 48.dp)
            Spacer(Modifier.width(14.dp))
            Column {
                Text(me.displayName, style = MaterialTheme.typography.titleMedium)
                Text(me.email, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                Text(c.session.serverUrl.orEmpty().removePrefix("https://").removePrefix("http://"), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        }
        SectionDivider()
        NavRow("Profile", "Name, date format, time zone, appearance", Icons.Outlined.Person) { onNavigate("settings/profile") }
        NavRow("Security", "Password, two-step sign-in, signed-in devices", Icons.Outlined.Security) { onNavigate("settings/security") }
        NavRow("Notifications", "Gotify, ntfy, email, webhooks, quiet hours", Icons.Outlined.Notifications) { onNavigate("settings/notifications") }
        NavRow("Saved views", "Rename, pin, reorder", Icons.Outlined.Bookmark) { onNavigate("settings/views") }
        NavRow("API tokens", "For scripts, scanners and other apps", Icons.Outlined.Key) { onNavigate("settings/tokens") }
        NavRow("This phone", "Scanner, uploads, app lock", Icons.Outlined.PhoneAndroid) { onNavigate("settings/phone") }
        SectionDivider()
        Text("Docveta for Android ${BuildConfig.VERSION_NAME}", Modifier.padding(horizontal = 16.dp, vertical = 6.dp), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        OutlinedButton({ confirmOut = true }, Modifier.padding(16.dp).fillMaxWidth()) {
            Icon(Icons.AutoMirrored.Outlined.Logout, null)
            Spacer(Modifier.width(8.dp))
            Text("Sign out")
        }
    }
    if (confirmOut) ConfirmDialog("Sign out?", "This phone forgets its access to Docveta. Scans still waiting to upload are kept.", "Sign out", onDismiss = { confirmOut = false }) {
        scope.launch { runCatching { c.repo.signOut() }; onSignedOut() }
    }
}

/* ------------------------------------------------------------------ profile */

private val dateFormats = listOf("DD/MM/YYYY" to "31/12/2026 (DD/MM/YYYY)", "MM/DD/YYYY" to "12/31/2026 (MM/DD/YYYY)", "YYYY-MM-DD" to "2026-12-31 (ISO)", "DD.MM.YYYY" to "31.12.2026", "D MMM YYYY" to "31 Dec 2026")

@Composable
fun ProfileScreen(onBack: () -> Unit) {
    val c = LocalContainer.current
    val me = LocalMe.current
    val reloadMe = LocalReloadMe.current
    val run = rememberRunner()
    var name by remember(me.displayName) { mutableStateOf(me.displayName) }
    var zonePicker by remember { mutableStateOf(false) }
    fun save(key: String, value: String, msg: String = "Saved") = run.run(msg) { c.repo.updateProfile(buildJsonObject { put(key, value) }); reloadMe() }
    Page("Profile", onBack, subtitle = me.email) {
        SectionLabel("Name")
        Row(FieldPad, verticalAlignment = Alignment.CenterVertically) {
            OutlinedTextField(name, { name = it }, Modifier.weight(1f), singleLine = true, label = { Text("Name") })
            Spacer(Modifier.width(8.dp))
            Button({ save("display_name", name.trim()) }, enabled = !run.busy && name.isNotBlank() && name.trim() != me.displayName) { Text("Save") }
        }
        SectionLabel("Dates and time")
        SelectField("Date format", dateFormats, me.dateFormat, supporting = "Also decides how dates like 03/04 are read from documents.") { save("date_format", it) }
        NavRow("Time zone", me.timezone.replace('_', ' ')) { zonePicker = true }
        SectionLabel("Appearance")
        SelectField("Theme", listOf("system" to "Auto (follow the phone)", "light" to "Light", "dark" to "Dark"), c.theme.value) {
            c.setTheme(it)
            run.run { c.repo.updateProfile(buildJsonObject { put("theme", it) }) }
        }
    }
    if (zonePicker) ZonePicker(me.timezone, onDismiss = { zonePicker = false }) { save("timezone", it) }
}

/** Every time zone, searchable (there are hundreds, too many for a drop-down). */
@Composable
private fun ZonePicker(current: String, onDismiss: () -> Unit, onPick: (String) -> Unit) {
    var q by remember { mutableStateOf("") }
    val all = remember { java.time.ZoneId.getAvailableZoneIds().filter { it.contains('/') && !it.startsWith("Etc/") && !it.startsWith("SystemV/") }.sorted() }
    val shown = all.filter { it.replace('_', ' ').contains(q.trim(), ignoreCase = true) }
    AlertDialog(
        onDismissRequest = onDismiss, title = { Text("Time zone") },
        text = {
            Column {
                OutlinedTextField(q, { q = it }, placeholder = { Text("Search, e.g. Kolkata") }, singleLine = true, modifier = Modifier.fillMaxWidth())
                androidx.compose.foundation.lazy.LazyColumn(Modifier.fillMaxWidth().height(360.dp)) {
                    items(shown.size) { i ->
                        val z = shown[i]
                        Row(Modifier.fillMaxWidth().clickable { onPick(z); onDismiss() }.padding(vertical = 12.dp), verticalAlignment = Alignment.CenterVertically) {
                            Text(z.replace('_', ' '), Modifier.weight(1f), color = if (z == current) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.onSurface)
                        }
                    }
                }
            }
        },
        confirmButton = { TextButton(onDismiss) { Text("Cancel") } },
    )
}

/* ------------------------------------------------------------------ security */

@Composable
fun SecurityScreen(onBack: () -> Unit) {
    val c = LocalContainer.current
    val me = LocalMe.current
    val ctx = LocalContext.current
    val run = rememberRunner()
    val confirm = rememberConfirmation()
    val twoFa = rememberLoader { c.repo.twoFactor() }
    val sessions = rememberLoader { c.repo.sessions() }
    val ids = rememberLoader { runCatching { c.repo.identities() }.getOrDefault(emptyList()) }
    var changing by remember { mutableStateOf(false) }
    var setup by remember { mutableStateOf(false) }
    var disabling by remember { mutableStateOf(false) }
    var regen by remember { mutableStateOf(false) }
    var codes by remember { mutableStateOf<List<String>?>(null) }

    Page("Security", onBack) {
        SectionLabel("Password")
        if (me.hasPassword) {
            Hint("Use at least 10 characters. Changing it signs out your browsers; this phone stays signed in.")
            FilledTonalButton({ changing = true }, Modifier.padding(horizontal = 16.dp, vertical = 6.dp)) { Text("Change password") }
        } else Hint("You sign in with single sign-on. To also set a password, use Docveta in a browser (Settings, Security).")

        SectionDivider()
        SectionLabel("Two-step sign-in")
        val tf = twoFa.data
        Hint(
            when {
                tf == null -> "Checking…"
                tf.enabled -> "On. You have ${plural(tf.recoveryCodesLeft, "recovery code")} left; each works once if you lose your phone."
                me.hasPassword -> "Ask for a 6-digit code from an authenticator app after your password. Recommended if Docveta is reachable from the internet."
                else -> "Two-step sign-in applies to password sign-in. You sign in with single sign-on, which has its own protection."
            },
        )
        if (tf != null) Row(Modifier.padding(horizontal = 16.dp, vertical = 6.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            if (tf.enabled) {
                OutlinedButton({ regen = true }) { Text("New recovery codes") }
                TextButton({ disabling = true }) { Text("Turn off", color = MaterialTheme.colorScheme.error) }
            } else if (me.hasPassword) FilledTonalButton({ setup = true }) { Text("Turn on") }
        }

        val linked = ids.data.orEmpty()
        if (linked.isNotEmpty()) {
            SectionDivider()
            SectionLabel("Single sign-on")
            linked.forEach { i ->
                ItemRow(i.email.ifBlank { "Connected account" }, runCatching { Uri.parse(i.provider).host }.getOrNull() ?: i.provider, leading = { Icon(Icons.Outlined.Link, null) }) {
                    TextButton({ confirm.ask("Disconnect single sign-on?", "You won't be able to sign in with it until you connect it again (in a browser).", "Disconnect", true) { run.run { c.repo.unlinkIdentity(i.id); ids.reload() } } }) { Text("Disconnect") }
                }
            }
        }

        SectionDivider()
        SectionLabel("Signed-in browsers")
        Hint("Browsers where you are signed in to Docveta. This phone uses an access token instead (see API tokens).")
        val list = sessions.data.orEmpty()
        if (sessions.data != null && list.isEmpty()) Hint("No browser is signed in.")
        list.forEach { s ->
            val mobile = Regex("Android|iPhone|iPad|Mobile").containsMatchIn(s.userAgent)
            ItemRow(browserName(s.userAgent), "${s.ip} · active ${timeAgo(s.lastSeenAt)}", leading = { Icon(if (mobile) Icons.Outlined.Smartphone else Icons.Outlined.Computer, null, tint = MaterialTheme.colorScheme.onSurfaceVariant) }) {
                TextButton({ run.run("Signed out") { c.repo.revokeSession(s.id); sessions.reload() } }) { Text("Sign out") }
            }
        }
        if (list.isNotEmpty()) TextButton({ confirm.ask("Sign out all browsers?", null, "Sign out all", true) { run.run("Signed out") { c.repo.revokeSession("others"); sessions.reload() } } }, Modifier.padding(horizontal = 8.dp)) {
            Text("Sign out all browsers", color = MaterialTheme.colorScheme.error)
        }
    }

    if (changing) {
        var next by remember { mutableStateOf("") }
        PasswordConfirmDialog("Change password", "Enter your current password and the new one.", "Change password", canConfirm = next.length >= 10, extra = {
            OutlinedTextField(next, { next = it }, label = { Text("New password") }, singleLine = true, modifier = Modifier.fillMaxWidth(), visualTransformation = androidx.compose.ui.text.input.PasswordVisualTransformation(), supportingText = { Text("At least 10 characters") })
        }, onDismiss = { changing = false }) { pw, code ->
            c.repo.changePassword(pw, next, code)
            toast(ctx, "Password changed. Your browsers were signed out.")
            sessions.reload()
        }
    }
    if (setup) TwoFactorSetupFlow(onDismiss = { setup = false }) { got -> codes = got; twoFa.reload() }
    if (disabling) TwoFactorConfirm("Turn off two-step sign-in", "Confirm it's you with your password and a code from your app (or a recovery code).", "Turn off", onDismiss = { disabling = false }) { pw, code ->
        c.repo.disableTwoFactor(pw, code)
        toast(ctx, "Two-step sign-in is off")
        twoFa.reload()
    }
    if (regen) TwoFactorConfirm("New recovery codes", "The old recovery codes stop working.", "Create new codes", onDismiss = { regen = false }) { pw, code ->
        codes = c.repo.newRecoveryCodes(pw, code)
        twoFa.reload()
    }
    codes?.let { RecoveryCodesDialog(it) { codes = null } }
}

/** Password plus a code, both always asked (turning off two-step sign-in, new recovery codes). */
@Composable
private fun TwoFactorConfirm(title: String, message: String, confirm: String, onDismiss: () -> Unit, action: suspend (String, String) -> Unit) {
    var code by remember { mutableStateOf("") }
    PasswordConfirmDialog(title, message, confirm, canConfirm = code.isNotBlank(), extra = {
        OutlinedTextField(code, { code = it }, label = { Text("Code from your app") }, singleLine = true, modifier = Modifier.fillMaxWidth())
    }, onDismiss = onDismiss) { pw, _ -> action(pw, code) }
}

/** Turning on two-step sign-in: confirm the password, add the key to an authenticator app, prove it with a code. */
@Composable
private fun TwoFactorSetupFlow(onDismiss: () -> Unit, onDone: (List<String>) -> Unit) {
    val c = LocalContainer.current
    val ctx = LocalContext.current
    val scope = rememberCoroutineScope()
    var session by remember { mutableStateOf<WebSession?>(null) }
    var info by remember { mutableStateOf<TwoFactorSetup?>(null) }
    var code by remember { mutableStateOf("") }
    var error by remember { mutableStateOf<String?>(null) }
    var busy by remember { mutableStateOf(false) }
    fun close() {
        session?.let { s -> scope.launch { s.close() } }
        onDismiss()
    }
    val s = session
    if (s == null) {
        PasswordConfirmDialog("Turn on two-step sign-in", "Confirm it's you first.", "Continue", onDismiss = onDismiss) { pw, cd ->
            val ws = c.repo.openWebSession(pw, cd)
            try {
                info = ws.post<TwoFactorSetup>("/me/2fa/setup")
                session = ws
            } catch (e: Exception) {
                ws.close()
                throw e
            }
        }
        return
    }
    val i = info ?: return
    AlertDialog(
        onDismissRequest = { if (!busy) close() },
        title = { Text("Add Docveta to your authenticator app") },
        text = {
            Column(Modifier.verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                Text("On this phone, tap the button to add it to your authenticator app. On another device, scan the code. Then enter the 6 digits it shows.", style = MaterialTheme.typography.bodyMedium)
                FilledTonalButton({
                    try {
                        ctx.startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(i.uri)))
                    } catch (e: ActivityNotFoundException) {
                        toast(ctx, "No authenticator app is installed. Enter the key by hand.")
                    }
                }, Modifier.fillMaxWidth()) { Text("Add to authenticator app") }
                val bmp = remember(i.qr) {
                    runCatching { Base64.decode(i.qr.substringAfter("base64,"), Base64.DEFAULT).let { BitmapFactory.decodeByteArray(it, 0, it.size) } }.getOrNull()
                }
                if (bmp != null) Image(bmp.asImageBitmap(), "QR code for your authenticator app", Modifier.size(180.dp).align(Alignment.CenterHorizontally).clip(RoundedCornerShape(8.dp)).background(Color.White).padding(6.dp))
                Text("Key: ${i.secret}", style = MaterialTheme.typography.bodySmall.copy(fontFamily = FontFamily.Monospace))
                TextButton({ copyText(ctx, "Key", i.secret) }) { Text("Copy key") }
                OutlinedTextField(code, { code = it }, label = { Text("6-digit code") }, singleLine = true, modifier = Modifier.fillMaxWidth(), keyboardOptions = androidx.compose.foundation.text.KeyboardOptions(keyboardType = androidx.compose.ui.text.input.KeyboardType.Number))
                if (error != null) Text(error!!, color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodySmall)
            }
        },
        confirmButton = {
            TextButton({
                busy = true
                error = null
                scope.launch {
                    try {
                        val r = s.post<RecoveryCodes>("/me/2fa/enable", buildJsonObject { put("code", code.replace(" ", "")) }.toString())
                        s.close()
                        onDone(r.codes)
                        onDismiss()
                    } catch (e: Exception) {
                        if (e is kotlinx.coroutines.CancellationException) throw e
                        error = e.friendly()
                    } finally {
                        busy = false
                    }
                }
            }, enabled = !busy && code.replace(" ", "").length >= 6) { Text("Verify and turn on") }
        },
        dismissButton = { TextButton({ close() }, enabled = !busy) { Text("Cancel") } },
    )
}

@Composable
private fun RecoveryCodesDialog(codes: List<String>, onDismiss: () -> Unit) {
    AlertDialog(
        onDismissRequest = {}, title = { Text("Save your recovery codes") },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
                Text("If you lose your phone, each of these signs you in once. You won't see them again.", style = MaterialTheme.typography.bodyMedium)
                SecretBox("Recovery codes", codes.joinToString("\n"))
            }
        },
        confirmButton = { TextButton(onDismiss) { Text("I've saved them") } },
    )
}

fun browserName(ua: String): String {
    val os = when {
        "Android" in ua -> "Android"
        "iPhone" in ua || "iPad" in ua -> "iOS"
        "Windows" in ua -> "Windows"
        "Mac OS" in ua -> "macOS"
        "Linux" in ua -> "Linux"
        else -> ""
    }
    val br = when {
        "DocvetaAndroid" in ua -> "Docveta app"
        "Edg/" in ua -> "Edge"
        "Firefox/" in ua -> "Firefox"
        "Chrome/" in ua -> "Chrome"
        "Safari/" in ua -> "Safari"
        ua.isNotBlank() -> "App"
        else -> "Unknown"
    }
    return if (os.isNotEmpty()) "$br on $os" else br
}

/* ------------------------------------------------------------------ API tokens */

private val scopeLabels = listOf("documents:read" to "Read documents", "documents:write" to "Edit documents", "upload" to "Upload documents", "admin" to "Administration")

@Composable
fun TokensScreen(onBack: () -> Unit, onSignedOut: () -> Unit) {
    val c = LocalContainer.current
    val me = LocalMe.current
    val run = rememberRunner()
    val confirm = rememberConfirmation()
    val tokens = rememberLoader { c.repo.tokens() }
    var creating by remember { mutableStateOf(false) }
    var secret by remember { mutableStateOf<String?>(null) }
    Page("API tokens", onBack, actions = { if (me.hasPassword) TextButton({ creating = true }) { Text("New token") } }) {
        Hint("For scripts, scanners, Home Assistant, AI assistants (MCP at /mcp, read-only) and other apps. Use as: Authorization: Bearer <token>")
        if (!me.hasPassword) Hint("You sign in with single sign-on, so create tokens in a browser.")
        Loaded(tokens) { list ->
            if (list.isEmpty()) EmptyState(Icons.Outlined.Key, "No tokens", modifier = Modifier.fillMaxWidth())
            list.forEach { t: ApiToken ->
                val mine = t.id == c.session.tokenId
                ItemRow(
                    t.name, listOf(t.scopes.joinToString(", ") { s -> scopeLabels.firstOrNull { it.first == s }?.second ?: s }, t.lastUsedAt?.let { "used ${timeAgo(it)}" } ?: "never used", t.expiresAt?.let { "expires ${formatDate(it, me.dateFormat)}" }).filterNotNull().joinToString(" · "),
                    badges = { if (mine) StatusBadge("This phone", "accent") },
                ) {
                    TextButton({
                        if (mine) confirm.ask("Revoke this phone's token?", "This phone will be signed out.", "Revoke and sign out", true) { run.run { c.repo.signOut(); onSignedOut() } }
                        else confirm.ask("Revoke “${t.name}”?", "Apps using this token will stop working.", "Revoke", true) { run.run { c.repo.revokeToken(t.id); tokens.reload() } }
                    }) { Text("Revoke", color = MaterialTheme.colorScheme.error) }
                }
            }
        }
    }
    if (creating) {
        var name by remember { mutableStateOf("") }
        var scopes by remember { mutableStateOf(listOf("documents:read", "upload")) }
        var days by remember { mutableStateOf<Int?>(365) }
        PasswordConfirmDialog("New API token", "Something to recognise it by, then confirm with your password.", "Create token", canConfirm = name.isNotBlank() && scopes.isNotEmpty(), extra = {
            OutlinedTextField(name, { name = it }, label = { Text("Name") }, placeholder = { Text("e.g. Scanner, Home Assistant") }, singleLine = true, modifier = Modifier.fillMaxWidth())
            scopeLabels.filter { it.first != "admin" || me.isAdmin }.forEach { (s, l) ->
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Checkbox(s in scopes, { on -> scopes = if (on) scopes + s else scopes - s })
                    Text(l)
                }
            }
            SelectField("Expires", listOf(30 to "In 30 days", 90 to "In 90 days", 365 to "In 1 year", null to "Never"), days, Modifier.fillMaxWidth()) { days = it }
        }, onDismiss = { creating = false }) { pw, code ->
            secret = c.repo.createToken(pw, code, name.trim(), scopes, days)
            tokens.reload()
        }
    }
    secret?.let { SecretDialog("New API token", "Copy your new token now", it, "You won't see it again.") { secret = null } }
}

/* ------------------------------------------------------------------ notifications */

val eventLabels = mapOf(
    "document.processed" to "Document is ready",
    "document.failed" to "Document couldn't be processed",
    "note.mention" to "Someone mentions me",
    "reminder.due" to "Reminders",
    "security.new_login" to "New sign-in to my account",
    "security.token_created" to "New API token",
    "worker.offline" to "Processing worker offline (admins)",
    "worker.online" to "Processing worker back online (admins)",
    "storage.low" to "Low disk space, under 5 GB free (admins)",
    "security.2fa_changed" to "Two-step sign-in changed",
    "import.failed" to "A watched-folder file couldn't be imported",
    "workflow.notice" to "Workflow messages",
)

private data class ChannelType(val label: String, val fields: List<Triple<String, String, String>>)

private val channelTypes = linkedMapOf(
    "gotify" to ChannelType("Gotify", listOf(Triple("url", "Server URL", "https://gotify.example.com"), Triple("token", "Application token", ""))),
    "ntfy" to ChannelType("ntfy", listOf(Triple("server", "Server (optional)", "https://ntfy.sh"), Triple("topic", "Topic", "my-docveta-alerts"), Triple("token", "Access token (optional)", ""))),
    "email" to ChannelType("Email", listOf(Triple("to", "Send to (optional)", "Defaults to your account email"))),
    "webhook" to ChannelType("Webhook", listOf(Triple("url", "URL", "https://example.com/hooks/docveta"), Triple("secret", "Signing secret (optional)", "Generated if empty"))),
    "apprise" to ChannelType("Apprise", listOf(Triple("url", "Apprise API URL", "http://apprise:8000"), Triple("key", "Saved configuration key (optional)", ""), Triple("urls", "Or service URLs, comma-separated", "tgram://token/chat"), Triple("tag", "Tag (optional)", ""))),
)

@Composable
fun NotificationSettingsScreen(onBack: () -> Unit) {
    Page("Notifications", onBack) {
        ChannelsSection(system = false)
        SectionDivider()
        QuietHoursSection()
    }
}

/** Where notifications go: the person's own channels, or (system) the admin alert channels. */
@Composable
fun ChannelsSection(system: Boolean) {
    val c = LocalContainer.current
    val run = rememberRunner()
    val confirm = rememberConfirmation()
    val data = rememberLoader { c.repo.channels() }
    var editing by remember { mutableStateOf<Channel?>(null) }
    var adding by remember { mutableStateOf(false) }
    SectionLabel(if (system) "Admin alert channels" else "Where to notify me")
    Hint(if (system) "Instance alerts (worker offline, low disk) go to these channels, as well as to admins' in-app notifications." else "You always get notifications in Docveta. Add Gotify, ntfy, Apprise, email or a webhook to get them elsewhere too.")
    Loaded(data) { d ->
        val list = d.items.filter { it.system == system }
        if (list.isEmpty()) Hint("No channels yet.")
        list.forEach { ch ->
            ItemRow(
                ch.name, (if (ch.events.isEmpty()) "All events" else ch.events.joinToString(", ") { eventLabels[it] ?: it }), error = ch.lastError.takeIf { it.isNotBlank() }?.let { "Last error: $it" },
                leading = { Switch(ch.enabled, { on -> run.run { c.repo.setChannelEnabled(ch.id, on); data.reload() } }) },
                badges = { StatusBadge(channelTypes[ch.type]?.label ?: ch.type) },
            ) {
                TextButton({ run.run("Test sent") { c.repo.testChannel(ch.id) } }) { Text("Test") }
                IconButton({ editing = ch }) { Icon(Icons.Outlined.Edit, "Edit") }
                IconButton({ confirm.ask("Remove “${ch.name}”?", null, "Remove", true) { run.run { c.repo.deleteChannel(ch.id); data.reload() } } }) { Icon(Icons.Outlined.DeleteOutline, "Remove") }
            }
        }
        FilledTonalButton({ adding = true }, Modifier.padding(horizontal = 16.dp, vertical = 6.dp)) { Text("Add channel") }
        if (adding || editing != null) ChannelDialog(editing, system, d.eventTypes, d.emailReady, onDismiss = { adding = false; editing = null }) { data.reload() }
    }
}

@OptIn(androidx.compose.foundation.layout.ExperimentalLayoutApi::class)
@Composable
private fun ChannelDialog(channel: Channel?, system: Boolean, eventTypes: List<String>, emailReady: Boolean, onDismiss: () -> Unit, onSaved: () -> Unit) {
    val c = LocalContainer.current
    var type by remember { mutableStateOf(channel?.type ?: "gotify") }
    var name by remember { mutableStateOf(channel?.name ?: "") }
    var config by remember { mutableStateOf(channel?.config ?: emptyMap()) }
    var events by remember { mutableStateOf(channel?.events ?: emptyList()) }
    var error by remember { mutableStateOf<String?>(null) }
    var busy by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()
    val isAdmin = LocalMe.current?.isAdmin == true
    fun adminEvent(e: String) = e.startsWith("worker.") || e.startsWith("storage.")
    val shown = eventTypes.filter { if (system) adminEvent(it) else isAdmin || !adminEvent(it) }
    val t = channelTypes[type] ?: channelTypes.values.first()
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(if (channel != null) "Edit ${channel.name}" else "Add notification channel") },
        text = {
            Column(Modifier.verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                if (channel == null) androidx.compose.foundation.layout.FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                    channelTypes.forEach { (k, v) -> FilterChip(type == k, { type = k; config = emptyMap() }, label = { Text(v.label) }) }
                }
                OutlinedTextField(name, { name = it }, label = { Text("Name") }, placeholder = { Text(t.label) }, singleLine = true, modifier = Modifier.fillMaxWidth())
                t.fields.forEach { (k, label, ph) ->
                    OutlinedTextField(config[k].orEmpty(), { v -> config = config + (k to v) }, label = { Text(label) }, placeholder = { if (ph.isNotEmpty()) Text(ph) }, singleLine = true, modifier = Modifier.fillMaxWidth())
                }
                if (type == "email") {
                    if (emailReady) Text("Email uses the server's email settings (Administration, Email).", style = MaterialTheme.typography.bodySmall)
                    else Text(
                        "This server can't send email yet, so nothing will arrive. " + if (isAdmin) "Set it up under Administration, Email first." else "Ask your administrator to set up email.",
                        style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.error,
                    )
                }
                Text("Send these events", style = MaterialTheme.typography.labelLarge)
                shown.forEach { ev ->
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        Checkbox(ev in events, { on -> events = if (on) events + ev else events - ev })
                        Text(eventLabels[ev] ?: ev, style = MaterialTheme.typography.bodyMedium)
                    }
                }
                Text("Leave all unchecked to receive everything.", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                if (error != null) Text(error!!, color = MaterialTheme.colorScheme.error)
            }
        },
        confirmButton = {
            TextButton({
                busy = true
                scope.launch {
                    try {
                        c.repo.saveChannel(channel, name.ifBlank { t.label }, type, config, events, system)
                        onSaved()
                        onDismiss()
                    } catch (e: Exception) {
                        error = (e as? app.docveta.android.data.ApiException)?.let { ex -> ex.fields.joinToString(" · ") { it.message }.ifBlank { ex.message } } ?: e.friendly()
                    } finally {
                        busy = false
                    }
                }
            }, enabled = !busy) { Text("Save") }
        },
        dismissButton = { TextButton(onDismiss) { Text("Cancel") } },
    )
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun QuietHoursSection() {
    val c = LocalContainer.current
    val run = rememberRunner()
    val prefs = rememberLoader { c.repo.notificationPrefs() }
    var picking by remember { mutableStateOf<String?>(null) }
    SectionLabel("Quiet hours")
    Hint("Hold messages to your phone, email and webhooks overnight. They're sent when quiet hours end; in-app notifications are unaffected.")
    val p = prefs.data ?: return
    fun save(n: app.docveta.android.data.NotificationPrefs) = run.run("Saved") { prefs.data = c.repo.saveNotificationPrefs(n) }
    ToggleRow("Pause pushes during quiet hours", null, p.quietEnabled) { save(p.copy(quietEnabled = it)) }
    if (p.quietEnabled) {
        NavRow("From", p.quietStart, trailing = {}) { picking = "start" }
        NavRow("Until", p.quietEnd + " (your time zone)", trailing = {}) { picking = "end" }
    }
    picking?.let { which ->
        val cur = (if (which == "start") p.quietStart else p.quietEnd).split(":")
        val st = rememberTimePickerState(cur.getOrNull(0)?.toIntOrNull() ?: 22, cur.getOrNull(1)?.toIntOrNull() ?: 0, is24Hour = true)
        AlertDialog(
            onDismissRequest = { picking = null },
            text = { TimePicker(st) },
            confirmButton = {
                TextButton({
                    picking = null
                    val v = "%02d:%02d".format(st.hour, st.minute)
                    save(if (which == "start") p.copy(quietStart = v) else p.copy(quietEnd = v))
                }) { Text("OK") }
            },
            dismissButton = { TextButton({ picking = null }) { Text("Cancel") } },
        )
    }
}

/* ------------------------------------------------------------------ saved views */

@Composable
fun SavedViewsScreen(onBack: () -> Unit, onOpen: (String) -> Unit) {
    val c = LocalContainer.current
    val me = LocalMe.current
    val run = rememberRunner()
    val confirm = rememberConfirmation()
    val views = rememberLoader { c.repo.savedViews() }
    var renaming by remember { mutableStateOf<SavedView?>(null) }
    fun patch(v: SavedView, body: kotlinx.serialization.json.JsonObject) = run.run { c.repo.updateView(v.id, body); views.reload() }
    fun move(list: List<SavedView>, i: Int, d: Int) = run.run {
        val order = list.toMutableList()
        val tmp = order[i]; order[i] = order[i + d]; order[i + d] = tmp
        order.forEachIndexed { idx, v -> if (v.sortOrder != idx) c.repo.updateView(v.id, buildJsonObject { put("sort_order", idx) }) }
        views.reload()
    }
    Page("Saved views", onBack) {
        Hint("Filters you saved from Documents. Pinned views show at the top of Documents, in this order.")
        Loaded(views) { list ->
            if (list.isEmpty()) EmptyState(Icons.Outlined.Bookmark, "No saved views yet", "Search or filter in Documents, then choose Save view.", Modifier.fillMaxWidth())
            list.forEachIndexed { i, v ->
                ItemRow(v.name, v.spaceId?.let { sid -> "Shared in " + (me.spaces.firstOrNull { it.id == sid }?.label ?: "a space") }, onClick = { onOpen(v.id) }, badges = { if (v.pinned) Icon(Icons.Outlined.PushPin, "Pinned", Modifier.size(16.dp), tint = MaterialTheme.colorScheme.primary) }) {
                    if (v.canEdit) {
                        IconButton({ move(list, i, -1) }, enabled = i > 0) { Icon(Icons.Outlined.ArrowUpward, "Move up") }
                        IconButton({ move(list, i, 1) }, enabled = i < list.lastIndex) { Icon(Icons.Outlined.ArrowDownward, "Move down") }
                        IconButton({ patch(v, buildJsonObject { put("pinned", !v.pinned) }) }) { Icon(Icons.Outlined.PushPin, if (v.pinned) "Unpin" else "Pin", tint = if (v.pinned) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.onSurfaceVariant) }
                        IconButton({ renaming = v }) { Icon(Icons.Outlined.Edit, "Rename") }
                        IconButton({ confirm.ask("Delete “${v.name}”?", "Only the saved view goes; no documents are deleted.", "Delete view", true) { run.run { c.repo.deleteView(v.id); views.reload() } } }) { Icon(Icons.Outlined.DeleteOutline, "Delete") }
                    }
                }
            }
        }
    }
    renaming?.let { v -> TextInputDialog("Rename view", "Name", v.name, onDismiss = { renaming = null }) { patch(v, buildJsonObject { put("name", it) }) } }
}

/* ------------------------------------------------------------------ this phone */

@Composable
fun PhoneSettingsScreen(onBack: () -> Unit) {
    val c = LocalContainer.current
    val s = c.session
    var wifi by remember { mutableStateOf(s.wifiOnlyUploads) }
    var auto by remember { mutableStateOf(s.autoCapture) }
    var lock by remember { mutableStateOf(s.appLock) }
    var ocr by remember { mutableStateOf(s.phoneOcr) }
    Page("This phone", onBack) {
        SectionLabel("Scanner")
        ToggleRow("Take the picture by itself", "When the page is found and held still.", auto) { auto = it; s.autoCapture = it }
        if (!PhoneOcr.supported(c.context)) Hint("This phone can't read scans' text itself (it needs a 64-bit phone); the server reads them.")
        else PhoneOcrSettings(ocr) { ocr = it; s.phoneOcr = it }
        SectionLabel("Uploads")
        ToggleRow("Wi-Fi only", "Wait for Wi-Fi before sending, to save mobile data.", wifi) { wifi = it; s.wifiOnlyUploads = it; c.uploads.schedule() }
        SectionLabel("Security")
        ToggleRow("Lock the app", "Ask for your fingerprint, face or screen lock when opening Docveta.", lock) { lock = it; s.appLock = it }
    }
}

/** Reading text on the phone: when, and in which languages (Tamil, Telugu and Kannada are downloaded once). */
@Composable
private fun PhoneOcrSettings(ocr: String, onChange: (String) -> Unit) {
    val c = LocalContainer.current
    val s = c.session
    val ctx = c.context
    SelectField("Read the text on this phone", listOf("auto" to "Automatic", "on" to "Always", "off" to "Never"), ocr) { onChange(it) }
    Hint(
        when (ocr) {
            "on" -> "Scans are read here before they're sent, so the server doesn't have to. It works offline and in the background, and is skipped in battery saver, on low battery and on phones with little memory."
            "off" -> "The server reads every scan."
            else -> "Scans are read here only when the server can't read text itself" +
                (if (s.serverReadsText) " (yours can, so the server does it)." else " (yours can't right now, so the phone does it).") +
                " Skipped in battery saver, on low battery and on phones with little memory."
        },
    )
    if (ocr == "off") return
    SectionLabel("Languages it reads")
    // Re-read when a download finishes or a language is removed.
    var changed by remember { mutableStateOf(0) }
    val available = remember(changed) { OcrModels.available(ctx) }
    val downloadable = remember(changed) { OcrModels.downloadable(ctx) }
    for (script in PpOcr.SCRIPTS.filter { it in available || it in downloadable }) key(script) {
        val name = OcrModels.NAMES.getValue(script)
        val downloading by OcrModels.fetching(ctx, script).collectAsState(false)
        LaunchedEffect(downloading) { if (!downloading) changed++ }
        val bundled = script == "en" || script == "devanagari"
        ItemRow(
            name,
            detail = when {
                script in available -> if (bundled) "Included" else "Downloaded"
                downloading -> "Downloading… (about 9 MB)"
                else -> "Not on this phone (about 9 MB)"
            },
        ) {
            when {
                script in available && !bundled -> TextButton({ OcrModels.remove(ctx, script); changed++ }) { Text("Remove") }
                script !in available && !downloading -> TextButton({ OcrModels.fetch(ctx, script, anyNetwork = true) }) { Text("Download") }
            }
        }
    }
    Hint("Each page's language is recognised from its text. Text in a language that isn't here yet isn't read on the phone; that language is downloaded on Wi-Fi for the next scans.")
}
