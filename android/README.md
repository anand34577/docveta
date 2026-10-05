# Docveta for Android

Kotlin + Jetpack Compose client for a Docveta server.

Scanning uses Google's ML Kit document scanner (live edge detection, crop, clean-up, gallery
import) where Google Play services are available (`scan/MlKitScan.kt`). Its pages go into the
app's own review: reorder, one combined PDF or separate pictures, upload to the server. Phones
without Play services fall back to the built-in scanner (page detection, crop, perspective
correction, filters, PDF writer), plain Kotlin in `app/src/main/java/app/docveta/android/scan/`,
unit-tested on the JVM.

## Build

Needs JDK 17 and the Android SDK (platform 34). Create `local.properties` with
`sdk.dir=C:/path/to/Android/Sdk`, then:

```
./gradlew assembleDebug        # app/build/outputs/apk/debug/app-debug.apk
./gradlew testDebugUnitTest    # scanner, upload and API tests
```

## Releases

Pushing a `v*` tag builds a signed APK in GitHub Actions and attaches it to the release. Add four
repository **secrets** (not variables): `ANDROID_KEYSTORE_BASE64` (`base64 -w0 release.jks`),
`ANDROID_KEYSTORE_PASSWORD`, `ANDROID_KEY_ALIAS`, `ANDROID_KEY_PASSWORD`. The version comes from
the tag. Keep the keystore: updates must be signed with the same key.

## What's in the app

Everything the web app does: Inbox, documents with filters, saved views and bulk actions, the
document viewer with details, custom fields, notes, text, versions, similar documents, history and
page arranging, Ask, spaces (members, tags, correspondents, types, custom fields, workflows, AI,
scanning), settings (profile, security, notifications, tokens) and, for administrators, the whole
Administration area. It can also set up a new server and accept invitation links.

Scanning works without the server: scans wait in the upload queue. The phone can also read a
scan's text itself (Settings → This phone) and send a searchable PDF.

## Signing in

Enter the server address, then email and password (and the two-step code if asked). The app
then swaps the web session for a long-lived access token kept in encrypted storage. Servers
that only offer single sign-on: create a token in the web app (Settings, API tokens) and paste it.

## Known limits

- The built-in scanner's page detection is brightness-based: white paper on a white desk falls back to adjustable
  corners (see `DocumentDetector`).
- The interface is English only for now (so is the web app).
- Changing the password, two-step sign-in and creating API tokens ask for your password: the
  server only allows these from a fresh sign-in, never with the phone's stored token. People who
  sign in only with single sign-on do these in a browser.
- Phones signed in before administration came to the app: Administration asks for the password
  once to get a token with the admin permission.
- Works online only; there is no offline copy of the library yet.
