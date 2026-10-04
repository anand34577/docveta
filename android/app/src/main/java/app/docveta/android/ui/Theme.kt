package app.docveta.android.ui

import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Shapes
import androidx.compose.material3.Typography
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.SideEffect
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalView
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.core.view.WindowCompat

val Accent = Color(0xFF4F6BED)
private val AccentDark = Color(0xFF8FA2FF)

private val Light = lightColorScheme(
    primary = Accent, onPrimary = Color.White, primaryContainer = Color(0xFFE3E8FF), onPrimaryContainer = Color(0xFF16215F),
    secondary = Color(0xFF5B6270), onSecondary = Color.White, secondaryContainer = Color(0xFFE9EBF2), onSecondaryContainer = Color(0xFF1B1F2A),
    background = Color(0xFFF7F8FB), onBackground = Color(0xFF14171F),
    surface = Color.White, onSurface = Color(0xFF14171F), surfaceVariant = Color(0xFFEEF0F6), onSurfaceVariant = Color(0xFF5B6270),
    surfaceContainer = Color(0xFFF1F3F9), surfaceContainerHigh = Color(0xFFEAEDF5), surfaceContainerLow = Color(0xFFF7F8FB),
    outline = Color(0xFFC4C8D4), outlineVariant = Color(0xFFE2E5EE), error = Color(0xFFC62F3E), errorContainer = Color(0xFFFFE1E3),
)

private val Dark = darkColorScheme(
    primary = AccentDark, onPrimary = Color(0xFF0F1640), primaryContainer = Color(0xFF26346E), onPrimaryContainer = Color(0xFFDCE2FF),
    secondary = Color(0xFFB4BAC8), onSecondary = Color(0xFF1B1F2A), secondaryContainer = Color(0xFF2A2F3C), onSecondaryContainer = Color(0xFFE2E5EE),
    background = Color(0xFF0F1115), onBackground = Color(0xFFE9EBF2),
    surface = Color(0xFF171A21), onSurface = Color(0xFFE9EBF2), surfaceVariant = Color(0xFF232733), onSurfaceVariant = Color(0xFF9EA5B5),
    surfaceContainer = Color(0xFF1B1F27), surfaceContainerHigh = Color(0xFF232733), surfaceContainerLow = Color(0xFF13161C),
    outline = Color(0xFF444B5C), outlineVariant = Color(0xFF2A2F3C), error = Color(0xFFFF8A94), errorContainer = Color(0xFF4A1C22),
)

private val Type = Typography(
    headlineMedium = TextStyle(fontSize = 26.sp, lineHeight = 32.sp, fontWeight = FontWeight.SemiBold, letterSpacing = (-0.3).sp),
    titleLarge = TextStyle(fontSize = 20.sp, lineHeight = 26.sp, fontWeight = FontWeight.SemiBold),
    titleMedium = TextStyle(fontSize = 16.sp, lineHeight = 22.sp, fontWeight = FontWeight.SemiBold),
    titleSmall = TextStyle(fontSize = 14.sp, lineHeight = 20.sp, fontWeight = FontWeight.Medium),
    bodyLarge = TextStyle(fontSize = 16.sp, lineHeight = 24.sp),
    bodyMedium = TextStyle(fontSize = 14.sp, lineHeight = 20.sp),
    bodySmall = TextStyle(fontSize = 12.sp, lineHeight = 16.sp),
    labelLarge = TextStyle(fontSize = 14.sp, lineHeight = 20.sp, fontWeight = FontWeight.Medium),
    labelMedium = TextStyle(fontSize = 12.sp, lineHeight = 16.sp, fontWeight = FontWeight.Medium),
    labelSmall = TextStyle(fontSize = 11.sp, lineHeight = 14.sp, fontWeight = FontWeight.Medium),
)

private val Round = Shapes(
    extraSmall = androidx.compose.foundation.shape.RoundedCornerShape(6.dp),
    small = androidx.compose.foundation.shape.RoundedCornerShape(10.dp),
    medium = androidx.compose.foundation.shape.RoundedCornerShape(14.dp),
    large = androidx.compose.foundation.shape.RoundedCornerShape(20.dp),
    extraLarge = androidx.compose.foundation.shape.RoundedCornerShape(28.dp),
)

@Composable
fun DocvetaTheme(dark: Boolean = isSystemInDarkTheme(), content: @Composable () -> Unit) {
    val view = LocalView.current
    if (!view.isInEditMode) {
        SideEffect {
            val w = (view.context as? android.app.Activity)?.window ?: return@SideEffect
            WindowCompat.getInsetsController(w, view).apply {
                isAppearanceLightStatusBars = !dark
                isAppearanceLightNavigationBars = !dark
            }
        }
    }
    MaterialTheme(colorScheme = if (dark) Dark else Light, typography = Type, shapes = Round, content = content)
}

/** The tag/space colour names the server uses, as Compose colours. */
fun colorFor(name: String): Color = when (name) {
    "slate" -> Color(0xFF64748B)
    "red" -> Color(0xFFEF4444)
    "orange" -> Color(0xFFF97316)
    "amber" -> Color(0xFFF59E0B)
    "lime" -> Color(0xFF84CC16)
    "green" -> Color(0xFF22C55E)
    "teal" -> Color(0xFF14B8A6)
    "cyan" -> Color(0xFF06B6D4)
    "blue" -> Color(0xFF3B82F6)
    "indigo" -> Color(0xFF6366F1)
    "violet" -> Color(0xFF8B5CF6)
    "pink" -> Color(0xFFEC4899)
    else -> Color(0xFF64748B)
}
