package app.docveta.android.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Description
import androidx.compose.material.icons.outlined.ErrorOutline
import androidx.compose.material.icons.outlined.Image
import androidx.compose.material.icons.outlined.Lock
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import app.docveta.android.data.Document
import app.docveta.android.data.Ref
import app.docveta.android.data.Space
import coil.compose.AsyncImage
import coil.request.ImageRequest
import java.time.LocalDate
import java.time.format.DateTimeFormatter

@Composable
fun LoadingBox(modifier: Modifier = Modifier.fillMaxSize()) {
    Box(modifier, contentAlignment = Alignment.Center) { CircularProgressIndicator(strokeWidth = 3.dp, modifier = Modifier.size(28.dp)) }
}

@Composable
fun EmptyState(icon: ImageVector, title: String, message: String? = null, modifier: Modifier = Modifier.fillMaxSize(), action: @Composable (() -> Unit)? = null) {
    Column(modifier.padding(32.dp), horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.Center) {
        Box(Modifier.size(56.dp).clip(RoundedCornerShape(16.dp)).background(MaterialTheme.colorScheme.surfaceVariant), contentAlignment = Alignment.Center) {
            Icon(icon, null, tint = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.size(28.dp))
        }
        Spacer(Modifier.height(16.dp))
        Text(title, style = MaterialTheme.typography.titleMedium, textAlign = TextAlign.Center)
        if (message != null) {
            Spacer(Modifier.height(6.dp))
            Text(message, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant, textAlign = TextAlign.Center)
        }
        if (action != null) {
            Spacer(Modifier.height(20.dp))
            action()
        }
    }
}

@Composable
fun ErrorState(message: String, onRetry: () -> Unit) {
    EmptyState(Icons.Outlined.ErrorOutline, "Something went wrong", message) {
        androidx.compose.material3.FilledTonalButton(onClick = onRetry) { Text("Try again") }
    }
}

/** A tag, as a rounded chip with its colour as a dot. */
@Composable
fun TagChip(tag: Ref, modifier: Modifier = Modifier) {
    Row(
        modifier.clip(RoundedCornerShape(8.dp)).background(colorFor(tag.color).copy(alpha = 0.14f)).padding(horizontal = 8.dp, vertical = 3.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Box(Modifier.size(7.dp).clip(CircleShape).background(colorFor(tag.color)))
        Spacer(Modifier.width(5.dp))
        Text(tag.name, style = MaterialTheme.typography.labelMedium, maxLines = 1, overflow = TextOverflow.Ellipsis)
    }
}

@Composable
fun Pill(text: String, container: Color, content: Color, modifier: Modifier = Modifier, icon: ImageVector? = null) {
    Row(modifier.clip(RoundedCornerShape(50)).background(container).padding(horizontal = 8.dp, vertical = 3.dp), verticalAlignment = Alignment.CenterVertically) {
        if (icon != null) {
            Icon(icon, null, tint = content, modifier = Modifier.size(12.dp))
            Spacer(Modifier.width(4.dp))
        }
        Text(text, style = MaterialTheme.typography.labelSmall, color = content)
    }
}

@Composable
fun StatusPill(doc: Document) {
    val cs = MaterialTheme.colorScheme
    when (doc.status) {
        "processing" -> Pill(processingLabel(doc), cs.surfaceVariant, cs.onSurfaceVariant)
        "failed" -> Pill("Couldn't process", cs.errorContainer, cs.error, icon = Icons.Outlined.ErrorOutline)
        "needs_password" -> Pill("Password protected", Color(0x33F59E0B), Color(0xFFB77900), icon = Icons.Outlined.Lock)
    }
}

/** The stage, with pages read so far while reading: "Reading text · 3 of 12 pages". */
fun processingLabel(doc: Document): String {
    val label = stageLabel(doc.processingStage)
    val p = doc.progress ?: return label
    return when {
        doc.processingStage != "ocr" -> label
        p.pagesTotal > 1 -> "$label · ${p.pagesDone} of ${p.pagesTotal} pages"
        p.pagesTotal == 0 && p.pagesDone > 1 -> "$label · ${p.pagesDone} pages read"
        else -> label
    }
}

fun stageLabel(stage: String) = when (stage) {
    "awaiting_ocr" -> "Waiting to read text"
    "ocr" -> "Reading text"
    "classifying", "indexing" -> "Organising"
    else -> "Processing"
}

@Composable
fun SpaceDot(space: Space?, modifier: Modifier = Modifier, size: Dp = 8.dp, color: Color? = null) {
    val c = color ?: if (space == null || space.isPersonal) Color(0xFF94A3B8) else colorFor(space.color)
    Box(modifier.size(size).clip(CircleShape).background(c))
}

/** Document thumbnail from the server, with a file-type icon while it loads or when there is none. */
@Composable
fun Thumbnail(doc: Document, url: String?, modifier: Modifier = Modifier) {
    Box(modifier.clip(RoundedCornerShape(8.dp)).background(MaterialTheme.colorScheme.surfaceVariant).border(0.5.dp, MaterialTheme.colorScheme.outlineVariant, RoundedCornerShape(8.dp)), contentAlignment = Alignment.Center) {
        val icon = if (doc.isImage) Icons.Outlined.Image else Icons.Outlined.Description
        Icon(icon, null, tint = MaterialTheme.colorScheme.outline, modifier = Modifier.size(24.dp))
        if (doc.hasThumbnail && url != null) {
            AsyncImage(
                model = ImageRequest.Builder(LocalContext.current).data(url).memoryCacheKey(url).diskCacheKey(url).build(),
                contentDescription = null,
                contentScale = ContentScale.Crop,
                alignment = Alignment.TopCenter,
                modifier = Modifier.fillMaxSize(),
            )
        }
    }
}

@Composable
fun SectionLabel(text: String, modifier: Modifier = Modifier) {
    Text(text.uppercase(), style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant, fontWeight = FontWeight.SemiBold, modifier = modifier.padding(horizontal = 16.dp, vertical = 8.dp))
}

@Composable
fun Modifier.clickableRow(onClick: () -> Unit): Modifier = this.fillMaxWidth().clickable(onClick = onClick)

/** 31 Jul 2026, in the way the person's account formats dates. */
fun formatDate(iso: String?, format: String = "DD/MM/YYYY"): String {
    if (iso.isNullOrBlank()) return ""
    return runCatching {
        val d = LocalDate.parse(iso.take(10))
        val pattern = when (format) {
            "MM/DD/YYYY" -> "MM/dd/yyyy"
            "YYYY-MM-DD" -> "yyyy-MM-dd"
            "DD.MM.YYYY" -> "dd.MM.yyyy"
            "D MMM YYYY" -> "d MMM yyyy"
            else -> "dd/MM/yyyy"
        }
        d.format(DateTimeFormatter.ofPattern(pattern))
    }.getOrDefault(iso)
}

fun formatBytes(n: Long): String {
    if (n < 1024) return "$n B"
    val units = listOf("KB", "MB", "GB")
    var v = n / 1024.0
    var i = 0
    while (v >= 1024 && i < units.lastIndex) {
        v /= 1024
        i++
    }
    return (if (v < 10) String.format(java.util.Locale.US, "%.1f", v) else v.toInt().toString()) + " " + units[i]
}

/** "3 minutes ago" for an ISO timestamp. */
fun timeAgo(iso: String?): String {
    if (iso.isNullOrBlank()) return ""
    return runCatching {
        val then = java.time.OffsetDateTime.parse(iso).toInstant()
        val s = java.time.Duration.between(then, java.time.Instant.now()).seconds
        when {
            s < 60 -> "just now"
            s < 3600 -> "${s / 60} min ago"
            s < 86400 -> "${s / 3600} h ago"
            s < 86400 * 7 -> "${s / 86400} d ago"
            else -> formatDate(iso)
        }
    }.getOrDefault("")
}
