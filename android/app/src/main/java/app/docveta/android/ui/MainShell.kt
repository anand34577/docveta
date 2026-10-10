package app.docveta.android.ui

import android.net.Uri
import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.EnterTransition
import androidx.compose.animation.ExitTransition
import androidx.compose.animation.core.CubicBezierEasing
import androidx.compose.animation.core.tween
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.scaleIn
import androidx.compose.animation.slideInHorizontally
import androidx.compose.animation.slideInVertically
import androidx.compose.animation.slideOutHorizontally
import androidx.compose.animation.slideOutVertically
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
import androidx.compose.material.icons.outlined.CloudOff
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
import androidx.compose.material3.FilledTonalButton
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
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.repeatOnLifecycle
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
import app.docveta.android.scan.CameraScreen
import app.docveta.android.scan.CropScreen
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.statusBarsPadding
import app.docveta.android.scan.ReviewScreen
import app.docveta.android.scan.ScanSession
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch

val LocalMe = androidx.compose.runtime.compositionLocalOf<Me> { error("not signed in") }
val LocalStats = androidx.compose.runtime.compositionLocalOf<Stats?> { null }
val LocalAi = androidx.compose.runtime.compositionLocalOf<androidx.compose.runtime.State<AiStatus>> { mutableStateOf(AiStatus()) }

/** Fetch the account again (after changing spaces, profile or members) so every screen sees it. */
val LocalReloadMe = androidx.compose.runtime.compositionLocalOf<() -> Unit> { {} }

/** Pick files on the phone and add them to the upload queue (after choosing a space). */
val LocalPickFiles = androidx.compose.runtime.compositionLocalOf<() -> Unit> { {} }

@Composable
fun produceAi(): androidx.compose.runtime.State<AiStatus> = LocalAi.current

private object Route {
    const val Inbox = "inbox"
    const val Docs = "docs"
    const val Ask = "ask"
    const val More = "more"
    const val Doc = "doc/{id}?page={page}"
    const val Pages = "doc/{id}/pages"
    const val AskDoc = "ask/doc/{id}"
    const val Scan = "scan"
    const val Review = "scan/review"
    const val Crop = "scan/crop/{pageId}"
    const val Uploads = "uploads"
    const val Notifications = "notifications"
    const val Settings = "settings"
    const val SettingsSection = "settings/{section}"
    const val Trash = "trash"
    const val View = "view/{id}"
    const val Spaces = "spaces"
    const val Space = "space/{id}"
    const val SpaceSection = "space/{id}/{section}"
    const val Workflow = "space/{id}/workflow/{wid}"
    const val Admin = "admin"
    const val AdminSection = "admin/{section}"
    fun doc(id: String, page: Int = 0) = "doc/$id?page=$page"
}

/*
 * Screen transitions, following Material motion:
 * - between bottom-bar tabs: fade through (siblings, no direction);
 * - opening a screen: shared axis X (slides in from the side, the previous one drifts back);
 *   going back plays it in reverse and follows the predictive back gesture;
 * - the scanner (camera, review, crop): slides up like a sheet, down when closed.
 */
private val tabRoutes = setOf(Route.Inbox, Route.Docs, Route.Ask, Route.More)
private fun isScanner(route: String?) = route?.startsWith("scan") == true
private const val MOTION_MS = 300
private const val OUT_MS = MOTION_MS * 35 / 100 // the old screen is gone in the first third…
private val emphasized = CubicBezierEasing(0.2f, 0f, 0f, 1f)
private fun <T> motion() = tween<T>(MOTION_MS, easing = emphasized)
private fun fadeInLater() = fadeIn(tween(MOTION_MS - OUT_MS, delayMillis = OUT_MS)) // …before the new one appears
private fun fadeOutFast() = fadeOut(tween(OUT_MS))
private fun shift(width: Int) = width / 10 // both screens move a little (Material's ~30 dp)

private fun screenEnter(from: String?, to: String?): EnterTransition = when {
    from in tabRoutes && to in tabRoutes -> fadeIn(tween(210, delayMillis = 90)) + scaleIn(tween(210, delayMillis = 90), initialScale = 0.96f)
    isScanner(to) && !isScanner(from) -> slideInVertically(motion()) { it / 3 } + fadeInLater()
    else -> slideInHorizontally(motion()) { shift(it) } + fadeInLater()
}

private fun screenExit(from: String?, to: String?): ExitTransition = when {
    from in tabRoutes && to in tabRoutes -> fadeOut(tween(90))
    isScanner(to) && !isScanner(from) -> fadeOutFast()
    else -> slideOutHorizontally(motion()) { -shift(it) } + fadeOutFast()
}

private fun screenPopEnter(from: String?, to: String?): EnterTransition = when {
    from in tabRoutes && to in tabRoutes -> fadeIn(tween(210, delayMillis = 90))
    isScanner(from) && !isScanner(to) -> fadeInLater()
    else -> slideInHorizontally(motion()) { -shift(it) } + fadeInLater()
}

private fun screenPopExit(from: String?, to: String?): ExitTransition = when {
    from in tabRoutes && to in tabRoutes -> fadeOut(tween(90))
    isScanner(from) && !isScanner(to) -> slideOutVertically(motion()) { it / 3 } + fadeOutFast()
    else -> slideOutHorizontally(motion()) { shift(it) } + fadeOutFast()
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
    // Start from the account as last seen, so the app opens (and scans) without the server.
    var me by remember { mutableStateOf(c.repo.cachedMe()) }
    var stats by remember { mutableStateOf<Stats?>(null) }
    var loadError by remember { mutableStateOf<String?>(null) }
    var offline by remember { mutableStateOf(false) }
    val ai = c.ai
    LaunchedEffect(Unit) { c.refreshAi() }
    val scope = rememberCoroutineScope()
    val reloadMe: () -> Unit = { scope.launch { runCatching { me = c.repo.me() } } }
    var picked by remember { mutableStateOf<List<Uri>>(emptyList()) }
    val pickFiles = androidx.activity.compose.rememberLauncherForActivityResult(androidx.activity.result.contract.ActivityResultContracts.OpenMultipleDocuments()) { picked = it }

    LaunchedEffect(Unit) { c.signedOut.collect { onSignedOut() } }
    // Only while the app is on screen: in the background Android cuts the network (Doze), so a
    // check there fails and the bar greeted the person on return. Coming back checks at once.
    val lifecycle = LocalLifecycleOwner.current.lifecycle
    LaunchedEffect(lifecycle) {
        lifecycle.repeatOnLifecycle(Lifecycle.State.STARTED) {
            var failures = 0
            while (true) {
                try {
                    me = c.repo.me()
                    stats = c.repo.stats().also { c.session.serverReadsText = it.ocrAvailable }
                    loadError = null
                    if (offline) c.uploads.schedule() // back online: send what waited
                    offline = false
                    failures = 0
                } catch (e: kotlinx.coroutines.CancellationException) {
                    throw e
                } catch (e: Exception) {
                    val a = e as? app.docveta.android.data.ApiException
                    // A proxy answering for a server that's down is as unreachable as no network.
                    val unreachable = a != null && (a.isNetwork || a.status in 502..504)
                    // One failed check is often just a Wi-Fi/mobile handover: say so on the second.
                    failures = if (unreachable) failures + 1 else 0
                    offline = failures >= 2 || (unreachable && me == null)
                    if (me == null) loadError = e.friendly()
                }
                delay(if (failures > 0) 10_000 else 30_000)
            }
        }
    }

    val m = me
    if (m == null) {
        if (loadError != null) {
            // Can't reach the server at start-up: retry, or get out (the server may have moved).
            Column(Modifier.fillMaxSize(), verticalArrangement = Arrangement.Center, horizontalAlignment = Alignment.CenterHorizontally) {
                Box(Modifier.weight(1f, fill = false)) {
                    ErrorState(loadError!!) { loadError = null; scope.launch { runCatching { me = c.repo.me() }.onFailure { loadError = it.friendly() } } }
                }
                // Scanning doesn't need the server: scans wait in the upload queue until it's back.
                if (offline) FilledTonalButton({
                    me = Me(id = "", email = c.session.userEmail.orEmpty(), displayName = c.session.userName.orEmpty())
                }) { Text("Scan now, upload later") }
                c.session.serverUrl?.let { Text(it.removePrefix("https://").removePrefix("http://"), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant) }
                TextButton({ scope.launch { runCatching { c.repo.signOut() }; onSignedOut() } }, Modifier.navigationBarsPadding().padding(bottom = 24.dp)) {
                    Text("Sign out and change server")
                }
            }
        } else LoadingBox()
        return
    }

    CompositionLocalProvider(LocalMe provides m, LocalStats provides stats, LocalAi provides ai, LocalReloadMe provides reloadMe, LocalPickFiles provides { pickFiles.launch(arrayOf("*/*")) }) {
        val entry by nav.currentBackStackEntryAsState()
        val route = entry?.destination?.route
        val inMain = route in setOf(Route.Inbox, Route.Docs, Route.Ask, Route.More)
        val uploads by c.uploads.items.collectAsStateWithLifecycle()
        val busy = uploads.count { it.active }

        Scaffold(contentWindowInsets = androidx.compose.foundation.layout.WindowInsets(0, 0, 0, 0), bottomBar = {
            if (inMain) Column {
                AnimatedVisibility(offline) {
                    Row(Modifier.fillMaxWidth().background(MaterialTheme.colorScheme.surfaceContainerHighest).padding(horizontal = 16.dp, vertical = 8.dp), verticalAlignment = Alignment.CenterVertically) {
                        Icon(Icons.Outlined.CloudOff, null, Modifier.size(16.dp), tint = MaterialTheme.colorScheme.onSurfaceVariant)
                        Spacer(Modifier.width(10.dp))
                        Text("Can't reach the server. You can still scan; uploads wait until it's back.", style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                }
                AnimatedVisibility(busy > 0) {
                    Row(Modifier.fillMaxWidth().background(MaterialTheme.colorScheme.primaryContainer).clickable { nav.navigate(Route.Uploads) }.padding(horizontal = 16.dp, vertical = 8.dp), verticalAlignment = Alignment.CenterVertically) {
                        if (!offline) CircularProgressIndicator(Modifier.size(16.dp), strokeWidth = 2.dp)
                        else Icon(Icons.Outlined.CloudOff, null, Modifier.size(16.dp))
                        Spacer(Modifier.width(10.dp))
                        Text(if (offline) "$busy file${if (busy == 1) "" else "s"} waiting to upload" else "Uploading $busy file${if (busy == 1) "" else "s"}…", style = MaterialTheme.typography.labelLarge, color = MaterialTheme.colorScheme.onPrimaryContainer)
                    }
                }
                BottomBar(nav, route, stats?.inbox ?: 0, ai.value.chat)
            }
        }) { pad ->
            NavHost(
                nav, Route.Docs, Modifier.padding(pad),
                enterTransition = { screenEnter(initialState.destination.route, targetState.destination.route) },
                exitTransition = { screenExit(initialState.destination.route, targetState.destination.route) },
                popEnterTransition = { screenPopEnter(initialState.destination.route, targetState.destination.route) },
                popExitTransition = { screenPopExit(initialState.destination.route, targetState.destination.route) },
            ) {
                val back: () -> Unit = { nav.popBackStack() }
                val open: (String) -> Unit = { nav.navigate(it) }
                val openDoc: (String) -> Unit = { nav.navigate(Route.doc(it)) }
                composable(Route.Inbox) { Box(Modifier.fillMaxSize().statusBarsPadding()) { InboxScreen(onOpen = openDoc, onScan = { nav.navigate(Route.Scan) }) } }
                composable(Route.Docs) { Box(Modifier.fillMaxSize().statusBarsPadding()) { DocumentsScreen(onOpen = openDoc, onScan = { nav.navigate(Route.Scan) }, onOpenView = { nav.navigate("view/$it") }) } }
                composable(Route.Ask) { Box(Modifier.fillMaxSize().statusBarsPadding()) { AskScreen(onOpenDoc = { id, page -> nav.navigate(Route.doc(id, page)) }) } }
                composable(Route.AskDoc, arguments = listOf(navArgument("id") { type = NavType.StringType })) {
                    Box(Modifier.fillMaxSize().statusBarsPadding()) { AskScreen(onOpenDoc = { id, page -> nav.navigate(Route.doc(id, page)) }, documentId = it.arguments!!.getString("id"), onBack = back) }
                }
                composable(Route.More) { Box(Modifier.fillMaxSize().statusBarsPadding()) { MoreScreen(onNavigate = open, uploadsActive = busy) } }
                composable(Route.Doc, arguments = listOf(navArgument("id") { type = NavType.StringType }, navArgument("page") { type = NavType.IntType; defaultValue = 0 })) {
                    val id = it.arguments!!.getString("id")!!
                    DocumentScreen(id, startPage = (it.arguments!!.getInt("page") - 1).coerceAtLeast(0), onBack = back, onOpenDoc = openDoc, onNavigate = open)
                }
                composable(Route.Pages, arguments = listOf(navArgument("id") { type = NavType.StringType })) { PageManagerScreen(it.arguments!!.getString("id")!!, onBack = back) }
                composable(Route.Scan) { CameraScreen(scan, onDone = { nav.navigate(Route.Review) { popUpTo(Route.Scan) { inclusive = true } } }, onEdit = { id -> nav.navigate("scan/crop/$id") }, onClose = back) }
                composable(Route.Review) {
                    ReviewScreen(scan, onAddMore = { nav.navigate(Route.Scan) }, onEdit = { id -> nav.navigate("scan/crop/$id") },
                        onUploaded = { nav.popBackStack(Route.Docs, false); nav.navigate(Route.Uploads) }, onBack = back)
                }
                composable(Route.Crop, arguments = listOf(navArgument("pageId") { type = NavType.StringType })) { CropScreen(scan, it.arguments!!.getString("pageId")!!, onBack = back) }
                composable(Route.Uploads) { Box(Modifier.fillMaxSize().navigationBarsPadding()) { UploadsScreen(onBack = back, onOpen = openDoc) } }
                composable(Route.Notifications) { Box(Modifier.fillMaxSize().navigationBarsPadding()) { NotificationsScreen(onBack = back, onOpenDoc = openDoc) } }
                composable(Route.Trash) { Box(Modifier.fillMaxSize().navigationBarsPadding()) { TrashScreen(onBack = back, onOpen = openDoc) } }
                composable(Route.View, arguments = listOf(navArgument("id") { type = NavType.StringType })) { SavedViewScreen(it.arguments!!.getString("id")!!, onBack = back, onOpen = openDoc) }
                composable(Route.Settings) { SettingsScreen(onBack = back, onNavigate = open, onSignedOut = onSignedOut) }
                composable(Route.SettingsSection, arguments = listOf(navArgument("section") { type = NavType.StringType })) {
                    when (it.arguments!!.getString("section")) {
                        "profile" -> ProfileScreen(back)
                        "security" -> SecurityScreen(back)
                        "notifications" -> NotificationSettingsScreen(back)
                        "views" -> SavedViewsScreen(back, onOpen = { id -> nav.navigate("view/$id") })
                        "tokens" -> TokensScreen(back, onSignedOut)
                        else -> PhoneSettingsScreen(back)
                    }
                }
                composable(Route.Spaces) { SpacesScreen(back, onOpen = { nav.navigate("space/$it") }) }
                composable(Route.Space, arguments = listOf(navArgument("id") { type = NavType.StringType })) { SpaceScreen(it.arguments!!.getString("id")!!, back, open) }
                composable(Route.SpaceSection, arguments = listOf(navArgument("id") { type = NavType.StringType }, navArgument("section") { type = NavType.StringType })) {
                    val id = it.arguments!!.getString("id")!!
                    val gone: () -> Unit = { if (!nav.popBackStack(Route.Spaces, false)) nav.popBackStack(Route.More, false) }
                    when (val section = it.arguments!!.getString("section")!!) {
                        "general" -> SpaceGeneralScreen(id, back, onGone = gone)
                        "members" -> SpaceMembersScreen(id, back)
                        "tags", "correspondents", "document-types" -> VocabularyScreen(id, section, back)
                        "fields" -> FieldsScreen(id, back)
                        "workflows" -> WorkflowsScreen(id, back, onEdit = { wid -> nav.navigate("space/$id/workflow/$wid") })
                        "ai" -> SpaceAiScreen(id, back)
                        else -> SpaceScanningScreen(id, back)
                    }
                }
                composable(Route.Workflow, arguments = listOf(navArgument("id") { type = NavType.StringType }, navArgument("wid") { type = NavType.StringType })) {
                    val wid = it.arguments!!.getString("wid")!!
                    WorkflowEditScreen(it.arguments!!.getString("id")!!, wid.takeIf { w -> w != "new" }, back)
                }
                composable(Route.Admin) { AdminScreen(back, open) }
                composable(Route.AdminSection, arguments = listOf(navArgument("section") { type = NavType.StringType })) {
                    when (it.arguments!!.getString("section")) {
                        "users" -> AdminUsersScreen(back)
                        "processing" -> AdminProcessingScreen(back, onOpenDoc = openDoc)
                        "ai" -> AdminAiScreen(back)
                        "folders" -> AdminFoldersScreen(back)
                        "office" -> AdminOfficeScreen(back)
                        "sso" -> AdminSsoScreen(back)
                        "email" -> AdminEmailScreen(back)
                        "alerts" -> AdminAlertsScreen(back)
                        "export" -> AdminExportScreen(back)
                        "system" -> AdminSystemScreen(back)
                        else -> AdminAuditScreen(back)
                    }
                }
            }
        }
        if (shared.uris.isNotEmpty()) ShareTargetDialog(shared.uris, onDone = { shared.uris = emptyList(); nav.navigate(Route.Uploads) }, onCancel = { shared.uris = emptyList() })
        if (picked.isNotEmpty()) ShareTargetDialog(picked, onDone = { picked = emptyList(); nav.navigate(Route.Uploads) }, onCancel = { picked = emptyList() })
    }
}

@Composable
private fun BottomBar(nav: NavHostController, route: String?, inbox: Int, askAvailable: Boolean) {
    val colors = NavigationBarItemDefaults.colors(indicatorColor = MaterialTheme.colorScheme.primaryContainer)
    fun go(r: String) = nav.navigate(r) {
        popUpTo(Route.Docs) { saveState = true } // Documents is home: Back from a tab returns there
        launchSingleTop = true
        restoreState = true
    }
    NavigationBar {
        NavigationBarItem(route == Route.Docs, { go(Route.Docs) }, colors = colors, label = { Text("Documents") }, icon = { Icon(Icons.Outlined.Description, null) })
        NavigationBarItem(route == Route.Inbox, { go(Route.Inbox) }, colors = colors, label = { Text("Inbox") }, icon = {
            BadgedBox(badge = { if (inbox > 0) Badge { Text(if (inbox > 99) "99+" else inbox.toString()) } }) { Icon(Icons.Outlined.Inbox, null) }
        })
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
