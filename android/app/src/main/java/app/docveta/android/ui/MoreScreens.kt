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
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.KeyboardArrowRight
import androidx.compose.material.icons.automirrored.outlined.Logout
import androidx.compose.material.icons.outlined.CheckCircle
import androidx.compose.material.icons.outlined.CloudUpload
import androidx.compose.material.icons.outlined.ContentCopy
import androidx.compose.material.icons.outlined.DeleteOutline
import androidx.compose.material.icons.outlined.ErrorOutline
import androidx.compose.material.icons.outlined.Notifications
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material.icons.outlined.Settings
import androidx.compose.material.icons.outlined.UploadFile
import androidx.compose.material.icons.outlined.Bookmark
import androidx.compose.material.icons.outlined.AdminPanelSettings
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Badge
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
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
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import app.docveta.android.BuildConfig
import app.docveta.android.data.Notification
import kotlinx.coroutines.launch

@Composable
fun MoreScreen(onNavigate: (String) -> Unit, uploadsActive: Int) {
    val me = LocalMe.current
    val stats = LocalStats.current
    val pickFiles = LocalPickFiles.current
    Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState())) {
        Row(Modifier.fillMaxWidth().clickable { onNavigate("settings/profile") }.padding(16.dp), verticalAlignment = Alignment.CenterVertically) {
            Initials(me.displayName, 48.dp)
            Spacer(Modifier.width(14.dp))
            Column {
                Text(me.displayName, style = MaterialTheme.typography.titleMedium)
                Text(me.email, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        }
        HorizontalDivider()
        Item(Icons.Outlined.UploadFile, "Upload files", "PDFs, photos and Office files from this phone") { pickFiles() }
        Item(Icons.Outlined.CloudUpload, "Uploads", if (uploadsActive > 0) "$uploadsActive in progress" else "Waiting and finished uploads") { onNavigate("uploads") }
        Item(Icons.Outlined.Notifications, "Notifications", null) { onNavigate("notifications") }
        Item(Icons.Outlined.Bookmark, "Saved views", "Your saved searches and filters") { onNavigate("settings/views") }
        Item(Icons.Outlined.DeleteOutline, "Trash", stats?.trash?.takeIf { it > 0 }?.let { "$it document${if (it == 1) "" else "s"}" }) { onNavigate("trash") }
        HorizontalDivider(Modifier.padding(vertical = 8.dp))
        Row(Modifier.fillMaxWidth().padding(start = 16.dp, end = 4.dp), verticalAlignment = Alignment.CenterVertically) {
            Text("Spaces", style = MaterialTheme.typography.labelLarge, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.weight(1f))
            TextButton({ onNavigate("spaces") }) { Text("Manage") }
        }
        me.spaces.forEach { s ->
            Row(Modifier.fillMaxWidth().clickable { onNavigate("space/${s.id}") }.padding(horizontal = 16.dp, vertical = 10.dp), verticalAlignment = Alignment.CenterVertically) {
                SpaceDot(s, size = 10.dp)
                Spacer(Modifier.width(12.dp))
                Text(s.label, Modifier.weight(1f))
                Text("${s.documentCount}", style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
                Spacer(Modifier.width(8.dp))
                Icon(Icons.AutoMirrored.Outlined.KeyboardArrowRight, null, tint = MaterialTheme.colorScheme.outline)
            }
        }
        HorizontalDivider(Modifier.padding(vertical = 8.dp))
        Item(Icons.Outlined.Settings, "Settings", "Profile, security, notifications, tokens") { onNavigate("settings") }
        if (me.isAdmin) Item(Icons.Outlined.AdminPanelSettings, "Administration", "Users, workers, AI, sign-in, email, system") { onNavigate("admin") }
        Spacer(Modifier.height(24.dp))
    }
}

@Composable
private fun Item(icon: androidx.compose.ui.graphics.vector.ImageVector, title: String, subtitle: String?, onClick: () -> Unit) {
    Row(Modifier.fillMaxWidth().clickable(onClick = onClick).padding(horizontal = 16.dp, vertical = 14.dp), verticalAlignment = Alignment.CenterVertically) {
        Icon(icon, null, tint = MaterialTheme.colorScheme.onSurfaceVariant)
        Spacer(Modifier.width(16.dp))
        Column(Modifier.weight(1f)) {
            Text(title, style = MaterialTheme.typography.bodyLarge)
            if (subtitle != null) Text(subtitle, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
        Icon(Icons.AutoMirrored.Outlined.KeyboardArrowRight, null, tint = MaterialTheme.colorScheme.outline)
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun UploadsScreen(onBack: () -> Unit, onOpen: (String) -> Unit) {
    val c = LocalContainer.current
    val items by c.uploads.items.collectAsStateWithLifecycle()
    Column(Modifier.fillMaxSize()) {
        TopAppBar(title = { Text("Uploads") }, navigationIcon = { BackButton(onBack) }, actions = {
            if (items.any { !it.active }) TextButton({ c.uploads.clearFinished() }) { Text("Clear finished") }
        })
        if (items.isEmpty()) EmptyState(Icons.Outlined.CloudUpload, "Nothing uploading", "Scans and shared files are sent from here. They keep going if the connection drops.")
        else LazyColumn(Modifier.fillMaxSize()) {
            items(items.reversed(), key = { it.id }) { u ->
                Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 10.dp)) {
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        val (icon, tint) = when (u.status) {
                            "done" -> Icons.Outlined.CheckCircle to MaterialTheme.colorScheme.primary
                            "error" -> Icons.Outlined.ErrorOutline to MaterialTheme.colorScheme.error
                            "duplicate" -> Icons.Outlined.ContentCopy to androidx.compose.ui.graphics.Color(0xFFB77900)
                            else -> Icons.Outlined.CloudUpload to MaterialTheme.colorScheme.onSurfaceVariant
                        }
                        Icon(icon, null, tint = tint)
                        Spacer(Modifier.width(12.dp))
                        Column(Modifier.weight(1f)) {
                            Text(u.title ?: u.name, maxLines = 1, overflow = TextOverflow.Ellipsis, style = MaterialTheme.typography.bodyLarge)
                            Text(
                                when (u.status) {
                                    "done" -> "Uploaded · reading the text…"
                                    "queued" -> if (u.ocr == "pending") "Reading the text on this phone…" else u.error ?: "Waiting · ${formatBytes(u.size)}"
                                    "uploading" -> "${(u.progress * 100).toInt()}% of ${formatBytes(u.size)}"
                                    "duplicate" -> "You already have this: ${u.duplicateTitle ?: "a copy"}"
                                    else -> u.error ?: "Failed"
                                },
                                style = MaterialTheme.typography.bodySmall, color = if (u.status == "error") MaterialTheme.colorScheme.error else MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 2,
                            )
                        }
                        when (u.status) {
                            "error" -> IconButton({ c.uploads.retry(u.id) }) { Icon(Icons.Outlined.Refresh, "Try again") }
                            "done" -> TextButton({ u.docId?.let(onOpen) }) { Text("Open") }
                        }
                        if (u.status != "done") IconButton({ c.uploads.remove(u.id) }) { Icon(Icons.Outlined.DeleteOutline, "Remove") }
                    }
                    if (u.status == "uploading") LinearProgressIndicator(progress = { u.progress }, Modifier.fillMaxWidth().padding(top = 8.dp))
                    if (u.status == "duplicate") Row(Modifier.padding(top = 6.dp, start = 36.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        u.duplicateOfId?.let { id -> OutlinedButton({ onOpen(id) }) { Text("Open existing") } }
                        TextButton({ c.uploads.retry(u.id, allowDuplicate = true) }) { Text("Upload anyway") }
                    }
                }
                HorizontalDivider()
            }
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun NotificationsScreen(onBack: () -> Unit, onOpenDoc: (String) -> Unit) {
    val c = LocalContainer.current
    var list by remember { mutableStateOf<List<Notification>?>(null) }
    var unread by remember { mutableStateOf(0) }
    var error by remember { mutableStateOf<String?>(null) }
    val scope = rememberCoroutineScope()
    suspend fun load() {
        try {
            val r = c.repo.notifications()
            list = r.items
            unread = r.unread
        } catch (e: Exception) {
            error = e.friendly()
        }
    }
    LaunchedEffect(Unit) { load() }
    Column(Modifier.fillMaxSize()) {
        TopAppBar(title = { Text("Notifications") }, navigationIcon = { BackButton(onBack) }, actions = {
            if (unread > 0) TextButton({ scope.launch { runCatching { c.repo.markNotificationsRead() }; load() } }) { Text("Mark all read") }
        })
        val l = list
        when {
            l == null && error != null -> ErrorState(error!!) { error = null; scope.launch { load() } }
            l == null -> LoadingBox()
            l.isEmpty() -> EmptyState(Icons.Outlined.Notifications, "You're all caught up")
            else -> LazyColumn(Modifier.fillMaxSize()) {
                items(l, key = { it.id }) { n ->
                    Row(Modifier.fillMaxWidth().clickable {
                        scope.launch {
                            if (n.readAt == null) runCatching { c.repo.markNotificationsRead(listOf(n.id)) }
                            Regex("/documents/([0-9a-fA-F-]{36})").find(n.link)?.let { onOpenDoc(it.groupValues[1]) }
                            load()
                        }
                    }.padding(horizontal = 16.dp, vertical = 12.dp), verticalAlignment = Alignment.Top) {
                        Box(Modifier.padding(top = 6.dp).size(8.dp).clip(CircleShape).let { if (n.readAt == null) it.background(MaterialTheme.colorScheme.primary) else it })
                        Spacer(Modifier.width(12.dp))
                        Column {
                            Text(n.title, style = MaterialTheme.typography.titleSmall)
                            if (n.body.isNotBlank()) Text(n.body, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 3)
                            Text(timeAgo(n.createdAt), style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.outline)
                        }
                    }
                    HorizontalDivider()
                }
            }
        }
    }
}
