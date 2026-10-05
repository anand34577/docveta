# kotlinx.serialization keeps its own generated serializers; nothing app-specific is needed yet.
-keepattributes *Annotation*, InnerClasses
-dontnote kotlinx.serialization.**

# ONNX Runtime calls back into its Java classes from native code.
-keep class ai.onnxruntime.** { *; }

# Tink (used by androidx.security.crypto) refers to compile-time-only annotations.
-dontwarn com.google.errorprone.annotations.**
-dontwarn javax.annotation.**
-dontwarn com.google.crypto.tink.**
