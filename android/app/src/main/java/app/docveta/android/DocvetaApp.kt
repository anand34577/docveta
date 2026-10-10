package app.docveta.android

import android.app.Application
import app.docveta.android.data.AiStatus
import app.docveta.android.data.ApiClient
import app.docveta.android.data.Repository
import app.docveta.android.data.SecureStore
import app.docveta.android.data.SessionStore
import app.docveta.android.ui.AskStore
import app.docveta.android.upload.UploadManager
import coil.ImageLoader
import coil.ImageLoaderFactory
import coil.disk.DiskCache
import coil.memory.MemoryCache
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.launch

/** Hand-wired dependencies: there are few enough that a DI framework would only add weight. */
class AppContainer(app: Application) {
    val context: android.content.Context = app
    val cacheDir: java.io.File = app.cacheDir
    val session = SessionStore(SecureStore(app))

    /** Emits when the server says the saved sign-in no longer works (token revoked or expired). */
    val signedOut = MutableSharedFlow<Unit>(extraBufferCapacity = 1)

    val api = ApiClient(session, onUnauthorized = {
        if (session.signedIn) {
            session.signOut()
            signedOut.tryEmit(Unit)
        }
    })
    val repo = Repository(api, session, app.cacheDir, android.os.Build.MODEL ?: "Android phone")
    val uploads = UploadManager(app, api, session)

    /** For work that outlives a screen (refreshing what the server offers). */
    val appScope = kotlinx.coroutines.CoroutineScope(kotlinx.coroutines.SupervisorJob() + kotlinx.coroutines.Dispatchers.Main)

    /** The Ask chats: kept here so they outlive the Ask screen. */
    val ask = AskStore(repo, appScope)

    /** Whether the server has AI (Ask, meaning-based search, similar documents). */
    val ai = androidx.compose.runtime.mutableStateOf(AiStatus())

    fun refreshAi() {
        appScope.launch { ai.value = repo.aiStatus() }
    }

    /** Light, dark or follow the phone; the whole app recomposes when it changes. */
    val theme = androidx.compose.runtime.mutableStateOf(session.theme)

    fun setTheme(t: String) {
        session.theme = t
        theme.value = t
    }
}

class DocvetaApp : Application(), ImageLoaderFactory {
    lateinit var container: AppContainer
        private set

    override fun onCreate() {
        super.onCreate()
        container = AppContainer(this)
    }

    /** Thumbnails go through the same client as the API, so they carry the sign-in; they're cached on disk. */
    override fun newImageLoader(): ImageLoader = ImageLoader.Builder(this)
        .okHttpClient { container.api.imageClient }
        .memoryCache { MemoryCache.Builder(this).maxSizePercent(0.2).build() }
        .diskCache { DiskCache.Builder().directory(cacheDir.resolve("thumbs")).maxSizeBytes(80L * 1024 * 1024).build() }
        .crossfade(true)
        .build()
}
