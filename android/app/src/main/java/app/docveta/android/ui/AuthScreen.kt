package app.docveta.android.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Description
import androidx.compose.material.icons.outlined.Visibility
import androidx.compose.material.icons.outlined.VisibilityOff
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import android.content.ActivityNotFoundException
import android.content.Intent
import android.net.Uri
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
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.input.VisualTransformation
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import app.docveta.android.data.ServerStatus
import app.docveta.android.data.SignIn
import app.docveta.android.data.normalizeServerUrl
import kotlinx.coroutines.launch

private enum class Step { Server, Login, Code, Token }

/** The result of single sign-on, handed over by MainActivity when the browser opens docveta://sso. */
class SsoInbox {
    var code by mutableStateOf<String?>(null)
    var error by mutableStateOf<String?>(null)
}

/** First run: which server, then who you are. Ends with an access token that keeps the phone signed in. */
@Composable
fun AuthScreen(sso: SsoInbox, onSignedIn: () -> Unit) {
    val c = LocalContainer.current
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    var step by remember { mutableStateOf(Step.Server) }
    var server by remember { mutableStateOf(c.session.serverUrl.orEmpty()) }
    var base by remember { mutableStateOf("") }
    var status by remember { mutableStateOf<ServerStatus?>(null) }
    var email by remember { mutableStateOf(c.session.userEmail.orEmpty()) }
    var password by remember { mutableStateOf("") }
    var show by remember { mutableStateOf(false) }
    var code by remember { mutableStateOf("") }
    var challenge by remember { mutableStateOf("") }
    var token by remember { mutableStateOf("") }
    var busy by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }

    fun run(block: suspend () -> Unit) {
        busy = true
        error = null
        scope.launch {
            try {
                block()
            } catch (e: Exception) {
                error = e.friendly()
            } finally {
                busy = false
            }
        }
    }

    fun checkServer() = run {
        val url = normalizeServerUrl(server) ?: throw IllegalArgumentException("Enter the address of your Docveta, like docs.example.com")
        base = url
        status = c.repo.checkServer(url)
        if (status!!.setupNeeded) error = "This server hasn't been set up yet. Open it in a browser first to create the administrator."
        else step = if (status!!.passwordLogin || status!!.oidc.enabled) Step.Login else Step.Token
    }

    fun startSso() {
        error = null
        try {
            context.startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(c.repo.ssoStartUrl(base))))
        } catch (e: ActivityNotFoundException) {
            error = "No web browser is installed to sign in with."
        }
    }

    // Back from the browser with a sign-in code (or an error).
    LaunchedEffect(sso.code, sso.error) {
        val got = sso.code
        sso.error?.let { error = it; sso.error = null }
        if (got != null) {
            sso.code = null
            run { c.repo.signInWithSso(got); onSignedIn() }
        }
    }

    fun signIn() = run {
        when (val r = c.repo.signIn(base, email, password)) {
            SignIn.Done -> onSignedIn()
            is SignIn.NeedsCode -> {
                challenge = r.challenge
                step = Step.Code
            }
        }
    }

    Box(Modifier.fillMaxSize().background(MaterialTheme.colorScheme.background).statusBarsPadding().navigationBarsPadding().imePadding()) {
        Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(horizontal = 24.dp, vertical = 32.dp), verticalArrangement = Arrangement.Center, horizontalAlignment = Alignment.CenterHorizontally) {
            Box(Modifier.size(64.dp).clip(RoundedCornerShape(18.dp)).background(MaterialTheme.colorScheme.primary), contentAlignment = Alignment.Center) {
                Icon(Icons.Outlined.Description, null, tint = MaterialTheme.colorScheme.onPrimary, modifier = Modifier.size(34.dp))
            }
            Spacer(Modifier.height(20.dp))
            Text(
                when (step) {
                    Step.Server -> "Welcome to Docveta"
                    Step.Login -> "Sign in"
                    Step.Code -> "Two-step sign-in"
                    Step.Token -> "Sign in with a token"
                },
                style = MaterialTheme.typography.headlineMedium,
            )
            Spacer(Modifier.height(6.dp))
            Text(
                when (step) {
                    Step.Server -> "Connect to your own Docveta server."
                    Step.Login -> "to ${base.removePrefix("https://").removePrefix("http://")}"
                    Step.Code -> "Enter the 6-digit code from your authenticator app, or a recovery code."
                    Step.Token -> "Create an access token in Docveta on the web (Settings, API tokens) and paste it here."
                },
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                textAlign = TextAlign.Center,
            )
            Spacer(Modifier.height(28.dp))

            Column(Modifier.fillMaxWidth().widthIn(max = 420.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
                when (step) {
                    Step.Server -> {
                        OutlinedTextField(
                            server, { server = it }, label = { Text("Server address") }, placeholder = { Text("docs.example.com") }, singleLine = true, modifier = Modifier.fillMaxWidth(),
                            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri, imeAction = ImeAction.Go), keyboardActions = KeyboardActions(onGo = { checkServer() }),
                            supportingText = { if (server.trim().startsWith("http://")) Text("Not encrypted: only use this on your own network.") },
                        )
                        PrimaryButton("Continue", busy, enabled = server.isNotBlank()) { checkServer() }
                    }
                    Step.Login -> {
                        if (status?.oidc?.enabled == true) {
                            PrimaryButton(status!!.oidc.buttonLabel.ifBlank { "Sign in with SSO" }, busy) { startSso() }
                            if (status?.passwordLogin == true) Text("or", Modifier.align(Alignment.CenterHorizontally), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                        }
                        if (status?.passwordLogin != false) {
                        OutlinedTextField(email, { email = it }, label = { Text("Email") }, singleLine = true, modifier = Modifier.fillMaxWidth(), keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Email, imeAction = ImeAction.Next))
                        OutlinedTextField(
                            password, { password = it }, label = { Text("Password") }, singleLine = true, modifier = Modifier.fillMaxWidth(),
                            visualTransformation = if (show) VisualTransformation.None else PasswordVisualTransformation(),
                            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password, imeAction = ImeAction.Go), keyboardActions = KeyboardActions(onGo = { signIn() }),
                            trailingIcon = { IconButton({ show = !show }) { Icon(if (show) Icons.Outlined.VisibilityOff else Icons.Outlined.Visibility, if (show) "Hide password" else "Show password") } },
                        )
                        PrimaryButton("Sign in", busy, enabled = email.isNotBlank() && password.isNotEmpty()) { signIn() }
                        }
                        TextButton({ step = Step.Token; error = null }, Modifier.align(Alignment.CenterHorizontally)) { Text("Use an access token instead") }
                    }
                    Step.Code -> {
                        OutlinedTextField(
                            code, { code = it }, label = { Text("Code") }, singleLine = true, modifier = Modifier.fillMaxWidth(),
                            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Ascii, imeAction = ImeAction.Go), keyboardActions = KeyboardActions(onGo = { run { c.repo.signInWithCode(base, challenge, code); onSignedIn() } }),
                        )
                        PrimaryButton("Verify", busy, enabled = code.isNotBlank()) { run { c.repo.signInWithCode(base, challenge, code); onSignedIn() } }
                    }
                    Step.Token -> {
                        OutlinedTextField(token, { token = it }, label = { Text("Access token") }, singleLine = true, modifier = Modifier.fillMaxWidth(), keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Ascii, imeAction = ImeAction.Go), keyboardActions = KeyboardActions(onGo = { run { c.repo.signInWithToken(base, token); onSignedIn() } }))
                        PrimaryButton("Sign in", busy, enabled = token.isNotBlank()) { run { c.repo.signInWithToken(base, token); onSignedIn() } }
                        if (status?.passwordLogin == true || status?.oidc?.enabled == true) TextButton({ step = Step.Login; error = null }, Modifier.align(Alignment.CenterHorizontally)) { Text(if (status?.passwordLogin == true) "Use email and password" else "Back to sign in") }
                    }
                }
                if (error != null) Text(error!!, color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodyMedium)
                if (step != Step.Server) TextButton({ step = Step.Server; error = null; password = ""; code = "" }, Modifier.align(Alignment.CenterHorizontally)) { Text("Change server") }
            }
        }
    }
}

@Composable
private fun PrimaryButton(text: String, busy: Boolean, enabled: Boolean = true, onClick: () -> Unit) {
    Button(onClick, Modifier.fillMaxWidth().height(52.dp), enabled = enabled && !busy, shape = RoundedCornerShape(14.dp)) {
        if (busy) CircularProgressIndicator(Modifier.size(20.dp), strokeWidth = 2.dp, color = MaterialTheme.colorScheme.onPrimary) else Text(text, style = MaterialTheme.typography.titleSmall)
    }
}
