package app.docveta.android.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.LinkAnnotation
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.TextLinkStyles
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.withLink
import androidx.compose.ui.text.withStyle
import androidx.compose.ui.unit.dp

/** Blocks of a Markdown answer (the subset AI models write). */
internal sealed interface MdBlock {
    data class Para(val text: String) : MdBlock
    data class Heading(val text: String) : MdBlock
    data class Code(val text: String) : MdBlock
    data class ListBlock(val ordered: Boolean, val items: List<String>) : MdBlock
    data class Table(val head: List<String>, val rows: List<List<String>>) : MdBlock
}

private val listRe = Regex("""^\s*([-*+]|\d{1,3}[.)])\s+(.*)$""")
private val tableSep = Regex("""^\s*\|?\s*:?-{2,}:?\s*(\|\s*:?-{2,}:?\s*)*\|?\s*$""")

internal fun parseMarkdown(src: String): List<MdBlock> {
    val lines = src.replace("\r\n", "\n").split("\n")
    val out = mutableListOf<MdBlock>()
    val para = mutableListOf<String>()
    fun flush() {
        if (para.isNotEmpty()) out += MdBlock.Para(para.joinToString("\n"))
        para.clear()
    }
    var i = 0
    while (i < lines.size) {
        val line = lines[i]
        when {
            line.trimStart().startsWith("```") -> {
                flush()
                val body = mutableListOf<String>()
                i++
                while (i < lines.size && !lines[i].trimStart().startsWith("```")) body += lines[i++]
                out += MdBlock.Code(body.joinToString("\n"))
            }
            line.isBlank() -> flush()
            Regex("""^#{1,4}\s+""").containsMatchIn(line) -> {
                flush()
                out += MdBlock.Heading(line.replace(Regex("""^#{1,4}\s+"""), ""))
            }
            line.contains('|') && i + 1 < lines.size && tableSep.matches(lines[i + 1]) -> {
                flush()
                fun cells(l: String) = l.trim().trim('|').split('|').map { it.trim() }
                val head = cells(line)
                val rows = mutableListOf<List<String>>()
                i += 2
                while (i < lines.size && lines[i].contains('|') && lines[i].isNotBlank()) rows += cells(lines[i++])
                i--
                out += MdBlock.Table(head, rows)
            }
            listRe.matches(line) -> {
                flush()
                val ordered = listRe.find(line)!!.groupValues[1].first().isDigit()
                val items = mutableListOf<String>()
                while (i < lines.size) {
                    val m = listRe.find(lines[i])
                    if (m != null) items += m.groupValues[2]
                    else if (lines[i].isNotBlank() && lines[i].startsWith("  ") && items.isNotEmpty()) items[items.lastIndex] = items.last() + " " + lines[i].trim()
                    else break
                    i++
                }
                i--
                out += MdBlock.ListBlock(ordered, items)
            }
            else -> para += line
        }
        i++
    }
    flush()
    return out
}

private val inlineRe = Regex("""(`[^`\n]+`)|(\*\*[^*\n]+?\*\*|__[^_\n]+?__)|(\*[^*\s][^*\n]*?\*)|(\[(\d{1,2})])""")

/**
 * Inline Markdown (bold, italic, code) with citation markers "[2]" turned into tappable links;
 * [onCite] gets the citation number.
 */
internal fun inlineMarkdown(text: String, accent: Color, codeBg: Color, onCite: (Int) -> Unit): AnnotatedString = buildAnnotatedString {
    var last = 0
    for (m in inlineRe.findAll(text)) {
        append(text.substring(last, m.range.first))
        val s = m.value
        when {
            m.groups[1] != null -> withStyle(SpanStyle(fontFamily = FontFamily.Monospace, background = codeBg)) { append(s.substring(1, s.length - 1)) }
            m.groups[2] != null -> withStyle(SpanStyle(fontWeight = FontWeight.SemiBold)) { append(s.substring(2, s.length - 2)) }
            m.groups[3] != null -> withStyle(SpanStyle(fontStyle = FontStyle.Italic)) { append(s.substring(1, s.length - 1)) }
            else -> {
                val n = m.groupValues[5].toInt()
                withLink(LinkAnnotation.Clickable("cite-$n", TextLinkStyles(SpanStyle(color = accent, fontWeight = FontWeight.Bold))) { onCite(n) }) {
                    append("[$n]")
                }
            }
        }
        last = m.range.last + 1
    }
    append(text.substring(last))
}

@Composable
fun MarkdownText(text: String, color: Color = MaterialTheme.colorScheme.onSurface, onCite: (Int) -> Unit = {}) {
    val blocks = remember(text) { parseMarkdown(text) }
    val accent = MaterialTheme.colorScheme.primary
    val codeBg = MaterialTheme.colorScheme.surfaceContainerHigh
    val body = MaterialTheme.typography.bodyLarge
    fun inline(s: String) = inlineMarkdown(s, accent, codeBg, onCite)
    Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
        blocks.forEach { b ->
            when (b) {
                is MdBlock.Para -> Text(inline(b.text), color = color, style = body)
                is MdBlock.Heading -> Text(inline(b.text), color = color, style = MaterialTheme.typography.titleMedium)
                is MdBlock.Code -> Text(
                    b.text, Modifier.clip(RoundedCornerShape(8.dp)).background(codeBg).horizontalScroll(rememberScrollState()).padding(10.dp),
                    style = MaterialTheme.typography.bodySmall.copy(fontFamily = FontFamily.Monospace), color = color,
                )
                is MdBlock.ListBlock -> Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
                    b.items.forEachIndexed { i, it ->
                        Row {
                            Text(if (b.ordered) "${i + 1}." else "•", Modifier.width(22.dp), color = color, style = body)
                            Text(inline(it), color = color, style = body)
                        }
                    }
                }
                is MdBlock.Table -> Column(Modifier.clip(RoundedCornerShape(8.dp)).background(MaterialTheme.colorScheme.surfaceContainer).horizontalScroll(rememberScrollState()).padding(8.dp)) {
                    (listOf(b.head) + b.rows).forEachIndexed { r, row ->
                        Row(Modifier.padding(vertical = 4.dp)) {
                            row.forEach { cell ->
                                Text(inline(cell), Modifier.width(140.dp).padding(end = 8.dp), color = color,
                                    style = if (r == 0) MaterialTheme.typography.labelLarge else MaterialTheme.typography.bodyMedium)
                            }
                        }
                    }
                }
            }
        }
    }
}
