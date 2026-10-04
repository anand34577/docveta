# Docveta for Android

Kotlin + Jetpack Compose client for a Docveta server. No Google Play Services or ML Kit: the
document scanner (page detection, crop, perspective correction, filters, PDF writer) is plain
Kotlin in `app/src/main/java/app/docveta/android/scan/` and is unit-tested on the JVM.

## Build

Needs JDK 17 and the Android SDK (platform 34). Create `local.properties` with
`sdk.dir=C:/path/to/Android/Sdk`, then:

```
./gradlew assembleDebug        # app/build/outputs/apk/debug/app-debug.apk
./gradlew testDebugUnitTest    # scanner, upload and API tests
```

## Signing in

Enter the server address, then email and password (and the two-step code if asked). The app
then swaps the web session for a long-lived access token kept in encrypted storage. Servers
that only offer single sign-on: create a token in the web app (Settings, API tokens) and paste it.

## Known limits

- Page detection is brightness-based: white paper on a white desk falls back to adjustable
  corners (see `DocumentDetector`).
- The interface is English only for now; admin and space settings are in the web app.
