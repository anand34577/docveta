package app.docveta.android

import android.app.Application
import app.docveta.android.data.ApiClient
import app.docveta.android.data.Repository
import app.docveta.android.data.SecureStore
import app.docveta.android.data.SessionStore
import app.docveta.android.upload.UploadManager
import coil.ImageLoader
import coil.ImageLoaderFactory
import coil.disk.DiskCache
import coil.memory.MemoryCache
import kotlinx.coroutines.flow.MutableSharedFlow

/** Hand-wired dependencies: there are few enough that a DI framework would only add weight. */
class AppContainer(app: Application) {
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
