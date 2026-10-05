package app.docveta.android.data

import android.content.Context
import android.content.SharedPreferences
import androidx.security.crypto.EncryptedSharedPreferences
import androidx.security.crypto.MasterKey

/** Where the app remembers the server and sign-in. Behind an interface so tests can use memory. */
interface KeyValueStore {
    fun get(key: String): String?
    fun put(key: String, value: String?)
}

class MemoryStore : KeyValueStore {
    private val map = HashMap<String, String>()
    override fun get(key: String) = map[key]
    override fun put(key: String, value: String?) {
        if (value == null) map.remove(key) else map[key] = value
    }
}

/** Android Keystore-backed storage: the access token never sits on disk in the clear. */
class SecureStore(context: Context) : KeyValueStore {
    private val prefs: SharedPreferences = try {
        val key = MasterKey.Builder(context).setKeyScheme(MasterKey.KeyScheme.AES256_GCM).build()
        EncryptedSharedPreferences.create(context, "docveta_secure", key, EncryptedSharedPreferences.PrefKeyEncryptionScheme.AES256_SIV, EncryptedSharedPreferences.PrefValueEncryptionScheme.AES256_GCM)
    } catch (e: Exception) {
        // A broken Keystore (some restored backups, a few OEM bugs): start fresh rather than crash. The person signs in again.
        context.deleteSharedPreferences("docveta_secure")
        context.getSharedPreferences("docveta_fallback", Context.MODE_PRIVATE).also { it.edit().clear().apply() }
    }

    override fun get(key: String): String? = prefs.getString(key, null)
    override fun put(key: String, value: String?) {
        prefs.edit().apply { if (value == null) remove(key) else putString(key, value) }.apply()
    }
}

class SessionStore(private val kv: KeyValueStore) {
    var serverUrl: String?
        get() = kv.get("server")
        set(v) = kv.put("server", v)

    var token: String?
        get() = kv.get("token")
        set(v) = kv.put("token", v)

    var tokenId: String?
        get() = kv.get("token_id")
        set(v) = kv.put("token_id", v)

    /** The permissions of the stored token, comma-separated (null for sign-ins from older versions). */
    var tokenScopes: String?
        get() = kv.get("token_scopes")
        set(v) = kv.put("token_scopes", v)

    var userName: String?
        get() = kv.get("user_name")
        set(v) = kv.put("user_name", v)

    var userEmail: String?
        get() = kv.get("user_email")
        set(v) = kv.put("user_email", v)

    /** Single sign-on in the browser: the server it's for and the PKCE secret, until the app link comes back. */
    var pendingSsoServer: String?
        get() = kv.get("sso_server")
        set(v) = kv.put("sso_server", v)

    var pendingSsoVerifier: String?
        get() = kv.get("sso_verifier")
        set(v) = kv.put("sso_verifier", v)

    var appLock: Boolean
        get() = kv.get("app_lock") == "1"
        set(v) = kv.put("app_lock", if (v) "1" else null)

    var wifiOnlyUploads: Boolean
        get() = kv.get("wifi_only") == "1"
        set(v) = kv.put("wifi_only", if (v) "1" else null)

    var defaultSpaceId: String?
        get() = kv.get("default_space")
        set(v) = kv.put("default_space", v)

    var scanFilter: String?
        get() = kv.get("scan_filter")
        set(v) = kv.put("scan_filter", v)

    /** system | light | dark, mirrored from the account so the sign-in screen matches too. */
    var theme: String
        get() = kv.get("theme") ?: "system"
        set(v) = kv.put("theme", v)

    var autoCapture: Boolean
        get() = kv.get("auto_capture") != "0"
        set(v) = kv.put("auto_capture", if (v) null else "0")

    /** Read scans' text on the phone: auto (when the server can't) | on | off. */
    var phoneOcr: String
        get() = kv.get("phone_ocr") ?: "auto"
        set(v) = kv.put("phone_ocr", v)

    /** Whether the server could read text from pictures, as of the last time it answered. */
    var serverReadsText: Boolean
        get() = kv.get("server_ocr") != "0"
        set(v) = kv.put("server_ocr", if (v) null else "0")

    /** The account as last loaded (JSON), so the app opens, and scans, while the server can't be reached. */
    var cachedMe: String?
        get() = kv.get("me")
        set(v) = kv.put("me", v)

    val signedIn: Boolean get() = !token.isNullOrBlank() && !serverUrl.isNullOrBlank()

    fun signOut() {
        token = null
        tokenId = null
        tokenScopes = null
        userName = null
        userEmail = null
        cachedMe = null
    }
}
