package app.docveta.android.ui

import android.net.Uri
import androidx.compose.animation.AnimatedVisibility
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
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.AutoAwesome
import androidx.compose.material.icons.outlined.Description
import androidx.compose.material.icons.outlined.DocumentScanner
import androidx.compose.material.icons.outlined.Inbox
import androidx.compose.material.icons.outlined.Menu
import androidx.compose.material.icons.outlined.Notifications
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Badge
import androidx.compose.material3.BadgedBox
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.NavigationBar
import androidx.compose.material3.NavigationBarItem
import androidx.compose.material3.NavigationBarItemDefaults
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.produceState
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.navigation.NavHostController
import androidx.navigation.NavType
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.composable
import androidx.navigation.compose.currentBackStackEntryAsState
import androidx.navigation.compose.rememberNavController
import androidx.navigation.navArgument
import app.docveta.android.data.AiStatus
import app.docveta.android.data.Me
import app.docveta.android.data.Stats
import app.docveta.android.scan.CropScreen
import app.docveta.android.scan.ReviewScreen
import app.docveta.android.scan.ScanEntry
import app.docveta.android.scan.ScanSession
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch

val LocalMe = androidx.compose.runtime.compositionLocalOf<Me> { error("not signed in") }
val LocalStats = androidx.compose.runtime.compositionLocalOf<Stats?> { null }
val LocalAi = androidx.compose.runtime.compositionLocalOf<androidx.compose.runtime.State<AiStatus>> { mutableStateOf(AiStatus()) }

@Composable
fun produceAi(): androidx.compose.runtime.State<AiStatus> = LocalAi.current

private object Route {
    const val Inbox = "inbox"
    const val Docs = "docs"
    const val Ask = "ask"
    const val More = "more"
    const val Doc = "doc/{id}?page={page}"
    const val Scan = "scan"
    const val Review = "scan/review"
    const val Crop = "scan/crop/{pageId}"
    const val Uploads = "uploads"
    const val Notifications = "notifications"
    const val Settings = "settings"
    const val Trash = "trash"
    fun doc(id: String, page: Int = 0) = "doc/$id?page=$page"
}

/** Files handed over by Android's share sheet, waiting for the person to pick a space. */
class ShareInbox {
    var uris by mutableStateOf<List<Uri>>(emptyList())
}

@Composable
fun MainShell(shared: ShareInbox, onSignedOut: () -> Unit) {
    val c = LocalContainer.current
    val nav = rememberNavController()
    val scan = container("scan") { ScanSession(it) }
    var me by remember { mutableStateOf<Me?>(null) }
    var stats by remember { mutableStateOf<Stats?>(null) }
    var loadError by remember { mutableStateOf<String?>(null) }
    val ai = produceState(AiStatus()) { value = c.repo.aiStatus() }
    val scope = rememberCoroutineScope()

    LaunchedEffect(Unit) { c.signedOut.collect { onSignedOut() } }
    LaunchedEffect(Unit) {
        while (true) {
            try {
                me = c.repo.me()
                stats = c.repo.stats()
                loadError = null
            } catch (e: Exception) {
                if (me == null) loadError = e.friendly()
            }
            delay(30_000)
        }
    }

    val m = me
    if (m == null) {
        if (loadError != null) ErrorState(loadError!!) { loadError = null; scope.launch { runCatching { me = c.repo.me() }.onFailure { loadError = it.friendly() } } } else LoadingBox()
        return
    }

    CompositionLocalProvider(LocalMe provides m, LocalStats provides stats, LocalAi provides ai) {
        val entry by nav.currentBackStackEntryAsState()
        val route = entry?.destination?.route
        val inMain = route in setOf(Route.Inbox, Route.Docs, Route.Ask, Route.More)
        val uploads by c.uploads.items.collectAsStateWithLifecycle()
        val busy = uploads.count { it.active }

        Scaffold(bottomBar = {
            if (inMain) Column {
                AnimatedVisibility(busy > 0) {
                    Row(Modifier.fillMaxWidth().background(MaterialTheme.colorScheme.primaryContainer).clickable { nav.navigate(Route.Uploads) }.padding(horizontal = 16.dp, vertical = 8.dp), verticalAlignment = Alignment.CenterVertically) {
                        CircularProgressIndicator(Modifier.size(16.dp), strokeWidth = 2.dp)
                        Spacer(Modifier.width(10.dp))
                        Text("Uploading $busy file${if (busy == 1) "" else "s"}…", style = MaterialTheme.typography.labelLarge, color = MaterialTheme.colorScheme.onPrimaryContainer)
                    }
                }
                BottomBar(nav, route, stats?.inbox ?: 0, ai.value.chat)
            }
        }) { pad ->
            NavHost(nav, Route.Inbox, Modifier.padding(pad)) {
                composable(Route.Inbox) { InboxScreen(onOpen = { nav.navigate(Route.doc(it)) }, onScan = { nav.navigate(Route.Scan) }) }
                composable(Route.Docs) { DocumentsScreen(onOpen = { nav.navigate(Route.doc(it)) }, onScan = { nav.navigate(Route.Scan) }) }
                composable(Route.Ask) { AskScreen(onOpenDoc = { id, page -> nav.navigate(Route.doc(id, page)) }) }
                composable(Route.More) { MoreScreen(onNavigate = { nav.navigate(it) }, uploadsActive = busy, onSignedOut = onSignedOut) }
                composable(Route.Doc, arguments = listOf(navArgument("id") { type = NavType.StringType }, navArgument("page") { type = NavType.IntType; defaultValue = 0 })) {
                    DocumentScreen(it.arguments!!.getString("id")!!, startPage = (it.arguments!!.getInt("page") - 1).coerceAtLeast(0), onBack = { nav.popBackStack() })
                }
                composable(Route.Scan) { ScanEntry(scan, onDone = { nav.navigate(Route.Review) { popUpTo(Route.Scan) { inclusive = true } } }, onClose = { nav.popBackStack() }) }
                composable(Route.Review) {
                    ReviewScreen(scan, onAddMore = { nav.navigate(Route.Scan) }, onEdit = { id -> nav.navigate("scan/crop/$id") },
                        onUploaded = { nav.popBackStack(Route.Inbox, false); nav.navigate(Route.Uploads) }, onBack = { nav.popBackStack() })
                }
                composable(Route.Crop, arguments = listOf(navArgument("pageId") { type = NavType.StringType })) { CropScreen(scan, it.arguments!!.getString("pageId")!!, onBack = { nav.popBackStack() }) }
                composable(Route.Uploads) { UploadsScreen(onBack = { nav.popBackStack() }, onOpen = { nav.navigate(Route.doc(it)) }) }
                composable(Route.Notifications) { NotificationsScreen(onBack = { nav.popBackStack() }, onOpenDoc = { nav.navigate(Route.doc(it)) }) }
                composable(Route.Settings) { SettingsScreen(onBack = { nav.popBackStack() }, onSignedOut = onSignedOut) }
                composable(Route.Trash) { TrashScreen(onBack = { nav.popBackStack() }, onOpen = { nav.navigate(Route.doc(it)) }) }
            }
        }
        if (shared.uris.isNotEmpty()) ShareTargetDialog(shared.uris, onDone = { shared.uris = emptyList(); nav.navigate(Route.Uploads) }, onCancel = { shared.uris = emptyList() })
    }
}

@Composable
private fun BottomBar(nav: NavHostController, route: String?, inbox: Int, askAvailable: Boolean) {
    val colors = NavigationBarItemDefaults.colors(indicatorColor = MaterialTheme.colorScheme.primaryContainer)
    fun go(r: String) = nav.navigate(r) {
        popUpTo(Route.Inbox) { saveState = true }
        launchSingleTop = true
        restoreState = true
    }
    NavigationBar {
        NavigationBarItem(route == Route.Inbox, { go(Route.Inbox) }, colors = colors, label = { Text("Inbox") }, icon = {
            BadgedBox(badge = { if (inbox > 0) Badge { Text(if (inbox > 99) "99+" else inbox.toString()) } }) { Icon(Icons.Outlined.Inbox, null) }
        })
        NavigationBarItem(route == Route.Docs, { go(Route.Docs) }, colors = colors, label = { Text("Documents") }, icon = { Icon(Icons.Outlined.Description, null) })
        NavigationBarItem(false, { nav.navigate(Route.Scan) }, colors = colors, label = { Text("Scan") }, icon = {
            Box(Modifier.size(width = 52.dp, height = 32.dp).background(MaterialTheme.colorScheme.primary, androidx.compose.foundation.shape.RoundedCornerShape(16.dp)), contentAlignment = Alignment.Center) {
                Icon(Icons.Outlined.DocumentScanner, null, tint = MaterialTheme.colorScheme.onPrimary)
            }
        })
        if (askAvailable) NavigationBarItem(route == Route.Ask, { go(Route.Ask) }, colors = colors, label = { Text("Ask") }, icon = { Icon(Icons.Outlined.AutoAwesome, null) })
        else NavigationBarItem(false, { nav.navigate(Route.Notifications) }, colors = colors, label = { Text("Alerts") }, icon = { Icon(Icons.Outlined.Notifications, null) })
        NavigationBarItem(route == Route.More, { go(Route.More) }, colors = colors, label = { Text("More") }, icon = { Icon(Icons.Outlined.Menu, null) })
    }
}

/** "Share to Docveta" from another app: choose a space and the files join the upload queue. */
@Composable
private fun ShareTargetDialog(uris: List<Uri>, onDone: () -> Unit, onCancel: () -> Unit) {
    val c = LocalContainer.current
    val me = LocalMe.current
    val writable = me.spaces.filter { it.canWrite }
    var space by remember { mutableStateOf(writable.firstOrNull { it.id == c.session.defaultSpaceId } ?: writable.firstOrNull { it.isPersonal } ?: writable.firstOrNull()) }
    var busy by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()
    AlertDialog(
        onDismissRequest = onCancel,
        title = { Text("Add ${uris.size} file${if (uris.size == 1) "" else "s"} to Docveta") },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
                if (writable.isEmpty()) Text("You can't add documents to any space.")
                writable.forEach { s ->
                    Row(Modifier.fillMaxWidth().clickable { space = s }.padding(vertical = 8.dp), verticalAlignment = Alignment.CenterVertically) {
                        androidx.compose.material3.RadioButton(space?.id == s.id, { space = s })
                        SpaceDot(s)
                        Spacer(Modifier.width(8.dp))
                        Text(s.label)
                    }
                }
            }
        },
        confirmButton = {
            Button({
                busy = true
                scope.launch {
                    c.session.defaultSpaceId = space?.id
                    uris.forEach { runCatching { c.uploads.enqueue(it, space?.id, "share") } }
                    onDone()
                }
            }, enabled = space != null && !busy) { Text("Upload") }
        },
        dismissButton = { TextButton(onCancel) { Text("Cancel") } },
    )
}
