package app.docveta.android.ui

import android.app.DownloadManager
import android.content.Context
import android.net.Uri
import android.os.Environment
import androidx.compose.foundation.background
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.AdminPanelSettings
import androidx.compose.material.icons.outlined.Badge
import androidx.compose.material.icons.outlined.Bolt
import androidx.compose.material.icons.outlined.Dns
import androidx.compose.material.icons.outlined.DeleteOutline
import androidx.compose.material.icons.outlined.Download
import androidx.compose.material.icons.outlined.Edit
import androidx.compose.material.icons.outlined.Email
import androidx.compose.material.icons.outlined.FolderOpen
import androidx.compose.material.icons.outlined.Group
import androidx.compose.material.icons.outlined.Key
import androidx.compose.material.icons.outlined.Memory
import androidx.compose.material.icons.outlined.MoreVert
import androidx.compose.material.icons.outlined.NotificationsActive
import androidx.compose.material.icons.outlined.Psychology
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material.icons.automirrored.outlined.ReceiptLong
import androidx.compose.material.icons.outlined.ShieldMoon
import androidx.compose.material.icons.outlined.TableChart
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.Checkbox
import androidx.compose.material3.FilledTonalButton
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import app.docveta.android.data.AdminUser
import app.docveta.android.data.AiProvider
import app.docveta.android.data.ApiException
import app.docveta.android.data.OidcConfig
import app.docveta.android.data.ProcessingSettings
import app.docveta.android.data.SmtpConfig
import app.docveta.android.data.WatchedFolder
import app.docveta.android.data.adminUsers
import app.docveta.android.data.aiProviders
import app.docveta.android.data.audit
import app.docveta.android.data.createInvite
import app.docveta.android.data.createUser
import app.docveta.android.data.createWorker
import app.docveta.android.data.deleteFolder
import app.docveta.android.data.deleteProvider
import app.docveta.android.data.deleteUser
import app.docveta.android.data.deleteWorker
import app.docveta.android.data.folders
import app.docveta.android.data.invites
import app.docveta.android.data.officeInfo
import app.docveta.android.data.oidcConfig
import app.docveta.android.data.processingSettings
import app.docveta.android.data.reindex
import app.docveta.android.data.resetUserTwoFactor
import app.docveta.android.data.retryTask
import app.docveta.android.data.revokeInvite
import app.docveta.android.data.rotateWorkerToken
import app.docveta.android.data.saveFolder
import app.docveta.android.data.saveOffice
import app.docveta.android.data.saveOidc
import app.docveta.android.data.saveProcessingSettings
import app.docveta.android.data.saveProvider
import app.docveta.android.data.saveServerSettings
import app.docveta.android.data.saveSmtp
import app.docveta.android.data.scanFolder
import app.docveta.android.data.serverSettings
import app.docveta.android.data.setFolderEnabled
import app.docveta.android.data.setProviderEnabled
import app.docveta.android.data.setWorkerEnabled
import app.docveta.android.data.smtpConfig
import app.docveta.android.data.systemInfo
import app.docveta.android.data.tasks
import app.docveta.android.data.testOffice
import app.docveta.android.data.testProvider
import app.docveta.android.data.testSmtp
import app.docveta.android.data.updateUser
import app.docveta.android.data.workers
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put

/* ------------------------------------------------------------------ index */

@Composable
fun AdminScreen(onBack: () -> Unit, onNavigate: (String) -> Unit) {
    val c = LocalContainer.current
    val me = LocalMe.current
    var allowed by remember { mutableStateOf(c.repo.hasAdminScope) }
    var enabling by remember { mutableStateOf(false) }
    val ctx = LocalContext.current
    // Older sign-ins: find out whether the stored token can do administration after all.
    LaunchedEffect(Unit) {
        if (!allowed) allowed = runCatching { c.repo.systemInfo(); c.session.tokenScopes = (c.session.tokenScopes ?: "documents:read,documents:write,upload") + ",admin"; true }.getOrDefault(false)
    }
    Page("Administration", onBack) {
        if (!me.isAdmin) {
            EmptyState(Icons.Outlined.AdminPanelSettings, "Administrators only", modifier = Modifier.fillMaxWidth())
            return@Page
        }
        if (!allowed) {
            Column(Modifier.padding(16.dp).fillMaxWidth().clip(androidx.compose.foundation.shape.RoundedCornerShape(14.dp)).background(MaterialTheme.colorScheme.primaryContainer.copy(alpha = 0.6f)).padding(14.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Text("Turn on administration on this phone", style = MaterialTheme.typography.titleSmall)
                Text(
                    if (me.hasPassword) "This phone signed in without the admin permission. Confirm your password to give it one."
                    else "This phone signed in without the admin permission. Sign out and sign in again to get it.",
                    style = MaterialTheme.typography.bodyMedium,
                )
                if (me.hasPassword) Button({ enabling = true }) { Text("Confirm password") }
            }
        }
        NavRow("Users", "People, invitations, administrators", Icons.Outlined.Group) { onNavigate("admin/users") }
        NavRow("Processing", "Workers, text recognition queue", Icons.Outlined.Memory) { onNavigate("admin/processing") }
        NavRow("AI", "Providers, meaning-based search", Icons.Outlined.Psychology) { onNavigate("admin/ai") }
        NavRow("Watched folders", "Import from folders on the server", Icons.Outlined.FolderOpen) { onNavigate("admin/folders") }
        NavRow("Office documents", "Word, Excel and PowerPoint (Gotenberg)", Icons.Outlined.TableChart) { onNavigate("admin/office") }
        NavRow("Single sign-on", "OpenID Connect", Icons.Outlined.Key) { onNavigate("admin/sso") }
        NavRow("Email", "SMTP server", Icons.Outlined.Email) { onNavigate("admin/email") }
        NavRow("Alerts", "Where instance alerts go", Icons.Outlined.NotificationsActive) { onNavigate("admin/alerts") }
        NavRow("Export", "Download everything as a zip", Icons.Outlined.Download) { onNavigate("admin/export") }
        NavRow("System", "Server address, health, storage", Icons.Outlined.Dns) { onNavigate("admin/system") }
        NavRow("Audit log", "Sign-ins and changes", Icons.AutoMirrored.Outlined.ReceiptLong) { onNavigate("admin/audit") }
    }
    if (enabling) PasswordConfirmDialog("Turn on administration", "This phone gets a new access token that includes administration.", "Turn on", onDismiss = { enabling = false }) { pw, code ->
        c.repo.enableAdminOnPhone(pw, code)
        allowed = true
        toast(ctx, "Administration is on for this phone")
    }
}

/* ------------------------------------------------------------------ users and invitations */

@Composable
fun AdminUsersScreen(onBack: () -> Unit) {
    val c = LocalContainer.current
    val me = LocalMe.current
    val run = rememberRunner()
    val confirm = rememberConfirmation()
    val users = rememberLoader { c.repo.adminUsers() }
    val invites = rememberLoader { c.repo.invites().filter { it.status == "pending" } }
    var editing by remember { mutableStateOf<AdminUser?>(null) }
    var adding by remember { mutableStateOf(false) }
    var inviting by remember { mutableStateOf(false) }
    var link by remember { mutableStateOf<app.docveta.android.data.InviteCreated?>(null) }
    Page("Users", onBack, actions = { TextButton({ adding = true }) { Text("Add user") } }) {
        SectionLabel("Invitations")
        Hint("Invite people with a link. They choose their own password; you never see it.")
        invites.data.orEmpty().forEach { i ->
            ItemRow(i.displayName.ifBlank { i.email ?: "Invitation" }, listOfNotNull(i.email, "expires ${formatDateTime(i.expiresAt, me.dateFormat)}", "by ${i.invitedBy}", i.spaces.takeIf { it.isNotEmpty() }?.joinToString(", ") { it.name ?: "a space" }).joinToString(" · ")) {
                TextButton({ run.run { c.repo.revokeInvite(i.id); invites.reload() } }) { Text("Cancel", color = MaterialTheme.colorScheme.error) }
            }
        }
        if (invites.data?.isEmpty() == true) Hint("No pending invitations.")
        FilledTonalButton({ inviting = true }, Modifier.padding(horizontal = 16.dp, vertical = 6.dp)) { Text("Invite someone") }
        SectionDivider()
        SectionLabel("Users")
        Hint("Everyone gets a private personal space. Add people to shared spaces from the space's Members page.")
        Loaded(users) { list ->
            list.forEach { u ->
                ItemRow(
                    u.displayName, "${u.email} · ${u.lastLoginAt?.let { "last seen ${timeAgo(it)}" } ?: "never signed in"}",
                    leading = { Initials(u.displayName) },
                    badges = {
                        if (u.isAdmin) StatusBadge("Admin", "accent")
                        if (u.status == "disabled") StatusBadge("Disabled", "danger")
                        if (!u.hasPassword) StatusBadge("SSO")
                    },
                    onClick = { editing = u },
                ) {
                    if (u.id != me.id) {
                        var menu by remember { mutableStateOf(false) }
                        Box {
                            IconButton({ menu = true }) { Icon(Icons.Outlined.MoreVert, "More") }
                            androidx.compose.material3.DropdownMenu(menu, { menu = false }) {
                                androidx.compose.material3.DropdownMenuItem(text = { Text("Edit") }, onClick = { menu = false; editing = u })
                                androidx.compose.material3.DropdownMenuItem(text = { Text(if (u.status == "active") "Disable" else "Enable") }, onClick = {
                                    menu = false
                                    run.run { c.repo.updateUser(u.id, buildJsonObject { put("status", if (u.status == "active") "disabled" else "active") }); users.reload() }
                                })
                                androidx.compose.material3.DropdownMenuItem(text = { Text("Turn off two-step sign-in") }, onClick = {
                                    menu = false
                                    confirm.ask("Turn off two-step sign-in for ${u.displayName}?", "Only if they lost their phone and recovery codes. They can set it up again themselves.", "Turn off") { run.run("Two-step sign-in turned off") { c.repo.resetUserTwoFactor(u.id) } }
                                })
                                androidx.compose.material3.DropdownMenuItem(text = { Text("Delete", color = MaterialTheme.colorScheme.error) }, onClick = {
                                    menu = false
                                    confirm.ask("Delete ${u.displayName}?", "Their personal space must be empty. Documents they added to shared spaces stay.", "Delete user", true) { run.run { c.repo.deleteUser(u.id); users.reload() } }
                                })
                            }
                        }
                    }
                }
            }
        }
    }
    if (adding || editing != null) UserDialog(editing, onDismiss = { adding = false; editing = null }) { users.reload() }
    if (inviting) InviteDialog(onDismiss = { inviting = false }) { created -> link = created; invites.reload() }
    link?.let { l ->
        SecretDialog("Invitation ready", "Send them this link", l.link, when { l.emailSent -> "We also emailed it."; l.emailError != null -> "The email couldn't be sent (${l.emailError}). Share the link yourself."; else -> null }) { link = null }
    }
}

@Composable
private fun UserDialog(user: AdminUser?, onDismiss: () -> Unit, onSaved: () -> Unit) {
    val c = LocalContainer.current
    val me = LocalMe.current
    val scope = rememberCoroutineScope()
    var name by remember { mutableStateOf(user?.displayName ?: "") }
    var email by remember { mutableStateOf(user?.email ?: "") }
    var password by remember { mutableStateOf("") }
    var admin by remember { mutableStateOf(user?.isAdmin ?: false) }
    var error by remember { mutableStateOf<String?>(null) }
    var busy by remember { mutableStateOf(false) }
    val ctx = LocalContext.current
    AlertDialog(
        onDismissRequest = onDismiss, title = { Text(if (user != null) "Edit ${user.displayName}" else "Add user") },
        text = {
            Column(Modifier.verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                OutlinedTextField(name, { name = it }, label = { Text("Name") }, singleLine = true, modifier = Modifier.fillMaxWidth())
                if (user == null) OutlinedTextField(email, { email = it }, label = { Text("Email") }, singleLine = true, modifier = Modifier.fillMaxWidth(), keyboardOptions = androidx.compose.foundation.text.KeyboardOptions(keyboardType = KeyboardType.Email))
                OutlinedTextField(password, { password = it }, label = { Text(if (user != null) "Reset password" else "Password") }, singleLine = true, modifier = Modifier.fillMaxWidth(),
                    visualTransformation = androidx.compose.ui.text.input.PasswordVisualTransformation(),
                    supportingText = { Text(if (user != null) "Leave empty to keep it. Resetting signs them out everywhere." else "Leave empty if they'll sign in with single sign-on.") })
                if (user?.id != me.id) Row(verticalAlignment = Alignment.CenterVertically) {
                    Column(Modifier.weight(1f)) {
                        Text("Administrator")
                        Text("Can manage users, workers and settings. Doesn't automatically see others' documents.", style = MaterialTheme.typography.bodySmall)
                    }
                    Switch(admin, { admin = it })
                }
                if (error != null) Text(error!!, color = MaterialTheme.colorScheme.error)
            }
        },
        confirmButton = {
            TextButton({
                busy = true
                scope.launch {
                    try {
                        if (user != null) c.repo.updateUser(user.id, buildJsonObject {
                            put("display_name", name.trim())
                            if (user.id != me.id) put("is_admin", admin)
                            if (password.isNotEmpty()) put("password", password)
                        }) else c.repo.createUser(email, name, password, admin)
                        toast(ctx, if (user != null) "User updated" else "User added. Share the password with them securely.")
                        onSaved()
                        onDismiss()
                    } catch (e: ApiException) {
                        error = e.fields.joinToString(" · ") { it.message }.ifBlank { e.message }
                    } catch (e: Exception) {
                        error = e.friendly()
                    } finally {
                        busy = false
                    }
                }
            }, enabled = !busy && name.isNotBlank() && (user != null || email.isNotBlank())) { Text(if (user != null) "Save" else "Add user") }
        },
        dismissButton = { TextButton(onDismiss) { Text("Cancel") } },
    )
}

@Composable
private fun InviteDialog(onDismiss: () -> Unit, onCreated: (app.docveta.android.data.InviteCreated) -> Unit) {
    val c = LocalContainer.current
    val me = LocalMe.current
    val scope = rememberCoroutineScope()
    val shared = me.spaces.filter { !it.isPersonal }
    var name by remember { mutableStateOf("") }
    var email by remember { mutableStateOf("") }
    var admin by remember { mutableStateOf(false) }
    var note by remember { mutableStateOf("") }
    var days by remember { mutableStateOf(7) }
    var send by remember { mutableStateOf(false) }
    var picked by remember { mutableStateOf<Map<String, String>>(emptyMap()) }
    var error by remember { mutableStateOf<String?>(null) }
    var busy by remember { mutableStateOf(false) }
    AlertDialog(
        onDismissRequest = onDismiss, title = { Text("Invite someone") },
        text = {
            Column(Modifier.verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                OutlinedTextField(name, { name = it }, label = { Text("Name") }, singleLine = true, modifier = Modifier.fillMaxWidth())
                OutlinedTextField(email, { email = it }, label = { Text("Email (optional)") }, supportingText = { Text("If set, only this address can accept.") }, singleLine = true, modifier = Modifier.fillMaxWidth())
                if (shared.isNotEmpty()) Text("Add to these spaces", style = MaterialTheme.typography.labelLarge)
                shared.forEach { s ->
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        Checkbox(s.id in picked, { on -> picked = if (on) picked + (s.id to "editor") else picked - s.id })
                        Text(s.name, Modifier.weight(1f))
                        picked[s.id]?.let { r -> SelectField("Role", listOf("viewer" to "Can view", "editor" to "Can edit", "owner" to "Owner"), r, Modifier.width(130.dp)) { picked = picked + (s.id to it) } }
                    }
                }
                SelectField("Link works for", listOf(1 to "1 day", 7 to "7 days", 30 to "30 days"), days, Modifier.fillMaxWidth()) { days = it }
                OutlinedTextField(note, { note = it }, label = { Text("Note to them (optional)") }, modifier = Modifier.fillMaxWidth())
                Row(verticalAlignment = Alignment.CenterVertically) { Text("Make them an administrator", Modifier.weight(1f)); Switch(admin, { admin = it }) }
                if (email.isNotBlank()) Row(verticalAlignment = Alignment.CenterVertically) { Text("Email them the link", Modifier.weight(1f)); Switch(send, { send = it }) }
                if (error != null) Text(error!!, color = MaterialTheme.colorScheme.error)
            }
        },
        confirmButton = {
            TextButton({
                busy = true
                scope.launch {
                    try {
                        onCreated(c.repo.createInvite(name, email, admin, note, days, send, picked))
                        onDismiss()
                    } catch (e: ApiException) {
                        error = e.fields.joinToString(" · ") { it.message }.ifBlank { e.message }
                    } catch (e: Exception) {
                        error = e.friendly()
                    } finally {
                        busy = false
                    }
                }
            }, enabled = !busy) { Text("Create invitation") }
        },
        dismissButton = { TextButton(onDismiss) { Text("Cancel") } },
    )
}

/* ------------------------------------------------------------------ processing */

@Composable
fun AdminProcessingScreen(onBack: () -> Unit, onOpenDoc: (String) -> Unit) {
    val c = LocalContainer.current
    val run = rememberRunner()
    val confirm = rememberConfirmation()
    var status by remember { mutableStateOf<String?>(null) }
    val workers = rememberLoader { c.repo.workers() }
    val tasks = rememberLoader(status) { c.repo.tasks(status) }
    var adding by remember { mutableStateOf(false) }
    var token by remember { mutableStateOf<Pair<String, String>?>(null) }
    // Keep the queue live while the screen is open.
    LaunchedEffect(status) { while (true) { delay(8000); tasks.reload(); workers.reload() } }
    Page("Processing", onBack, actions = { TextButton({ adding = true }) { Text("Add worker") } }) {
        val st = tasks.data?.stats
        StatTiles(listOf("Waiting" to (st?.queued?.toString() ?: "–"), "In progress" to (st?.leased?.toString() ?: "–"), "Pages read (24h)" to (st?.pagesDone24h?.toString() ?: "–"), "Failed (24h)" to (st?.failed24h?.toString() ?: "–")))
        SectionLabel("Processing workers")
        Hint("Workers read text from scans (OCR). Run one on a Rockchip NPU board (RK3588/RK3576/RK3566) or any computer; it connects with a token.")
        Loaded(workers) { list ->
            if (list.isEmpty()) Hint("No workers yet. Without one, Docveta still stores and shows everything and searches PDFs that already contain text.")
            list.forEach { w ->
                ItemRow(
                    w.name + if (w.version.isNotBlank()) "  v${w.version}" else "",
                    listOf(if (w.online) "Online" else w.lastSeenAt?.let { "Last seen ${timeAgo(it)}" } ?: "Never connected", w.host.ifBlank { null }, "${w.activeTasks} active", "${w.done24h} done / ${w.failed24h} failed today").filterNotNull().joinToString(" · ") +
                        if (w.capabilities.isNotEmpty()) "\n" + w.capabilities.joinToString("; ") { cap -> listOfNotNull(cap.task_type, cap.engine, cap.languages.takeIf { it.isNotEmpty() }?.joinToString(","), cap.tags.takeIf { it.isNotEmpty() }?.joinToString(",")).joinToString(" · ") } else "",
                    leading = { Box(Modifier.size(10.dp).clip(CircleShape).background(if (w.online) Color(0xFF22C55E) else MaterialTheme.colorScheme.outline)) },
                    badges = { if (!w.enabled) StatusBadge("Disabled", "danger") },
                ) {
                    var menu by remember { mutableStateOf(false) }
                    Box {
                        IconButton({ menu = true }) { Icon(Icons.Outlined.MoreVert, "More") }
                        androidx.compose.material3.DropdownMenu(menu, { menu = false }) {
                            androidx.compose.material3.DropdownMenuItem(text = { Text(if (w.enabled) "Disable" else "Enable") }, onClick = { menu = false; run.run { c.repo.setWorkerEnabled(w.id, !w.enabled); workers.reload() } })
                            androidx.compose.material3.DropdownMenuItem(text = { Text("New token") }, onClick = {
                                menu = false
                                confirm.ask("New token for ${w.name}?", "The old token stops working immediately.", "Rotate token") { run.run { token = w.name to c.repo.rotateWorkerToken(w.id) } }
                            })
                            androidx.compose.material3.DropdownMenuItem(text = { Text("Remove", color = MaterialTheme.colorScheme.error) }, onClick = {
                                menu = false
                                confirm.ask("Remove ${w.name}?", null, "Remove", true) { run.run { c.repo.deleteWorker(w.id); workers.reload() } }
                            })
                        }
                    }
                }
            }
        }
        SectionDivider()
        ProcessingSettingsSection()
        SectionDivider()
        SectionLabel("Recent tasks")
        SelectField("Show", listOf(null to "All", "queued" to "Waiting", "leased" to "In progress", "failed" to "Failed", "done" to "Done"), status) { status = it }
        val items = tasks.data?.items.orEmpty()
        if (tasks.data != null && items.isEmpty()) Hint("No tasks.")
        items.forEach { t ->
            ItemRow(
                t.documentTitle.ifBlank { "Document" }, listOfNotNull(t.type + (t.pageFrom?.let { " p$it–${t.pageTo}" } ?: ""), t.status + if (t.attempt > 1) " (try ${t.attempt})" else "", t.worker).joinToString(" · "),
                error = t.lastError.ifBlank { null }, onClick = { if (t.documentId.isNotBlank()) onOpenDoc(t.documentId) },
            ) {
                if (t.status == "failed") TextButton({ run.run("Queued again") { c.repo.retryTask(t.id); tasks.reload() } }) { Text("Retry") }
            }
        }
    }
    if (adding) TextInputDialog("Add processing worker", "Name", "rk3588-npu-1", "Create and show token", "A name to recognise this machine.", onDismiss = { adding = false }) { name ->
        run.run { token = name to c.repo.createWorker(name); workers.reload() }
    }
    token?.let { (name, t) ->
        SecretDialog("Token for $name", "Worker token", t, "Start the worker with DOCVETA_URL=${c.session.serverUrl} and DOCVETA_WORKER_TOKEN set to this token. See docs/workers.md for the Rockchip NPU and CPU workers.") { token = null }
    }
}

@Composable
private fun ProcessingSettingsSection() {
    val c = LocalContainer.current
    val run = rememberRunner()
    val loaded = rememberLoader { c.repo.processingSettings() }
    var s by remember { mutableStateOf<ProcessingSettings?>(null) }
    LaunchedEffect(loaded.data) { if (loaded.data != null) s = loaded.data }
    SectionLabel("Processing settings")
    val v = s ?: return
    Field(v.preferTags.joinToString(", "), { s = v.copy(preferTags = it.split(",").map(String::trim).filter(String::isNotEmpty)) }, "Prefer workers tagged", supporting = "Tasks go to workers with these tags first (e.g. npu).")
    Field(v.fallbackAfterMinutes.toString(), { s = v.copy(fallbackAfterMinutes = it.filter(Char::isDigit).toIntOrNull() ?: 0) }, "Fall back to any worker after (minutes)", supporting = "0 = immediately.", keyboard = KeyboardType.Number)
    Field(v.pageBatchSize.toString(), { s = v.copy(pageBatchSize = it.filter(Char::isDigit).toIntOrNull() ?: 1) }, "Pages per task", supporting = "Large PDFs are split so several workers or NPU cores can share them.", keyboard = KeyboardType.Number)
    Field(v.maxAttempts.toString(), { s = v.copy(maxAttempts = it.filter(Char::isDigit).toIntOrNull() ?: 1) }, "Attempts per task", keyboard = KeyboardType.Number)
    ToggleRow("Skip OCR when a PDF already has text", "Saves time on PDFs created by software (bank statements, e-bills).", v.skipOcrWithText) { s = v.copy(skipOcrWithText = it) }
    ToggleRow("Create searchable PDFs", "Adds an invisible text layer to scans so you can select and search text in any PDF reader.", v.archive) { s = v.copy(archive = it) }
    Button({ run.run("Saved") { s = c.repo.saveProcessingSettings(v) } }, Modifier.padding(horizontal = 16.dp, vertical = 6.dp), enabled = !run.busy) { Text("Save settings") }
}

/* ------------------------------------------------------------------ AI providers */

private data class Preset(val label: String, val url: String, val chat: String, val embed: String, val local: Boolean)

private val presets = listOf(
    Preset("OpenAI", "https://api.openai.com/v1", "gpt-4o-mini", "text-embedding-3-small", false),
    Preset("Ollama", "http://localhost:11434/v1", "llama3.1", "nomic-embed-text", true),
    Preset("LM Studio", "http://localhost:1234/v1", "", "", true),
    Preset("OpenRouter", "https://openrouter.ai/api/v1", "", "", false),
)

@Composable
fun AdminAiScreen(onBack: () -> Unit) {
    val c = LocalContainer.current
    val ctx = LocalContext.current
    val run = rememberRunner()
    val confirm = rememberConfirmation()
    val providers = rememberLoader { c.repo.aiProviders() }
    var editing by remember { mutableStateOf<AiProvider?>(null) }
    var adding by remember { mutableStateOf(false) }
    Page("AI", onBack, actions = { TextButton({ adding = true }) { Text("Add provider") } }) {
        Hint("Any OpenAI-compatible service works: OpenAI, OpenRouter, or a model on your own machine (Ollama, LM Studio). Document text goes only to providers added here, and only for spaces where AI is on.")
        Loaded(providers) { list ->
            if (list.isEmpty()) EmptyState(Icons.Outlined.Psychology, "No AI provider yet", "Add one for tag and sender suggestions, meaning-based search, similar documents and Ask.", Modifier.fillMaxWidth())
            list.forEach { p ->
                ItemRow(
                    p.name, "${p.baseUrl} · chat: ${p.chatModel.ifBlank { "none" }} · embeddings: ${p.embeddingModel.ifBlank { "none" }}" + (if (p.lastError.isBlank()) p.lastOkAt?.let { "\nWorked ${timeAgo(it)}" } ?: "" else ""),
                    error = p.lastError.ifBlank { null }?.let { "Last error: $it" },
                    leading = { Switch(p.enabled, { on -> run.run { c.repo.setProviderEnabled(p.id, on); providers.reload(); c.refreshAi() } }) },
                    badges = { if (p.isDefault) StatusBadge("Default", "accent"); StatusBadge(if (p.isLocal) "Local" else "Cloud", if (p.isLocal) "success" else "neutral") },
                    onClick = { editing = p },
                ) {
                    IconButton({
                        run.run {
                            val r = c.repo.testProvider(p.id)
                            val missing = listOfNotNull(if (r.chatModelFound == false) "chat model “${p.chatModel}”" else null, if (r.embeddingModelFound == false) "embedding model “${p.embeddingModel}”" else null)
                            toast(ctx, when { !r.ok -> r.error ?: "Couldn't connect"; missing.isNotEmpty() -> "Connected, but the provider doesn't list the " + missing.joinToString(" or "); else -> "Connected" })
                            providers.reload()
                        }
                    }) { Icon(Icons.Outlined.Bolt, "Test") }
                    IconButton({ confirm.ask("Remove ${p.name}?", "Suggestions already made stay. New ones stop until you add another provider.", "Remove", true) { run.run { c.repo.deleteProvider(p.id); providers.reload(); c.refreshAi() } } }) { Icon(Icons.Outlined.DeleteOutline, "Remove") }
                }
            }
            if (list.any { it.enabled && it.embeddingModel.isNotBlank() }) {
                SectionDivider()
                SectionLabel("Meaning-based search")
                Hint("New documents are prepared automatically. Run this once after adding a provider so older documents can be found by meaning too.")
                OutlinedButton({ run.run { val n = c.repo.reindex(); toast(ctx, if (n > 0) "Preparing ${plural(n, "document")} for meaning-based search" else "Everything is already prepared") } }, Modifier.padding(horizontal = 16.dp, vertical = 6.dp), enabled = !run.busy) {
                    Icon(Icons.Outlined.Refresh, null); Spacer(Modifier.width(8.dp)); Text("Prepare existing documents")
                }
            }
        }
    }
    if (adding || editing != null) ProviderDialog(editing, first = providers.data.isNullOrEmpty(), onDismiss = { adding = false; editing = null }) { providers.reload(); c.refreshAi() }
}

@Composable
private fun ProviderDialog(p: AiProvider?, first: Boolean, onDismiss: () -> Unit, onSaved: () -> Unit) {
    val c = LocalContainer.current
    val scope = rememberCoroutineScope()
    var name by remember { mutableStateOf(p?.name ?: "") }
    var url by remember { mutableStateOf(p?.baseUrl ?: "") }
    var key by remember { mutableStateOf("") }
    var chat by remember { mutableStateOf(p?.chatModel ?: "") }
    var embed by remember { mutableStateOf(p?.embeddingModel ?: "") }
    var local by remember { mutableStateOf(p?.isLocal ?: false) }
    var default by remember { mutableStateOf(p?.isDefault ?: first) }
    var timeout by remember { mutableStateOf((p?.timeoutSeconds ?: 60).toString()) }
    var conc by remember { mutableStateOf((p?.maxConcurrency ?: 2).toString()) }
    var error by remember { mutableStateOf<String?>(null) }
    var busy by remember { mutableStateOf(false) }
    AlertDialog(
        onDismissRequest = onDismiss, title = { Text(if (p != null) "Edit ${p.name}" else "Add an AI provider") },
        text = {
            Column(Modifier.verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                if (p == null) androidx.compose.foundation.layout.Row(Modifier.horizontalScroll(rememberScrollState()), horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                    presets.forEach { pr -> androidx.compose.material3.AssistChip({ if (name.isBlank()) name = pr.label; url = pr.url; chat = pr.chat; embed = pr.embed; local = pr.local }, label = { Text(pr.label) }) }
                }
                OutlinedTextField(name, { name = it }, label = { Text("Name") }, singleLine = true, modifier = Modifier.fillMaxWidth())
                OutlinedTextField(url, { url = it }, label = { Text("Address (base URL)") }, supportingText = { Text("Ends in /v1 for most services.") }, singleLine = true, modifier = Modifier.fillMaxWidth())
                OutlinedTextField(key, { key = it }, label = { Text("API key") }, supportingText = { Text(if (p?.hasApiKey == true) "Leave blank to keep the saved key." else "Not needed for most local models.") }, singleLine = true, modifier = Modifier.fillMaxWidth(), visualTransformation = androidx.compose.ui.text.input.PasswordVisualTransformation())
                OutlinedTextField(chat, { chat = it }, label = { Text("Chat model") }, supportingText = { Text("Used for suggestions and answers.") }, singleLine = true, modifier = Modifier.fillMaxWidth())
                OutlinedTextField(embed, { embed = it }, label = { Text("Embedding model") }, supportingText = { Text("Used for meaning-based search and similar documents.") }, singleLine = true, modifier = Modifier.fillMaxWidth())
                OutlinedTextField(timeout, { timeout = it.filter(Char::isDigit) }, label = { Text("Waits up to (seconds)") }, singleLine = true, modifier = Modifier.fillMaxWidth())
                OutlinedTextField(conc, { conc = it.filter(Char::isDigit) }, label = { Text("Requests at once") }, singleLine = true, modifier = Modifier.fillMaxWidth())
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Column(Modifier.weight(1f)) { Text("Runs on my own hardware"); Text("Spaces set to “local only” may use it.", style = MaterialTheme.typography.bodySmall) }
                    Switch(local, { local = it })
                }
                Row(verticalAlignment = Alignment.CenterVertically) { Text("Use as the default provider", Modifier.weight(1f)); Switch(default, { default = it }) }
                if (error != null) Text(error!!, color = MaterialTheme.colorScheme.error)
            }
        },
        confirmButton = {
            TextButton({
                busy = true
                scope.launch {
                    try {
                        c.repo.saveProvider(p?.id, buildJsonObject {
                            put("name", name.trim()); put("base_url", url.trim()); put("chat_model", chat.trim()); put("embedding_model", embed.trim())
                            put("is_local", local); put("is_default", default); put("timeout_seconds", timeout.toIntOrNull() ?: 60); put("max_concurrency", conc.toIntOrNull() ?: 2)
                            if (key.isNotBlank()) put("api_key", key.trim())
                        })
                        onSaved()
                        onDismiss()
                    } catch (e: ApiException) {
                        error = e.fields.joinToString(" · ") { it.message }.ifBlank { e.message }
                    } catch (e: Exception) {
                        error = e.friendly()
                    } finally {
                        busy = false
                    }
                }
            }, enabled = !busy && name.isNotBlank() && url.isNotBlank()) { Text("Save") }
        },
        dismissButton = { TextButton(onDismiss) { Text("Cancel") } },
    )
}

/* ------------------------------------------------------------------ watched folders */

@Composable
fun AdminFoldersScreen(onBack: () -> Unit) {
    val c = LocalContainer.current
    val me = LocalMe.current
    val ctx = LocalContext.current
    val run = rememberRunner()
    val confirm = rememberConfirmation()
    val data = rememberLoader { c.repo.folders() }
    var editing by remember { mutableStateOf<WatchedFolder?>(null) }
    var adding by remember { mutableStateOf(false) }
    Page("Watched folders", onBack, actions = { TextButton({ adding = true }) { Text("Watch a folder") } }) {
        Hint("Drop files into a folder on the server (for example from a network scanner) and Docveta imports them.")
        Loaded(data) { d ->
            Hint(if (d.roots.isNotEmpty()) "Allowed locations: " + d.roots.joinToString(", ") else "No location is allowed yet. Set DOCVETA_WATCH_ROOTS (for example /data/inbox) and restart Docveta.")
            if (d.items.isEmpty()) EmptyState(Icons.Outlined.FolderOpen, "No watched folders", modifier = Modifier.fillMaxWidth())
            d.items.forEach { f ->
                ItemRow(
                    f.path, "into ${me.spaces.firstOrNull { it.id == f.spaceId }?.label ?: "unknown space"} · ${f.importedCount} imported${if (f.failedCount > 0) ", ${f.failedCount} failed" else ""} · ${f.lastScanAt?.let { "checked ${timeAgo(it)}" } ?: "not checked yet"}",
                    error = f.lastError.ifBlank { null }, leading = { Switch(f.enabled, { on -> run.run { c.repo.setFolderEnabled(f.id, on); data.reload() } }) }, onClick = { editing = f },
                ) {
                    IconButton({ run.run { val r = c.repo.scanFolder(f.id); toast(ctx, "${r.imported} imported" + (if (r.failed > 0) ", ${r.failed} failed" else "") + (if (r.skipped > 0) ", ${r.skipped} skipped" else "")); data.reload() } }) { Icon(Icons.Outlined.Refresh, "Check now") }
                    IconButton({ confirm.ask("Stop watching this folder?", "Files already imported stay in Docveta. Nothing on disk is deleted.", "Stop watching", true) { run.run { c.repo.deleteFolder(f.id); data.reload() } } }) { Icon(Icons.Outlined.DeleteOutline, "Stop watching") }
                }
            }
        }
    }
    if (adding || editing != null) FolderDialog(editing, onDismiss = { adding = false; editing = null }) { data.reload() }
}

@Composable
private fun FolderDialog(f: WatchedFolder?, onDismiss: () -> Unit, onSaved: () -> Unit) {
    val c = LocalContainer.current
    val me = LocalMe.current
    val scope = rememberCoroutineScope()
    val writable = me.spaces.filter { it.canWrite }
    var path by remember { mutableStateOf(f?.path ?: "") }
    var space by remember { mutableStateOf(f?.spaceId ?: writable.firstOrNull()?.id ?: "") }
    var recursive by remember { mutableStateOf(f?.recursive ?: true) }
    var sub by remember { mutableStateOf(f?.subfolders ?: "none") }
    var after by remember { mutableStateOf(f?.afterImport ?: "move") }
    var stable by remember { mutableStateOf((f?.stableSeconds ?: 5).toString()) }
    var tags by remember { mutableStateOf(f?.tagIds ?: emptyList()) }
    var picking by remember { mutableStateOf(false) }
    val tagNames = rememberNames("tags", space)
    var error by remember { mutableStateOf<String?>(null) }
    var busy by remember { mutableStateOf(false) }
    AlertDialog(
        onDismissRequest = onDismiss, title = { Text(if (f != null) "Edit watched folder" else "Watch a folder") },
        text = {
            Column(Modifier.verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                OutlinedTextField(path, { path = it }, label = { Text("Folder on the server") }, supportingText = { Text("For example /data/inbox. Inside Docker, the folder as the container sees it.") }, singleLine = true, enabled = f == null, modifier = Modifier.fillMaxWidth())
                SelectField("Import into", writable.map { it.id to it.label }, space, Modifier.fillMaxWidth()) { space = it; tags = emptyList() }
                PickerField("Tags for everything imported", tags.map { tagNames[it] ?: "…" }, "None", onClear = { tags = emptyList() }) { picking = true }
                SelectField("Subfolders", listOf("none" to "Ignore folder names", "tag" to "Use the subfolder name as a tag", "space" to "Use the subfolder name as the space"), sub, Modifier.fillMaxWidth()) { sub = it }
                SelectField("After importing", listOf("move" to "Move the file to an “imported” folder", "delete" to "Delete the file"), after, Modifier.fillMaxWidth()) { after = it }
                OutlinedTextField(stable, { stable = it.filter(Char::isDigit) }, label = { Text("Wait until a file stops changing (seconds)") }, singleLine = true, modifier = Modifier.fillMaxWidth())
                Row(verticalAlignment = Alignment.CenterVertically) { Checkbox(recursive, { recursive = it }); Text("Also look inside subfolders") }
                if (error != null) Text(error!!, color = MaterialTheme.colorScheme.error)
            }
        },
        confirmButton = {
            TextButton({
                busy = true
                scope.launch {
                    try {
                        c.repo.saveFolder(f?.id, buildJsonObject {
                            put("path", path.trim()); put("space_id", space); put("recursive", recursive); put("subfolders", sub); put("after_import", after)
                            put("stable_seconds", stable.toIntOrNull() ?: 5); put("tag_ids", JsonArray(tags.map { JsonPrimitive(it) }))
                        })
                        onSaved()
                        onDismiss()
                    } catch (e: ApiException) {
                        error = e.fields.joinToString(" · ") { it.message }.ifBlank { e.message }
                    } catch (e: Exception) {
                        error = e.friendly()
                    } finally {
                        busy = false
                    }
                }
            }, enabled = !busy && path.isNotBlank() && space.isNotBlank()) { Text("Save") }
        },
        dismissButton = { TextButton(onDismiss) { Text("Cancel") } },
    )
    if (picking) TaxonomyPicker("tags", space, tags, multi = true, onDismiss = { picking = false }) { tags = it.map { t -> t.id }; picking = false }
}

/* ------------------------------------------------------------------ office, SSO, email, alerts */

@Composable
fun AdminOfficeScreen(onBack: () -> Unit) {
    val c = LocalContainer.current
    val run = rememberRunner()
    val info = rememberLoader { c.repo.officeInfo() }
    var url by remember { mutableStateOf<String?>(null) }
    Page("Office documents", onBack) {
        Hint("Word, Excel and PowerPoint files are converted to PDF by Gotenberg, a small service you run next to Docveta (docker compose --profile office up). Converted files can be viewed and searched; the original is kept untouched.")
        Loaded(info) { i ->
            val v = url ?: i.url
            Field(v, { url = it }, "Gotenberg address", enabled = !i.fromEnv, placeholder = "http://gotenberg:3000", supporting = if (i.fromEnv) "Set by the DOCVETA_GOTENBERG_URL environment variable; change it there." else "Leave empty to turn Office support off.", keyboard = KeyboardType.Uri)
            Row(Modifier.padding(horizontal = 16.dp, vertical = 6.dp), horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically) {
                if (!i.fromEnv) Button({ run.run("Saved") { c.repo.saveOffice(v); url = null; info.reload() } }, enabled = url != null && !run.busy) { Text("Save") }
                OutlinedButton({ run.run("Gotenberg answered") { c.repo.testOffice(v) } }, enabled = v.isNotBlank()) { Text("Test connection") }
                StatusBadge(if (i.enabled) "On" else "Off", if (i.enabled) "success" else "neutral")
            }
        }
    }
}

@Composable
fun AdminSsoScreen(onBack: () -> Unit) {
    val c = LocalContainer.current
    val ctx = LocalContext.current
    val run = rememberRunner()
    val loaded = rememberLoader { c.repo.oidcConfig() }
    var cfg by remember { mutableStateOf<OidcConfig?>(null) }
    var secret by remember { mutableStateOf("") }
    LaunchedEffect(loaded.data) { if (loaded.data != null) cfg = loaded.data }
    fun list(s: String) = s.split(",").map(String::trim).filter(String::isNotEmpty)
    Page("Single sign-on", onBack, actions = { TextButton({ val v = cfg ?: return@TextButton; run.run("Saved") { cfg = c.repo.saveOidc(v, secret); secret = "" } }, enabled = cfg != null && !run.busy) { Text("Save") } }) {
        Hint("Let people sign in with Authentik, Keycloak, Authelia, Pocket ID, Google and other OpenID Connect providers.")
        val v = cfg
        if (v == null) { Loaded(loaded) {}; return@Page }
        ToggleRow("Enable single sign-on", null, v.enabled) { cfg = v.copy(enabled = it) }
        Field(v.issuer, { cfg = v.copy(issuer = it) }, "Issuer URL", placeholder = "https://auth.example.com/application/o/docveta/", keyboard = KeyboardType.Uri)
        Field(v.clientId, { cfg = v.copy(clientId = it) }, "Client ID")
        Field(secret, { secret = it }, "Client secret", password = true, supporting = if (v.hasClientSecret) "A secret is saved. Enter a new one to replace it." else null)
        val redirect = v.redirectUri ?: (c.session.serverUrl.orEmpty() + "/api/v1/auth/oidc/callback")
        NavRow("Redirect URI (add this in your provider)", redirect, trailing = { TextButton({ copyText(ctx, "Redirect URI", redirect) }) { Text("Copy") } }) { copyText(ctx, "Redirect URI", redirect) }
        Field(v.buttonLabel, { cfg = v.copy(buttonLabel = it) }, "Button label")
        Field(v.scopes.joinToString(", "), { cfg = v.copy(scopes = list(it)) }, "Scopes")
        Field(v.groupsClaim, { cfg = v.copy(groupsClaim = it) }, "Groups claim")
        Field(v.allowedGroups.joinToString(", "), { cfg = v.copy(allowedGroups = list(it)) }, "Allowed groups", supporting = "Empty = anyone your provider lets through.")
        Field(v.adminGroups.joinToString(", "), { cfg = v.copy(adminGroups = list(it)) }, "Admin groups", supporting = "Members become Docveta administrators.")
        ToggleRow("Create accounts automatically", "New people who sign in get an account and a personal space.", v.autoProvision) { cfg = v.copy(autoProvision = it) }
        ToggleRow("Link to existing accounts by verified email", "Only if you trust your provider to verify email addresses.", v.linkByVerifiedEmail) { cfg = v.copy(linkByVerifiedEmail = it) }
        ToggleRow("Disable password sign-in", "Everyone must use single sign-on. Recovery: docveta user reset-password on the server.", v.disablePasswordLogin) { cfg = v.copy(disablePasswordLogin = it) }
    }
}

@Composable
fun AdminEmailScreen(onBack: () -> Unit) {
    val c = LocalContainer.current
    val run = rememberRunner()
    val loaded = rememberLoader { c.repo.smtpConfig() }
    var cfg by remember { mutableStateOf<SmtpConfig?>(null) }
    var pw by remember { mutableStateOf("") }
    LaunchedEffect(loaded.data) { if (loaded.data != null) cfg = loaded.data }
    Page("Email", onBack, actions = { TextButton({ val v = cfg ?: return@TextButton; run.run("Saved") { cfg = c.repo.saveSmtp(v, pw); pw = "" } }, enabled = cfg != null && !run.busy) { Text("Save") } }) {
        Hint("Used for email notifications and invitations.")
        val v = cfg
        if (v == null) { Loaded(loaded) {}; return@Page }
        ToggleRow("Enable email", null, v.enabled) { cfg = v.copy(enabled = it) }
        Field(v.host, { cfg = v.copy(host = it) }, "Server", placeholder = "smtp.gmail.com", keyboard = KeyboardType.Uri)
        Field(v.port.toString(), { cfg = v.copy(port = it.filter(Char::isDigit).toIntOrNull() ?: 0) }, "Port", keyboard = KeyboardType.Number)
        SelectField("Security", listOf("starttls" to "STARTTLS (587)", "tls" to "TLS (465)", "none" to "None (not recommended)"), v.security) { cfg = v.copy(security = it) }
        Field(v.username, { cfg = v.copy(username = it) }, "Username")
        Field(pw, { pw = it }, "Password", password = true, supporting = if (v.hasPassword) "Saved. Enter a new one to replace it." else null)
        Field(v.from, { cfg = v.copy(from = it) }, "From address", placeholder = "Docveta <docveta@example.com>")
        OutlinedButton({ run.run("Test email sent to your address") { c.repo.testSmtp() } }, Modifier.padding(horizontal = 16.dp, vertical = 6.dp), enabled = v.enabled && !run.busy) { Text("Send test") }
    }
}

@Composable
fun AdminAlertsScreen(onBack: () -> Unit) {
    Page("Alerts", onBack) { ChannelsSection(system = true) }
}

/* ------------------------------------------------------------------ export, system, audit */

@Composable
fun AdminExportScreen(onBack: () -> Unit) {
    val c = LocalContainer.current
    val ctx = LocalContext.current
    Page("Export", onBack) {
        Hint("A zip with every document and its text, notes, tags, custom fields and original files, in readable folders. It can be imported into another Docveta, or kept as a backup you can open without Docveta.")
        Button({
            val dm = ctx.getSystemService(Context.DOWNLOAD_SERVICE) as DownloadManager
            val req = DownloadManager.Request(Uri.parse(c.repo.api.url("/admin/export").toString()))
                .addRequestHeader("Authorization", "Bearer ${c.session.token}")
                .addRequestHeader("Origin", app.docveta.android.data.ApiClient.origin(c.session.serverUrl.orEmpty()))
                .setTitle("Docveta export").setMimeType("application/zip")
                .setNotificationVisibility(DownloadManager.Request.VISIBILITY_VISIBLE_NOTIFY_COMPLETED)
                .setDestinationInExternalPublicDir(Environment.DIRECTORY_DOWNLOADS, "docveta-export-${java.time.LocalDate.now()}.zip")
            dm.enqueue(req)
            toast(ctx, "Downloading to your Downloads folder")
        }, Modifier.padding(16.dp)) { Icon(Icons.Outlined.Download, null); Spacer(Modifier.width(8.dp)); Text("Download export") }
        Hint("Large libraries take a while to start; the download streams as it is built. For very large libraries use the command line on the server: docveta export --zip docveta-export.zip")
        SectionLabel("Import")
        Hint("Import an export on the server: unzip it, then run docveta import --dir export. It's safe to run twice: documents already there are skipped.")
    }
}

@Composable
fun AdminSystemScreen(onBack: () -> Unit) {
    val c = LocalContainer.current
    val run = rememberRunner()
    val info = rememberLoader { c.repo.systemInfo() }
    val server = rememberLoader { c.repo.serverSettings() }
    var url by remember { mutableStateOf<String?>(null) }
    Page("System", onBack) {
        SectionLabel("Server address")
        Hint("The address people use to open Docveta. It goes into links in emails, notifications, invitations and share links.")
        server.data?.let { s ->
            if (s.baseUrlFixed) Hint("Set by DOCVETA_BASE_URL: ${s.baseUrl}")
            else {
                val v = url ?: s.publicUrl
                Field(v, { url = it }, "Address", placeholder = s.detected, supporting = "The server sees this phone reach it at ${s.detected}.", keyboard = KeyboardType.Uri)
                Row(Modifier.padding(horizontal = 16.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    Button({ run.run("Saved") { server.data = c.repo.saveServerSettings(v, s.allowLocalTargets); url = null } }, enabled = !run.busy) { Text("Save") }
                    if (v != s.detected && s.detected.isNotBlank()) OutlinedButton({ url = s.detected }) { Text("Use detected") }
                }
            }
            ToggleRow(
                "Let everyone's notifications reach the local network",
                if (s.allowLocalEnv) "Allowed for everyone by DOCVETA_ALLOW_LOCAL_TARGETS." else "Channels set up by administrators can always use local addresses (192.168.x.x, 10.x.x.x). Turn this on to let other people's channels use them too.",
                s.allowLocalEnv || s.allowLocalTargets, enabled = !s.allowLocalEnv,
            ) { on -> run.run("Saved") { server.data = c.repo.saveServerSettings(url ?: s.publicUrl, on) } }
        }
        SectionDivider()
        SectionLabel("Health")
        Loaded(info) { s ->
            listOf(
                "Version" to s.version, "Platform" to "${s.platform} · ${s.goVersion}", "Documents" to "${s.documents} (${s.pages} pages)", "Users" to s.users.toString(),
                "File storage used" to formatBytes(s.storageBytes), "Free disk space" to (if (s.storageFreeBytes >= 0) formatBytes(s.storageFreeBytes) else "unknown"),
                "Database size" to formatBytes(s.databaseBytes), "Workers online" to "${s.workersOnline} of ${s.workersTotal}", "Processing queue" to "${s.queue.queued} waiting · ${s.queue.leased} in progress",
            ).forEach { (k, v) ->
                Row(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 5.dp)) {
                    Text(k, Modifier.weight(1f), color = MaterialTheme.colorScheme.onSurfaceVariant, style = MaterialTheme.typography.bodyMedium)
                    Text(v, style = MaterialTheme.typography.bodyMedium)
                }
            }
            Hint("Back up regularly: the database (pg_dump) and the data directory. See docs/operations.md.")
        }
    }
}

@Composable
fun AdminAuditScreen(onBack: () -> Unit) {
    val c = LocalContainer.current
    val me = LocalMe.current
    var action by remember { mutableStateOf<String?>(null) }
    val log = rememberLoader(action) { c.repo.audit(action) }
    Page("Audit log", onBack) {
        SelectField("Show", listOf(null to "All", "auth." to "Sign-ins", "user." to "Users", "token." to "API tokens", "worker." to "Workers", "settings." to "Settings", "space." to "Spaces"), action) { action = it }
        Loaded(log) { list ->
            if (list.isEmpty()) EmptyState(Icons.Outlined.ShieldMoon, "Nothing logged yet", modifier = Modifier.fillMaxWidth())
            list.forEach { e ->
                Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 7.dp)) {
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        Text(e.action, Modifier.weight(1f), style = MaterialTheme.typography.titleSmall, color = if ("failed" in e.action) MaterialTheme.colorScheme.error else MaterialTheme.colorScheme.onSurface)
                        Text(formatDateTime(e.at, me.dateFormat), style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                    Text(listOf(e.actorName.ifBlank { e.actorType }, e.ip).filter { it.isNotBlank() }.joinToString(" · "), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                    e.details?.takeIf { it.isNotEmpty() }?.let { d -> Text(d.entries.joinToString(" · ") { (k, v) -> "$k: ${(v as? JsonPrimitive)?.content ?: v.toString()}" }, style = MaterialTheme.typography.bodySmall, maxLines = 3) }
                }
            }
        }
    }
}
