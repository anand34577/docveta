# Docveta for Android

Kotlin + Jetpack Compose client for a Docveta server.

Scanning is the app's own: live page detection, automatic capture, gallery import, crop with corner
and edge handles, perspective correction, filters and the PDF writer are plain Kotlin in
`app/src/main/java/app/docveta/android/scan/`, unit-tested on the JVM. No Google Play services,
offline. Page corners come from [DocAligner](https://github.com/DocsaidLab/DocAligner)
(Apache-2.0; its 83 MB FastViT-SA24 heatmap model in `assets/scan`, on ONNX Runtime's CPU, about
0.2 s a frame), see `PageFinder`. Its smaller models cut pages short on real photos. On 32-bit
phones, which have no ONNX Runtime, pages are found by brightness and by outline (straight
edges) instead; see `DocumentDetector` and `PageEdges`.

## Build

Needs JDK 17 and the Android SDK (platform 34). Create `local.properties` with
`sdk.dir=C:/path/to/Android/Sdk`, then:

```
./gradlew assembleDebug        # app/build/outputs/apk/debug/app-debug.apk
./gradlew testDebugUnitTest    # scanner, OCR, upload and API tests
./gradlew connectedDebugAndroidTest  # reads a page with the real models on a phone or emulator
```

The OCR models are taken from `workers/onnx/models` (or `DOCVETA_OCR_MODELS`): build them with
`workers/onnx/convert.py`, or unpack a release's `docveta-ocr-models-<version>.zip` there. Without
them the app still builds; it just leaves reading text to the server.

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
scan's text itself (Settings → This phone) and send a searchable PDF. It uses the same PaddleOCR
models as the server's OCR engine, on ONNX Runtime (`scan/PpOcr.kt`, `scan/OnnxOcr.kt`), offline
and without Google services. English and Devanagari come with the app; Tamil, Telugu and Kannada
are downloaded once from the release when needed. 32-bit phones leave reading to the server.

## Signing in

Enter the server address, then email and password (and the two-step code if asked). The app
then swaps the web session for a long-lived access token kept in encrypted storage. Servers
that only offer single sign-on: create a token in the web app (Settings, API tokens) and paste it.

## Known limits

- A page the scanner can't find (white paper on an equally white desk, say) opens the crop screen
  so the corners can be placed by hand.
- The interface is English only for now (so is the web app).
- Changing the password, two-step sign-in and creating API tokens ask for your password: the
  server only allows these from a fresh sign-in, never with the phone's stored token. People who
  sign in only with single sign-on do these in a browser.
- Phones signed in before administration came to the app: Administration asks for the password
  once to get a token with the admin permission.
- Works online only; there is no offline copy of the library yet.
