package app.docveta.android

import android.content.Intent
import android.content.pm.PackageManager
import android.net.Uri
import android.os.Build
import android.os.Bundle
import androidx.activity.compose.setContent
import androidx.activity.result.contract.ActivityResultContracts
import androidx.activity.enableEdgeToEdge
import androidx.biometric.BiometricManager
import androidx.biometric.BiometricPrompt
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Lock
import androidx.compose.material3.Button
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import androidx.core.content.ContextCompat
import androidx.fragment.app.FragmentActivity
import app.docveta.android.ui.AuthScreen
import app.docveta.android.ui.DocvetaTheme
import app.docveta.android.ui.LocalContainer
import app.docveta.android.ui.MainShell
import app.docveta.android.ui.ShareInbox
import app.docveta.android.ui.SsoInbox

private const val LOCAL_NETWORK = "android.permission.ACCESS_LOCAL_NETWORK"

class MainActivity : FragmentActivity() {
    private val shared = ShareInbox()
    private val sso = SsoInbox()
    private var locked by mutableStateOf(false)
    private var leftAt = 0L
    private lateinit var container: AppContainer
    private val askLocalNetwork = registerForActivityResult(ActivityResultContracts.RequestPermission()) {}

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        container = (application as DocvetaApp).container
        // Android 17 blocks connections to local-network addresses (192.168.x.x, 10.x.x.x, .local)
        // unless the person allows it; without this a server at home can't be reached.
        if (Build.VERSION.SDK_INT >= 37 && checkSelfPermission(LOCAL_NETWORK) != PackageManager.PERMISSION_GRANTED) askLocalNetwork.launch(LOCAL_NETWORK)
        locked = container.session.appLock && container.session.signedIn
        handle(intent)
        setContent {
            val mode by container.theme
            DocvetaTheme(dark = when (mode) { "dark" -> true; "light" -> false; else -> androidx.compose.foundation.isSystemInDarkTheme() }) {
                CompositionLocalProvider(LocalContainer provides container) {
                    var signedIn by remember { mutableStateOf(container.session.signedIn) }
                    Surface(Modifier.fillMaxSize(), color = MaterialTheme.colorScheme.background) {
                        when {
                            !signedIn -> AuthScreen(sso, onSignedIn = { signedIn = true; container.uploads.schedule() })
                            locked -> LockScreen(onUnlock = ::promptUnlock)
                            else -> MainShell(shared, onSignedOut = { container.session.signOut(); signedIn = false })
                        }
                    }
                }
            }
        }
        if (locked) window.decorView.post { promptUnlock() }
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        handle(intent)
    }

    override fun onStop() {
        super.onStop()
        leftAt = System.currentTimeMillis()
    }

    override fun onResume() {
        super.onResume()
        // With the app lock on, the recent-apps screen must not show the document that was open.
        // (Android 13+; screenshots you take yourself still work.)
        if (Build.VERSION.SDK_INT >= 33) setRecentsScreenshotEnabled(!container.session.appLock)
    }

    override fun onStart() {
        super.onStart()
        // Coming back after a while: ask again if the person turned on the lock.
        if (container.session.appLock && container.session.signedIn && leftAt != 0L && System.currentTimeMillis() - leftAt > 30_000) {
            locked = true
            promptUnlock()
        }
    }

    /** Picks up files shared from other apps ("Share, Docveta") and single sign-on results. */
    private fun handle(i: Intent?) {
        i ?: return
        // Back from single sign-on in the browser: docveta://sso?code=… (or ?error=…).
        val data = i.data
        if (i.action == Intent.ACTION_VIEW && data?.scheme == "docveta" && data.host == "sso") {
            sso.code = data.getQueryParameter("code")
            sso.error = data.getQueryParameter("error")
            return
        }
        val uris = when (i.action) {
            Intent.ACTION_SEND -> listOfNotNull(streamOf(i))
            Intent.ACTION_SEND_MULTIPLE -> streamsOf(i)
            else -> emptyList()
        }
        if (uris.isNotEmpty() && container.session.signedIn) shared.uris = uris
    }

    @Suppress("DEPRECATION")
    private fun streamOf(i: Intent): Uri? = if (Build.VERSION.SDK_INT >= 33) i.getParcelableExtra(Intent.EXTRA_STREAM, Uri::class.java) else i.getParcelableExtra(Intent.EXTRA_STREAM)

    @Suppress("DEPRECATION")
    private fun streamsOf(i: Intent): List<Uri> = (if (Build.VERSION.SDK_INT >= 33) i.getParcelableArrayListExtra(Intent.EXTRA_STREAM, Uri::class.java) else i.getParcelableArrayListExtra<Uri>(Intent.EXTRA_STREAM)).orEmpty()

    private fun promptUnlock() {
        val auth = BiometricManager.Authenticators.BIOMETRIC_WEAK or BiometricManager.Authenticators.DEVICE_CREDENTIAL
        if (BiometricManager.from(this).canAuthenticate(auth) != BiometricManager.BIOMETRIC_SUCCESS) {
            // No screen lock set up on the phone: nothing to ask, so the lock can't protect anything.
            locked = false
            return
        }
        val prompt = BiometricPrompt(this, ContextCompat.getMainExecutor(this), object : BiometricPrompt.AuthenticationCallback() {
            override fun onAuthenticationSucceeded(result: BiometricPrompt.AuthenticationResult) {
                locked = false
            }
        })
        prompt.authenticate(BiometricPrompt.PromptInfo.Builder().setTitle("Unlock Docveta").setAllowedAuthenticators(auth).build())
    }
}

@androidx.compose.runtime.Composable
private fun LockScreen(onUnlock: () -> Unit) {
    Column(Modifier.fillMaxSize().background(MaterialTheme.colorScheme.background).padding(32.dp), verticalArrangement = Arrangement.Center, horizontalAlignment = Alignment.CenterHorizontally) {
        Icon(Icons.Outlined.Lock, null, tint = MaterialTheme.colorScheme.primary, modifier = Modifier.padding(bottom = 16.dp))
        Text("Docveta is locked", style = MaterialTheme.typography.titleLarge)
        Text("Use your fingerprint, face or screen lock to continue.", style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(vertical = 8.dp))
        Button(onUnlock) { Text("Unlock") }
    }
}
