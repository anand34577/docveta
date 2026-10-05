import java.security.MessageDigest

plugins {
    alias(libs.plugins.android.application)
    alias(libs.plugins.kotlin.android)
    alias(libs.plugins.kotlin.compose)
    alias(libs.plugins.kotlin.serialization)
}

// CI passes the release version (tag) and the signing key; a local build gets a dev version and
// is signed with the debug key only if you ask for a debug build.
val releaseVersion = System.getenv("DOCVETA_VERSION")?.removePrefix("v") ?: "0.0.0-dev"
val versionParts = releaseVersion.substringBefore('-').split('.').map { it.toIntOrNull() ?: 0 } + listOf(0, 0, 0)
val keystorePath: String? = System.getenv("ANDROID_KEYSTORE_PATH")

// The OCR models (PaddleOCR as ONNX, from workers/onnx/convert.py or the release's models zip).
// The text finder and the English and Devanagari readers go into the app; the other readers are
// downloaded by the phone when needed, so only their checksums go in. Without models the app
// builds fine and leaves reading text to the server.
val ocrModelsDir = file(System.getenv("DOCVETA_OCR_MODELS") ?: "../../workers/onnx/models")
val bundledOcr = listOf("det.onnx", "rec_en.onnx", "rec_devanagari.onnx")
// Where the phone downloads the other readers: this version's release (the latest one for dev builds).
val ocrModelsUrl = if (releaseVersion.contains("dev")) "https://github.com/anand34577/docveta/releases/latest/download/"
else "https://github.com/anand34577/docveta/releases/download/v$releaseVersion/"

abstract class OcrAssets : DefaultTask() {
    @get:InputFiles @get:Optional @get:PathSensitive(PathSensitivity.NAME_ONLY)
    abstract val models: ConfigurableFileCollection

    @get:Input
    abstract val bundled: ListProperty<String>

    @get:OutputDirectory
    abstract val outputDir: DirectoryProperty

    @TaskAction
    fun copy() {
        val out = outputDir.get().asFile.resolve("ocr")
        out.deleteRecursively()
        val files = models.files.filter { it.isFile }
        if (files.none { it.name == "det.onnx" }) {
            logger.warn("No OCR models found (looked in ${models.files.firstOrNull()?.parent}): this build won't read text on the phone.")
            return
        }
        out.mkdirs()
        val sums = StringBuilder()
        for (f in files.sortedBy { it.name }) {
            if (f.name.startsWith("dict_") || f.name in bundled.get()) f.copyTo(out.resolve(f.name))
            if (f.name.endsWith(".onnx")) {
                val md = MessageDigest.getInstance("SHA-256")
                f.inputStream().use { i -> val b = ByteArray(1 shl 16); while (true) { val n = i.read(b); if (n < 0) break; md.update(b, 0, n) } }
                sums.append(md.digest().joinToString("") { "%02x".format(it) }).append("  ").append(f.name).append('\n')
            }
        }
        out.resolve("checksums.txt").writeText(sums.toString())
    }
}

val ocrAssets = tasks.register<OcrAssets>("ocrAssets") {
    models.from(ocrModelsDir.listFiles()?.filter { it.name.endsWith(".onnx") || it.name.endsWith(".txt") } ?: emptyList<File>())
    bundled.set(bundledOcr)
    outputDir.set(layout.buildDirectory.dir("generated/ocr-assets"))
}

android {
    namespace = "app.docveta.android"
    compileSdk = 37

    defaultConfig {
        applicationId = "app.docveta.android"
        minSdk = 26
        targetSdk = 37
        versionCode = (versionParts[0] * 1_000_000 + versionParts[1] * 1_000 + versionParts[2]).coerceAtLeast(1)
        versionName = releaseVersion
        testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner"
                vectorDrawables.useSupportLibrary = true
        buildConfigField("String", "OCR_MODELS_URL", "\"$ocrModelsUrl\"")
        ndk { abiFilters += listOf("arm64-v8a", "armeabi-v7a") }
    }

    signingConfigs {
        if (keystorePath != null) create("release") {
            storeFile = file(keystorePath)
            storePassword = System.getenv("ANDROID_KEYSTORE_PASSWORD")
            keyAlias = System.getenv("ANDROID_KEY_ALIAS")
            keyPassword = System.getenv("ANDROID_KEY_PASSWORD")
        }
    }
    buildTypes {
        debug {
            ndk { abiFilters += "x86_64" } // the emulator
        }
        release {
            if (keystorePath != null) signingConfig = signingConfigs.getByName("release")
            isMinifyEnabled = true
            isShrinkResources = true
            proguardFiles(getDefaultProguardFile("proguard-android-optimize.txt"), "proguard-rules.pro")
        }
    }
    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
    kotlinOptions { jvmTarget = "17" }
    buildFeatures { compose = true; buildConfig = true }
    packaging {
        resources.excludes += "/META-INF/{AL2.0,LGPL2.1}"
        // Compressed native libraries: a much smaller download (ONNX Runtime), unpacked once on install.
        jniLibs.useLegacyPackaging = true
        // No text reading on 32-bit phones: they're low-memory phones where it's skipped anyway
        // (see PhoneOcr.canRunNow), and ONNX Runtime would add 12 MB for them.
        jniLibs.excludes += "lib/armeabi-v7a/libonnxruntime*.so"
    }
    testOptions { unitTests.isReturnDefaultValues = true }
    sourceSets["androidTest"].resources.srcDir("src/test/resources") // the OCR test page
}

androidComponents {
    onVariants { v -> v.sources.assets?.addGeneratedSourceDirectory(ocrAssets, OcrAssets::outputDir) }
}

dependencies {
    implementation(libs.androidx.core.ktx)
    implementation(libs.androidx.activity.compose)
    implementation(libs.androidx.fragment)
    implementation(libs.androidx.lifecycle.runtime)
    implementation(libs.androidx.lifecycle.viewmodel)
    implementation(libs.androidx.lifecycle.compose)
    implementation(libs.androidx.navigation)
    implementation(platform(libs.compose.bom))
    implementation(libs.compose.ui)
    implementation(libs.compose.ui.graphics)
    implementation(libs.compose.ui.tooling.preview)
    implementation(libs.compose.material3)
    implementation(libs.compose.material.icons)
    implementation(libs.kotlinx.coroutines)
    implementation(libs.kotlinx.serialization.json)
    implementation(libs.okhttp)
    implementation(libs.coil.compose)
    implementation(libs.camerax.core)
    implementation(libs.camerax.camera2)
    implementation(libs.camerax.lifecycle)
    implementation(libs.camerax.view)
    implementation(libs.androidx.work)
    implementation(libs.androidx.datastore)
    implementation(libs.androidx.biometric)
    implementation(libs.androidx.security.crypto)
    implementation(libs.androidx.exifinterface)
    implementation(libs.onnxruntime.android)

    testImplementation(libs.junit)
    testImplementation(libs.kotlinx.coroutines.test)
    testImplementation(libs.okhttp.mockwebserver)
    testImplementation(libs.kotlinx.serialization.json)
    androidTestImplementation(libs.androidx.test.runner)
    androidTestImplementation(libs.androidx.test.junit)
    testImplementation(libs.onnxruntime.jvm) // the same API on the desktop, for the OCR test with real models
}
