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

## Signing in

Enter the server address, then email and password (and the two-step code if asked). The app
then swaps the web session for a long-lived access token kept in encrypted storage. Servers
that only offer single sign-on: create a token in the web app (Settings, API tokens) and paste it.

## Known limits

- The built-in scanner's page detection is brightness-based: white paper on a white desk falls back to adjustable
  corners (see `DocumentDetector`).
- The interface is English only for now; admin and space settings are in the web app.
