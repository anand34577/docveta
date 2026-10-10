package app.docveta.android.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.AutoAwesome
import androidx.compose.material.icons.outlined.Category
import androidx.compose.material.icons.outlined.Close
import androidx.compose.material.icons.outlined.DeleteOutline
import androidx.compose.material.icons.outlined.Description
import androidx.compose.material.icons.outlined.DocumentScanner
import androidx.compose.material.icons.outlined.Edit
import androidx.compose.material.icons.outlined.Group
import androidx.compose.material.icons.outlined.History
import androidx.compose.material.icons.automirrored.outlined.Label
import androidx.compose.material.icons.automirrored.outlined.ListAlt
import androidx.compose.material.icons.outlined.Person
import androidx.compose.material.icons.outlined.Print
import androidx.compose.material.icons.outlined.Settings
import androidx.compose.material.icons.outlined.AccountTree
import androidx.compose.material.icons.outlined.AutoFixHigh
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.Checkbox
import androidx.compose.material3.FilledTonalButton
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import app.docveta.android.data.ApiException
import app.docveta.android.data.CustomField
import app.docveta.android.data.Space
import app.docveta.android.data.Taxonomy
import app.docveta.android.data.Workflow
import app.docveta.android.data.aiStats
import app.docveta.android.data.barcodeSheet
import app.docveta.android.data.createSpace
import app.docveta.android.data.customFields
import app.docveta.android.data.deleteField
import app.docveta.android.data.deleteSpace
import app.docveta.android.data.deleteTaxonomy
import app.docveta.android.data.deleteWorkflow
import app.docveta.android.data.directory
import app.docveta.android.data.members
import app.docveta.android.data.mergeTaxonomy
import app.docveta.android.data.removeMember
import app.docveta.android.data.saveField
import app.docveta.android.data.saveTaxonomy
import app.docveta.android.data.saveWorkflow
import app.docveta.android.data.setMember
import app.docveta.android.data.setWorkflowEnabled
import app.docveta.android.data.updateSpace
import app.docveta.android.data.workflowRuns
import app.docveta.android.data.workflows
import kotlinx.coroutines.launch
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.booleanOrNull
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.put

/* ------------------------------------------------------------------ list */

@Composable
fun SpacesScreen(onBack: () -> Unit, onOpen: (String) -> Unit) {
    val c = LocalContainer.current
    val me = LocalMe.current
    val reloadMe = LocalReloadMe.current
    val run = rememberRunner()
    var creating by remember { mutableStateOf(false) }
    Page("Spaces", onBack, actions = { TextButton({ creating = true }) { Text("New space") } }) {
        Hint("Your personal space is only for you. Shared spaces hold documents for a family, a team or a business; everyone in one sees all of its documents.")
        me.spaces.forEach { s ->
            ItemRow(s.label, listOf(roleLabel(s.role), if (s.isPersonal) null else plural(s.memberCount, "member"), plural(s.documentCount, "document")).filterNotNull().joinToString(" · "), leading = { SpaceDot(s, size = 12.dp) }, onClick = { onOpen(s.id) })
        }
    }
    if (creating) {
        var name by remember { mutableStateOf("") }
        var desc by remember { mutableStateOf("") }
        var color by remember { mutableStateOf("blue") }
        AlertDialog(
            onDismissRequest = { creating = false }, title = { Text("New shared space") },
            text = {
                Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    OutlinedTextField(name, { name = it }, label = { Text("Name") }, placeholder = { Text("e.g. Family, Acme Traders") }, singleLine = true, modifier = Modifier.fillMaxWidth())
                    OutlinedTextField(desc, { desc = it }, label = { Text("Description (optional)") }, modifier = Modifier.fillMaxWidth())
                    Text("Colour", style = MaterialTheme.typography.labelLarge)
                    ColorPicker(color) { color = it }
                }
            },
            confirmButton = {
                TextButton({
                    run.run("Space created") {
                        val s = c.repo.createSpace(name.trim(), desc.trim(), color)
                        creating = false
                        reloadMe()
                        onOpen(s.id)
                    }
                }, enabled = name.isNotBlank() && !run.busy) { Text("Create") }
            },
            dismissButton = { TextButton({ creating = false }) { Text("Cancel") } },
        )
    }
}

fun roleLabel(r: String) = when (r) { "owner" -> "Owner"; "editor" -> "Editor"; else -> "Viewer" }

private val roleInfo = listOf("viewer" to "View, search, download and comment", "editor" to "Upload, edit, tag and delete documents", "owner" to "Everything, including members and settings")

/* ------------------------------------------------------------------ one space */

@Composable
fun SpaceScreen(id: String, onBack: () -> Unit, onNavigate: (String) -> Unit) {
    val me = LocalMe.current
    val s = me.spaces.firstOrNull { it.id == id }
    if (s == null) {
        Page("Space", onBack) { EmptyState(Icons.Outlined.Group, "This space is gone", "It was deleted, or you are no longer a member.", Modifier.fillMaxWidth()) }
        return
    }
    Page(s.label, onBack, subtitle = if (s.isPersonal) "Only you can see documents here." else "${plural(s.memberCount, "member")} · ${plural(s.documentCount, "document")} · you are ${roleLabel(s.role).lowercase()}") {
        NavRow("General", "Name, colour, description, language", Icons.Outlined.Settings) { onNavigate("space/$id/general") }
        if (!s.isPersonal) NavRow("Members", "Who can see and change documents", Icons.Outlined.Group) { onNavigate("space/$id/members") }
        NavRow("Tags", "Labels like Tax, Medical or Car", Icons.AutoMirrored.Outlined.Label) { onNavigate("space/$id/tags") }
        NavRow("Correspondents", "Who documents are from or to", Icons.Outlined.Person) { onNavigate("space/$id/correspondents") }
        NavRow("Document types", "Identification, Bill, Insurance, Certificate…", Icons.Outlined.Category) { onNavigate("space/$id/document-types") }
        NavRow("Custom fields", "Amount, due date, policy number…", Icons.AutoMirrored.Outlined.ListAlt) { onNavigate("space/$id/fields") }
        NavRow("Workflows", "Do things automatically", Icons.Outlined.AccountTree) { onNavigate("space/$id/workflows") }
        NavRow("AI", "Suggestions for new documents", Icons.Outlined.AutoAwesome) { onNavigate("space/$id/ai") }
        NavRow("Scanning", "Separator sheets and archive numbers", Icons.Outlined.DocumentScanner) { onNavigate("space/$id/scanning") }
    }
}

/** Looks up the space for a sub-screen; shows a placeholder when it no longer exists. */
@Composable
private fun WithSpace(id: String, title: String, onBack: () -> Unit, content: @Composable (Space) -> Unit) {
    val s = LocalMe.current.spaces.firstOrNull { it.id == id }
    if (s == null) Page(title, onBack) { EmptyState(Icons.Outlined.Group, "This space is gone", modifier = Modifier.fillMaxWidth()) } else content(s)
}

private val languages = listOf("en" to "English", "hi" to "Hindi", "mr" to "Marathi", "bn" to "Bengali", "gu" to "Gujarati", "ta" to "Tamil", "te" to "Telugu", "kn" to "Kannada", "ml" to "Malayalam", "pa" to "Punjabi", "ur" to "Urdu", "de" to "German", "fr" to "French", "es" to "Spanish")

fun languageOptions(current: String?): List<Pair<String, String>> = if (current.isNullOrBlank() || languages.any { it.first == current }) languages else listOf(current to current) + languages

@Composable
fun SpaceGeneralScreen(id: String, onBack: () -> Unit, onGone: () -> Unit) = WithSpace(id, "General", onBack) { s ->
    val c = LocalContainer.current
    val me = LocalMe.current
    val reloadMe = LocalReloadMe.current
    val run = rememberRunner()
    val confirm = rememberConfirmation()
    var name by remember(s.id) { mutableStateOf(s.name) }
    var desc by remember(s.id) { mutableStateOf(s.description) }
    var lang by remember(s.id) { mutableStateOf(s.defaultLanguage) }
    val owner = s.isOwner
    fun save(patch: JsonObject, msg: String? = "Saved") = run.run(msg) { c.repo.updateSpace(s.id, patch); reloadMe() }
    Page("General", onBack, subtitle = s.label) {
        if (!owner) Hint("Only owners of this space can change these.")
        if (!s.isPersonal) Field(name, { name = it }, "Name", enabled = owner)
        Field(desc, { desc = it }, "Description", enabled = owner, singleLine = false)
        SelectField("Default document language", languageOptions(lang), lang, enabled = owner, supporting = "The first guess for new documents. Text recognition also spots Hindi, Tamil, Telugu, Kannada and English pages by itself.") { lang = it }
        if (owner) Button({ save(buildJsonObject { put("name", name.trim()); put("description", desc); put("default_language", lang) }) }, Modifier.padding(horizontal = 16.dp, vertical = 6.dp), enabled = !run.busy && (s.isPersonal || name.isNotBlank())) { Text("Save") }
        SectionLabel("Colour")
        Hint("Shown next to this space's name in lists and filters.")
        ColorPicker(s.color, enabled = owner) { save(buildJsonObject { put("color", it) }, null) }
        if (!s.isPersonal) {
            SectionDivider()
            if (owner) {
                SectionLabel("Delete space")
                Hint("Move or delete all its documents first (including the Trash).")
                TextButton({ confirm.ask("Delete “${s.name}”?", "The space must be empty, including its Trash.", "Delete space", true) { run.run("Space deleted") { c.repo.deleteSpace(s.id); reloadMe(); onGone() } } }, Modifier.padding(horizontal = 8.dp)) {
                    Text("Delete this space", color = MaterialTheme.colorScheme.error)
                }
            } else {
                SectionLabel("Leave space")
                TextButton({ confirm.ask("Leave “${s.name}”?", "You'll lose access to its documents.", "Leave", true) { run.run { c.repo.removeMember(s.id, me.id); reloadMe(); onGone() } } }, Modifier.padding(horizontal = 8.dp)) {
                    Text("Leave this space", color = MaterialTheme.colorScheme.error)
                }
            }
        }
    }
}

@Composable
fun SpaceMembersScreen(id: String, onBack: () -> Unit) = WithSpace(id, "Members", onBack) { s ->
    val c = LocalContainer.current
    val me = LocalMe.current
    val reloadMe = LocalReloadMe.current
    val run = rememberRunner()
    val confirm = rememberConfirmation()
    val members = rememberLoader(s.id) { c.repo.members(s.id) }
    val dir = rememberLoader { runCatching { c.repo.directory() }.getOrDefault(emptyList()) }
    var adding by remember { mutableStateOf(false) }
    val owner = s.isOwner
    Page("Members", onBack, subtitle = s.label, actions = { if (owner) TextButton({ adding = true }) { Text("Add") } }) {
        Hint("People in this space can see all of its documents.")
        Loaded(members) { list ->
            list.forEach { m ->
                ItemRow(m.displayName + if (m.userId == me.id) " (you)" else "", m.email, leading = { Initials(m.displayName) }) {
                    if (owner) {
                        SelectField("Role", roleInfo.map { it.first to roleLabel(it.first) }, m.role, Modifier.width(132.dp)) { r -> run.run { c.repo.setMember(s.id, m.userId, r); members.reload(); reloadMe() } }
                        if (m.userId != me.id) IconButton({ confirm.ask("Remove ${m.displayName}?", "They'll lose access to documents in ${s.name}.", "Remove", true) { run.run { c.repo.removeMember(s.id, m.userId); members.reload(); reloadMe() } } }) { Icon(Icons.Outlined.DeleteOutline, "Remove") }
                    } else StatusBadge(roleLabel(m.role))
                }
            }
        }
        SectionDivider()
        roleInfo.forEach { (r, info) -> Hint("${roleLabel(r)}: $info") }
    }
    if (adding) {
        val existing = members.data.orEmpty().map { it.userId }.toSet()
        val candidates = dir.data.orEmpty().filter { it.id !in existing }
        var who by remember { mutableStateOf(candidates.firstOrNull()?.id) }
        var role by remember { mutableStateOf("editor") }
        AlertDialog(
            onDismissRequest = { adding = false }, title = { Text("Add a person") },
            text = {
                Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    if (candidates.isEmpty()) Text("Everyone with a Docveta account is already here. Ask an administrator to add more people.")
                    else {
                        SelectField("Person", candidates.map { it.id to "${it.displayName} (${it.email})" }, who, Modifier.fillMaxWidth()) { who = it }
                        SelectField("Role", roleInfo.map { it.first to roleLabel(it.first) }, role, Modifier.fillMaxWidth(), supporting = roleInfo.first { it.first == role }.second) { role = it }
                    }
                }
            },
            confirmButton = { TextButton({ val u = who ?: return@TextButton; adding = false; run.run("Added") { c.repo.setMember(s.id, u, role); members.reload(); reloadMe() } }, enabled = who != null) { Text("Add") } },
            dismissButton = { TextButton({ adding = false }) { Text("Cancel") } },
        )
    }
}

/* ------------------------------------------------------------------ tags, correspondents, types */

private val kindInfo = mapOf(
    "tags" to Triple("tag", "Tags", "Labels like Tax, Medical or Car. A document can have many."),
    "correspondents" to Triple("correspondent", "Correspondents", "Who a document is from or to: a bank, a hospital, a company."),
    "document-types" to Triple("document type", "Document types", "The broad kind of document: Identification, Bill, Insurance, Certificate. Each document has one; tags (Aadhaar, PAN) say what exactly it is."),
)

private val matchHelp = listOf(
    "none" to "Don't assign automatically", "any" to "Any of these words appears", "all" to "All of these words appear",
    "exact" to "This exact phrase appears", "regex" to "Regular expression matches", "fuzzy" to "Similar phrase appears (tolerates scanning errors)",
)

@Composable
fun VocabularyScreen(id: String, kind: String, onBack: () -> Unit) = WithSpace(id, kindInfo[kind]?.second ?: kind, onBack) { s ->
    val c = LocalContainer.current
    val run = rememberRunner()
    val confirm = rememberConfirmation()
    val (one, many, help) = kindInfo[kind] ?: Triple(kind, kind, "")
    val items = rememberLoader(s.id, kind) { c.repo.taxonomy(kind, s.id).filter { it.spaceId == s.id || it.spaceId.isEmpty() } }
    var editing by remember { mutableStateOf<Taxonomy?>(null) }
    var creating by remember { mutableStateOf(false) }
    var selecting by remember { mutableStateOf(false) }
    var selected by remember { mutableStateOf<List<String>>(emptyList()) }
    var filter by remember { mutableStateOf("") }
    val canEdit = s.canWrite
    Page(many, onBack, subtitle = s.label, actions = {
        if (canEdit) {
            if (selecting) TextButton({ selecting = false; selected = emptyList() }) { Text("Done") }
            else if ((items.data?.size ?: 0) > 1) TextButton({ selecting = true }) { Text("Merge") }
            TextButton({ creating = true }) { Text("New") }
        }
    }) {
        Hint(help)
        if (selecting) {
            Hint("Tick the ones to combine. The first one you tick is kept; the others are merged into it.")
            Button({
                val t = items.data?.firstOrNull { it.id == selected.first() } ?: return@Button
                confirm.ask("Merge ${selected.size} into “${t.name}”?", "Documents keep everything; duplicates are removed.", "Merge") {
                    run.run("Merged") { c.repo.mergeTaxonomy(kind, selected.first(), selected.drop(1)); selected = emptyList(); selecting = false; items.reload() }
                }
            }, Modifier.padding(horizontal = 16.dp), enabled = selected.size > 1) { Text("Merge ${selected.size}") }
        }
        if ((items.data?.size ?: 0) > 8) Field(filter, { filter = it }, "Filter ${many.lowercase()}")
        Loaded(items) { list ->
            val shown = list.filter { it.name.contains(filter.trim(), ignoreCase = true) }
            if (shown.isEmpty()) EmptyState(Icons.AutoMirrored.Outlined.Label, "No ${many.lowercase()} yet", "You can also create them right from a document.", Modifier.fillMaxWidth())
            shown.forEach { t ->
                val auto = if (t.matchAlgorithm != "none" && t.matchPattern.isNotBlank()) "Auto: " + (matchHelp.firstOrNull { it.first == t.matchAlgorithm }?.second?.lowercase() ?: t.matchAlgorithm) + " — " + t.matchPattern else null
                ItemRow(
                    t.name, listOfNotNull(plural(t.documentCount, "document"), auto).joinToString(" · "),
                    leading = if (selecting || kind == "tags") {
                        {
                            if (selecting) Checkbox(t.id in selected, { on -> selected = if (on) selected + t.id else selected - t.id })
                            else SpaceDot(null, size = 12.dp, color = colorFor(t.color))
                        }
                    } else null,
                    onClick = if (selecting) ({ selected = if (t.id in selected) selected - t.id else selected + t.id }) else if (canEdit) ({ editing = t }) else null,
                ) {
                    if (canEdit && !selecting) IconButton({
                        confirm.ask("Delete $one “${t.name}”?", if (t.documentCount > 0) "It will be removed from ${plural(t.documentCount, "document")}. The documents are kept." else null, "Delete", true) {
                            run.run { c.repo.deleteTaxonomy(kind, t.id); items.reload() }
                        }
                    }) { Icon(Icons.Outlined.DeleteOutline, "Delete") }
                }
            }
        }
    }
    if (creating || editing != null) TaxonomyDialog(kind, one, s.id, editing, onDismiss = { creating = false; editing = null }) { items.reload() }
}

@Composable
private fun TaxonomyDialog(kind: String, one: String, spaceId: String, item: Taxonomy?, onDismiss: () -> Unit, onSaved: () -> Unit) {
    val c = LocalContainer.current
    val scope = rememberCoroutineScope()
    var name by remember { mutableStateOf(item?.name ?: "") }
    var color by remember { mutableStateOf(item?.color?.ifBlank { null } ?: "blue") }
    var algo by remember { mutableStateOf(item?.matchAlgorithm ?: "none") }
    var pattern by remember { mutableStateOf(item?.matchPattern ?: "") }
    var cs by remember { mutableStateOf(item?.caseSensitive ?: false) }
    var error by remember { mutableStateOf<String?>(null) }
    var busy by remember { mutableStateOf(false) }
    AlertDialog(
        onDismissRequest = onDismiss, title = { Text(if (item != null) "Edit $one" else "New $one") },
        text = {
            Column(Modifier.verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                OutlinedTextField(name, { name = it }, label = { Text("Name") }, singleLine = true, modifier = Modifier.fillMaxWidth())
                if (kind == "tags") {
                    Text("Colour", style = MaterialTheme.typography.labelLarge)
                    ColorPicker(color) { color = it }
                }
                Text("Assign automatically", style = MaterialTheme.typography.labelLarge)
                Text("When new documents contain matching text, Docveta adds this $one for you.", style = MaterialTheme.typography.bodySmall)
                SelectField("Matching", matchHelp, algo, Modifier.fillMaxWidth()) { algo = it }
                if (algo != "none") {
                    OutlinedTextField(pattern, { pattern = it }, label = { Text(if (algo == "regex") "Regular expression" else "Words or phrase") }, modifier = Modifier.fillMaxWidth(),
                        supportingText = { if (algo == "any" || algo == "all") Text("Separate words with spaces. Use quotes for phrases: BESCOM \"electricity bill\"") })
                    Row(verticalAlignment = Alignment.CenterVertically) { Checkbox(cs, { cs = it }); Text("Case sensitive") }
                }
                if (error != null) Text(error!!, color = MaterialTheme.colorScheme.error)
            }
        },
        confirmButton = {
            TextButton({
                busy = true
                scope.launch {
                    try {
                        c.repo.saveTaxonomy(kind, spaceId, item, name, color, algo, pattern, cs)
                        onSaved()
                        onDismiss()
                    } catch (e: ApiException) {
                        error = e.fields.joinToString(" · ") { it.message }.ifBlank { e.message }
                    } catch (e: Exception) {
                        error = e.friendly()
                    } finally {
                        busy = false
                    }
                }
            }, enabled = !busy && name.isNotBlank()) { Text("Save") }
        },
        dismissButton = { TextButton(onDismiss) { Text("Cancel") } },
    )
}

/* ------------------------------------------------------------------ custom fields */

val fieldTypes = listOf(
    "text" to "Short text", "longtext" to "Long text", "integer" to "Whole number", "decimal" to "Number with decimals", "monetary" to "Amount of money",
    "date" to "Date", "boolean" to "Yes / No", "url" to "Web address", "select" to "One choice from a list", "multiselect" to "Several choices from a list",
)

@Composable
fun FieldsScreen(id: String, onBack: () -> Unit) = WithSpace(id, "Custom fields", onBack) { s ->
    val c = LocalContainer.current
    val run = rememberRunner()
    val confirm = rememberConfirmation()
    val fields = rememberLoader(s.id) { c.repo.customFields(s.id).filter { it.spaceId == s.id } }
    var editing by remember { mutableStateOf<CustomField?>(null) }
    var creating by remember { mutableStateOf(false) }
    val owner = s.isOwner
    Page("Custom fields", onBack, subtitle = s.label, actions = { if (owner) TextButton({ creating = true }) { Text("Add") } }) {
        Hint("Extra details to record on documents, such as Amount or Due date. Search by them too, for example cf:Amount>1500.")
        Loaded(fields) { list ->
            if (list.isEmpty()) EmptyState(Icons.AutoMirrored.Outlined.ListAlt, "No custom fields", "Add “Amount”, “Due date” or “Policy number” and fill them in on each document.", Modifier.fillMaxWidth())
            list.forEach { f ->
                ItemRow(f.name, listOfNotNull(fieldTypes.firstOrNull { it.first == f.dataType }?.second ?: f.dataType, "used on ${plural(f.documentCount, "document")}", f.options.choices.takeIf { it.isNotEmpty() }?.joinToString(", ")).joinToString(" · "), onClick = if (owner) ({ editing = f }) else null) {
                    if (owner) IconButton({ confirm.ask("Delete “${f.name}”?", "Its value is removed from ${plural(f.documentCount, "document")}.", "Delete field", true) { run.run { c.repo.deleteField(f.id); fields.reload() } } }) { Icon(Icons.Outlined.DeleteOutline, "Delete") }
                }
            }
        }
    }
    if (creating || editing != null) {
        val f = editing
        var name by remember { mutableStateOf(f?.name ?: "") }
        var type by remember { mutableStateOf(f?.dataType ?: "text") }
        var choices by remember { mutableStateOf(f?.options?.choices?.joinToString("\n") ?: "") }
        var currency by remember { mutableStateOf(f?.options?.currency ?: "INR") }
        AlertDialog(
            onDismissRequest = { creating = false; editing = null }, title = { Text(if (f != null) "Edit field" else "New custom field") },
            text = {
                Column(Modifier.verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    OutlinedTextField(name, { name = it }, label = { Text("Name") }, placeholder = { Text("e.g. Amount, Due date") }, singleLine = true, modifier = Modifier.fillMaxWidth())
                    SelectField("Type", fieldTypes, type, Modifier.fillMaxWidth(), enabled = f == null, supporting = if (f != null) "The type can't change after creation." else null) { type = it }
                    if (type == "monetary") OutlinedTextField(currency, { if (it.length <= 3) currency = it.uppercase() }, label = { Text("Currency code") }, supportingText = { Text("For example INR, USD, EUR.") }, singleLine = true)
                    if (type == "select" || type == "multiselect") OutlinedTextField(choices, { choices = it }, label = { Text("Choices") }, supportingText = { Text("One per line.") }, minLines = 4, modifier = Modifier.fillMaxWidth())
                }
            },
            confirmButton = {
                TextButton({
                    run.run {
                        c.repo.saveField(f, s.id, name, type, choices.lines().map { it.trim() }.filter { it.isNotEmpty() }, currency)
                        creating = false; editing = null
                        fields.reload()
                    }
                }, enabled = name.isNotBlank() && !run.busy) { Text("Save") }
            },
            dismissButton = { TextButton({ creating = false; editing = null }) { Text("Cancel") } },
        )
    }
}

/* ------------------------------------------------------------------ AI */

@Composable
fun SpaceAiScreen(id: String, onBack: () -> Unit) = WithSpace(id, "AI", onBack) { s ->
    val c = LocalContainer.current
    val reloadMe = LocalReloadMe.current
    val run = rememberRunner()
    val stats = rememberLoader(s.id) { runCatching { c.repo.aiStats(s.id) }.getOrNull() }
    fun save(k: String, v: String) = run.run("Saved") { c.repo.updateSpace(s.id, buildJsonObject { put(k, v) }); reloadMe() }
    Page("AI", onBack, subtitle = s.label) {
        Hint("AI reads each new document and works out what kind of document it is, who it is from, its date, its tags and your custom fields. You see what it would change first, unless you pick automatic.")
        if (!s.isOwner) Hint("Only owners of this space can change these.")
        SelectField("Use AI for this space", listOf("off" to "Off", "local_only" to "Only with a local provider", "any" to "Any configured provider"), s.aiPolicy, enabled = s.isOwner,
            supporting = "“Local only” sends text only to providers marked as running on your own hardware.") { save("ai_policy", it) }
        if (s.aiPolicy != "off") SelectField("When AI has suggestions", listOf("suggest" to "Ask me first (shown in the Inbox)", "auto" to "Apply automatically"), s.aiApplyMode, enabled = s.isOwner) { save("ai_apply_mode", it) }
        if (s.aiPolicy != "off") {
            fun saveFlag(k: String, v: Boolean) = run.run("Saved") { c.repo.updateSpace(s.id, buildJsonObject { put(k, v) }); reloadMe() }
            fun saveNumber(k: String, v: String) = run.run("Saved") { c.repo.updateSpace(s.id, buildJsonObject { put(k, v.toInt()) }); reloadMe() }
            // The steps offered, and the saved value if it was set to something in between on the web.
            fun choices(range: IntProgression, current: Int, suffix: String) = (range.toList() + current).distinct().sorted().map { it.toString() to "$it$suffix" }
            SectionDivider()
            SectionLabel("Fine-tuning")
            Hint("How much AI may decide by itself in this space. The defaults suit most; change them if it files too eagerly or too timidly.")
            if (s.aiApplyMode == "auto") SelectField("Apply automatically when AI is at least this sure", choices(50..100 step 5, s.aiAutoConfidence, "%"), s.aiAutoConfidence.toString(), enabled = s.isOwner,
                supporting = "Lower applies more by itself and gets more wrong; higher leaves more for you to check.") { saveNumber("ai_auto_confidence", it) }
            ToggleRow("Let AI create new tags", "When a document has no fitting tag yet (say, Aadhaar or PAN), AI may add one. Turn this off to keep to the tags you made yourself.", s.aiNewTags, enabled = s.isOwner) { saveFlag("ai_new_tags", it) }
            if (s.aiNewTags) SelectField("New tags per document, at most", choices(1..10, s.aiMaxNewTags, ""), s.aiMaxNewTags.toString(), enabled = s.isOwner) { saveNumber("ai_max_new_tags", it) }
            ToggleRow("Let AI create new document types", "When none of this space's types fits (say, Identification), AI may add one. Turn this off to keep to your own types.", s.aiNewTypes, enabled = s.isOwner) { saveFlag("ai_new_types", it) }
            if (s.aiNewTags || s.aiNewTypes) SelectField("Create something new when AI is at least this sure", choices(0..100 step 10, s.aiNewConfidence, "%"), s.aiNewConfidence.toString(), enabled = s.isOwner,
                supporting = "For tags, senders and types that don't exist yet.") { saveNumber("ai_new_confidence", it) }
            if (s.isOwner && (s.aiAutoConfidence != 85 || s.aiNewConfidence != 60 || s.aiMaxNewTags != 3 || !s.aiNewTags || !s.aiNewTypes)) TextButton({
                run.run("Back to the defaults") {
                    c.repo.updateSpace(s.id, buildJsonObject { put("ai_auto_confidence", 85); put("ai_new_confidence", 60); put("ai_max_new_tags", 3); put("ai_new_tags", true); put("ai_new_types", true) })
                    reloadMe()
                }
            }, Modifier.padding(horizontal = 8.dp)) { Text("Back to the defaults") }
            SectionDivider()
        }
        val st = stats.data
        if (s.aiPolicy != "off" && st != null) {
            SectionLabel("How well is it doing?")
            StatTiles(listOf("Accepted" to st.accepted.toString(), "Dismissed" to st.rejected.toString(), "Waiting" to st.pending.toString(), "Accept rate" to "${(st.acceptRate * 100).toInt()}%"))
        }
    }
}

/* ------------------------------------------------------------------ scanning */

@Composable
fun SpaceScanningScreen(id: String, onBack: () -> Unit) = WithSpace(id, "Scanning", onBack) { s ->
    val c = LocalContainer.current
    val reloadMe = LocalReloadMe.current
    val ctx = LocalContext.current
    val run = rememberRunner()
    var asn by remember { mutableStateOf("1") }
    fun save(k: String, v: Boolean) = run.run { c.repo.updateSpace(s.id, buildJsonObject { put(k, v) }); reloadMe() }
    Page("Scanning", onBack, subtitle = s.label) {
        SectionLabel("Batch scanning")
        Hint("Scan a whole stack in one go and let Docveta cut it into documents.")
        ToggleRow("Split at separator sheets", "Put a printed separator page between documents. Docveta splits the scan there and drops the separator.", s.splitOnSeparators, enabled = s.isOwner) { save("split_on_separators", it) }
        ToggleRow("Read archive-number barcodes", "If a page carries an ASN label, that number becomes the document's archive number.", s.readAsnBarcodes, enabled = s.isOwner) { save("read_asn_barcodes", it) }
        SectionDivider()
        SectionLabel("Print sheets and labels")
        Hint("Opens the picture; print it from there on plain paper.")
        OutlinedButton({ run.run { openFile(ctx, c.repo.barcodeSheet(null), "image/png") } }, Modifier.padding(horizontal = 16.dp, vertical = 6.dp)) {
            Icon(Icons.Outlined.Print, null); Spacer(Modifier.width(8.dp)); Text("Separator sheet")
        }
        Row(FieldPad, verticalAlignment = Alignment.CenterVertically) {
            OutlinedTextField(asn, { asn = it.filter(Char::isDigit).take(12) }, Modifier.width(140.dp), label = { Text("Archive number") }, singleLine = true, keyboardOptions = androidx.compose.foundation.text.KeyboardOptions(keyboardType = KeyboardType.Number))
            Spacer(Modifier.width(10.dp))
            OutlinedButton({ val n = asn.toLongOrNull()?.coerceAtLeast(1) ?: 1; run.run { openFile(ctx, c.repo.barcodeSheet(n), "image/png") } }) { Icon(Icons.Outlined.Print, null); Spacer(Modifier.width(8.dp)); Text("Label") }
        }
    }
}

/* ------------------------------------------------------------------ workflows */

private val triggers = listOf("added" to "A document is added", "processed" to "A document finishes processing", "updated" to "A document is changed", "schedule" to "Every day at a set time")

private val actionLabels = listOf(
    "add_tags" to "Add tags", "remove_tags" to "Remove tags", "set_correspondent" to "Set who it's from", "set_document_type" to "Set the document type",
    "set_field" to "Set a custom field", "set_inbox" to "Put in or take out of the Inbox", "move_to_space" to "Move to another space",
    "notify" to "Send a notification", "webhook" to "Call a webhook", "run_ai" to "Ask AI for suggestions",
)

@Composable
fun WorkflowsScreen(id: String, onBack: () -> Unit, onEdit: (String) -> Unit) = WithSpace(id, "Workflows", onBack) { s ->
    val c = LocalContainer.current
    val run = rememberRunner()
    val confirm = rememberConfirmation()
    val list = rememberLoader(s.id) { c.repo.workflows(s.id) }
    var runs by remember { mutableStateOf<Workflow?>(null) }
    androidx.lifecycle.compose.LifecycleEventEffect(androidx.lifecycle.Lifecycle.Event.ON_RESUME) { list.reload() }
    Page("Workflows", onBack, subtitle = s.label, actions = { if (s.isOwner) TextButton({ onEdit("new") }) { Text("New") } }) {
        Hint("Do things automatically. For example: when a document from HDFC is added, tag it “Bank” and set its type to Statement.")
        Loaded(list) { l ->
            if (l.isEmpty()) EmptyState(Icons.Outlined.AutoFixHigh, "No workflows yet", modifier = Modifier.fillMaxWidth())
            l.forEach { w ->
                val trig = triggers.firstOrNull { it.first == w.trigger }?.second ?: w.trigger
                ItemRow(
                    w.name, "When ${trig.lowercase()}${if (w.trigger == "schedule") " (${w.scheduleTime})" else ""} · ${plural(w.actions.size, "action")} · ran ${plural(w.runCount, "time")}${w.lastRunAt?.let { ", last ${timeAgo(it)}" } ?: ""}",
                    leading = { Switch(w.enabled, { on -> run.run { c.repo.setWorkflowEnabled(w.id, on); list.reload() } }, enabled = s.isOwner) },
                    onClick = if (s.isOwner) ({ onEdit(w.id) }) else null,
                ) {
                    IconButton({ runs = w }) { Icon(Icons.Outlined.History, "History") }
                    if (s.isOwner) IconButton({ confirm.ask("Delete “${w.name}”?", null, "Delete", true) { run.run { c.repo.deleteWorkflow(w.id); list.reload() } } }) { Icon(Icons.Outlined.DeleteOutline, "Delete") }
                }
            }
        }
    }
    runs?.let { w ->
        val r = rememberLoader(w.id) { c.repo.workflowRuns(w.id) }
        AlertDialog(
            onDismissRequest = { runs = null }, title = { Text("History: ${w.name}") },
            text = {
                Column(Modifier.verticalScroll(rememberScrollState())) {
                    Loaded(r) { items ->
                        if (items.isEmpty()) Text("It hasn't run yet.")
                        items.forEach { x ->
                            Column(Modifier.padding(vertical = 6.dp)) {
                                Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                                    StatusBadge(when (x.status) { "done" -> "Done"; "failed" -> "Failed"; else -> "Skipped" }, when (x.status) { "done" -> "success"; "failed" -> "danger"; else -> "neutral" })
                                    Text(x.documentTitle.ifBlank { "Scheduled run" }, Modifier.weight(1f), style = MaterialTheme.typography.bodyMedium, maxLines = 1)
                                }
                                Text(formatDateTime(x.ranAt), style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                                if (x.summary.isNotBlank()) Text(x.summary, style = MaterialTheme.typography.bodySmall)
                            }
                        }
                    }
                }
            },
            confirmButton = { TextButton({ runs = null }) { Text("Close") } },
        )
    }
}

private fun JsonObject.str(k: String) = (this[k] as? JsonPrimitive)?.contentOrNull
private fun JsonObject.ids(k: String) = (this[k] as? JsonArray)?.mapNotNull { (it as? JsonPrimitive)?.contentOrNull }.orEmpty()

/** One action of a workflow while it's edited: its type and every parameter as text. */
private data class ActionDraft(
    val type: String = "add_tags", val names: String = "", val name: String = "", val fieldId: String = "", val value: String = "",
    val inbox: Boolean = true, val spaceId: String = "", val to: String = "space_owners", val title: String = "", val message: String = "", val url: String = "",
) {
    fun toJson(fields: List<CustomField>): JsonObject = buildJsonObject {
        put("type", type)
        when (type) {
            "add_tags", "remove_tags" -> put("names", JsonArray(names.split(",").map { it.trim() }.filter { it.isNotEmpty() }.map { JsonPrimitive(it) }))
            "set_correspondent", "set_document_type" -> put("name", name.trim())
            "set_field" -> {
                put("field_id", fieldId)
                val f = fields.firstOrNull { it.id == fieldId }
                put("value", if (f?.dataType == "boolean") JsonPrimitive(value == "true") else JsonPrimitive(value))
            }
            "set_inbox" -> put("value", inbox)
            "move_to_space" -> put("space_id", spaceId)
            "notify" -> { put("to", to); put("title", title); put("message", message) }
            "webhook" -> put("url", url.trim())
        }
    }

    companion object {
        fun from(o: JsonObject) = ActionDraft(
            type = o.str("type") ?: "add_tags", names = o.ids("names").joinToString(", "), name = o.str("name").orEmpty(), fieldId = o.str("field_id").orEmpty(),
            value = (o["value"] as? JsonPrimitive)?.contentOrNull.orEmpty(), inbox = (o["value"] as? JsonPrimitive)?.booleanOrNull ?: true,
            spaceId = o.str("space_id").orEmpty(), to = o.str("to") ?: "space_owners", title = o.str("title").orEmpty(), message = o.str("message").orEmpty(), url = o.str("url").orEmpty(),
        )
    }
}

@Composable
fun WorkflowEditScreen(spaceId: String, workflowId: String?, onBack: () -> Unit) = WithSpace(spaceId, "Workflow", onBack) { s ->
    val c = LocalContainer.current
    val existing = rememberLoader(spaceId, workflowId) { if (workflowId == null) null else c.repo.workflows(spaceId).firstOrNull { it.id == workflowId } }
    val fields = rememberLoader(spaceId) { runCatching { c.repo.customFields(spaceId).filter { it.spaceId == spaceId } }.getOrDefault(emptyList()) }
    if (workflowId != null && existing.data == null) {
        Page("Edit workflow", onBack) { if (existing.error != null) ErrorState(existing.error!!) { existing.reload() } else LoadingBox(Modifier.fillMaxWidth().padding(48.dp)) }
        return@WithSpace
    }
    WorkflowForm(s, existing.data, fields.data.orEmpty(), onBack)
}

@Composable
private fun WorkflowForm(s: Space, w: Workflow?, fields: List<CustomField>, onBack: () -> Unit) {
    val c = LocalContainer.current
    val me = LocalMe.current
    var name by remember { mutableStateOf(w?.name ?: "") }
    var trigger by remember { mutableStateOf(w?.trigger ?: "added") }
    var time by remember { mutableStateOf(w?.scheduleTime?.ifBlank { null } ?: "09:00") }
    val cond = w?.conditions ?: JsonObject(emptyMap())
    var q by remember { mutableStateOf(cond.str("q").orEmpty()) }
    var corr by remember { mutableStateOf(cond.ids("correspondent_ids")) }
    var types by remember { mutableStateOf(cond.ids("type_ids")) }
    var tags by remember { mutableStateOf(cond.ids("any_tag_ids")) }
    val untagged = (cond["untagged"] as? JsonPrimitive)?.booleanOrNull == true
    var actions by remember { mutableStateOf(w?.actions?.map { ActionDraft.from(it) }?.ifEmpty { null } ?: listOf(ActionDraft())) }
    var picker by remember { mutableStateOf<String?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    var busy by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()
    val corrNames = rememberNames("correspondents", s.id)
    val typeNames = rememberNames("document-types", s.id)
    val tagNames = rememberNames("tags", s.id)

    fun save() {
        busy = true
        error = null
        scope.launch {
            try {
                val body = buildJsonObject {
                    put("space_id", s.id); put("name", name.trim()); put("trigger", trigger)
                    if (trigger == "schedule") put("schedule_time", time)
                    put("conditions", buildJsonObject {
                        if (q.isNotBlank()) put("q", q.trim())
                        fun ids(k: String, l: List<String>) { if (l.isNotEmpty()) put(k, JsonArray(l.map { JsonPrimitive(it) })) }
                        ids("correspondent_ids", corr); ids("type_ids", types); ids("any_tag_ids", tags)
                        if (untagged) put("untagged", true)
                    })
                    put("actions", JsonArray(actions.map { it.toJson(fields) }))
                }
                c.repo.saveWorkflow(w?.id, body)
                onBack()
            } catch (e: ApiException) {
                error = e.fields.joinToString(" · ") { it.message }.ifBlank { e.message }
            } catch (e: Exception) {
                error = e.friendly()
            } finally {
                busy = false
            }
        }
    }

    Page(if (w == null) "New workflow" else "Edit workflow", onBack, subtitle = s.label, actions = { TextButton(::save, enabled = !busy && name.isNotBlank()) { Text("Save") } }) {
        Field(name, { name = it }, "Name", placeholder = "e.g. File HDFC statements")
        SectionLabel("When")
        SelectField("Trigger", triggers, trigger) { trigger = it }
        if (trigger == "schedule") Field(time, { time = it.take(5) }, "Time (HH:MM)", keyboard = KeyboardType.Number)
        SectionLabel("…and the document matches")
        Hint("Leave everything empty to match every document.")
        Field(q, { q = it }, "Text or filters", placeholder = "e.g. from:HDFC", supporting = "Same as the search box: words, or things like from:HDFC type:statement")
        PickerField("From", corr.map { corrNames[it] ?: "…" }, "Anyone", onClear = { corr = emptyList() }) { picker = "correspondents" }
        PickerField("Type", types.map { typeNames[it] ?: "…" }, "Any type", onClear = { types = emptyList() }) { picker = "document-types" }
        PickerField("Has any of these tags", tags.map { tagNames[it] ?: "…" }, "Any", onClear = { tags = emptyList() }) { picker = "tags" }
        SectionLabel("Then")
        actions.forEachIndexed { i, a ->
            ActionEditor(a, s, me.spaces, fields, onChange = { n -> actions = actions.toMutableList().also { it[i] = n } }, onRemove = if (actions.size > 1) ({ actions = actions.filterIndexed { j, _ -> j != i } }) else null)
        }
        OutlinedButton({ actions = actions + ActionDraft() }, Modifier.padding(horizontal = 16.dp, vertical = 6.dp)) { Text("Add action") }
        if (error != null) Text(error!!, color = MaterialTheme.colorScheme.error, modifier = Modifier.padding(16.dp))
        Button(::save, Modifier.padding(16.dp).fillMaxWidth(), enabled = !busy && name.isNotBlank()) { Text("Save workflow") }
    }
    picker?.let { k ->
        TaxonomyPicker(k, s.id, when (k) { "correspondents" -> corr; "document-types" -> types; else -> tags }, multi = true, allowCreate = false, onDismiss = { picker = null }) { picked ->
            val ids = picked.map { it.id }
            when (k) { "correspondents" -> corr = ids; "document-types" -> types = ids; else -> tags = ids }
            picker = null
        }
    }
}

@Composable
private fun ActionEditor(a: ActionDraft, space: Space, spaces: List<Space>, fields: List<CustomField>, onChange: (ActionDraft) -> Unit, onRemove: (() -> Unit)?) {
    Column(FieldPad.padding(vertical = 4.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            SelectField("Action", actionLabels, a.type, Modifier.weight(1f)) { onChange(ActionDraft(type = it)) }
            if (onRemove != null) IconButton(onRemove) { Icon(Icons.Outlined.Close, "Remove action") }
        }
        val m = Modifier.fillMaxWidth().padding(top = 6.dp)
        when (a.type) {
            "add_tags", "remove_tags" -> Field(a.names, { onChange(a.copy(names = it)) }, "Tag names, comma-separated", m)
            "set_correspondent", "set_document_type" -> Field(a.name, { onChange(a.copy(name = it)) }, "Name (created if missing)", m)
            "set_field" -> {
                SelectField("Field", fields.map { it.id to it.name }, a.fieldId, m) { onChange(a.copy(fieldId = it, value = "")) }
                if (fields.firstOrNull { it.id == a.fieldId }?.dataType == "boolean") SelectField("Value", listOf("true" to "Yes", "false" to "No"), a.value.ifBlank { "true" }, m) { onChange(a.copy(value = it)) }
                else Field(a.value, { onChange(a.copy(value = it)) }, "Value", m)
            }
            "set_inbox" -> SelectField("Inbox", listOf(true to "Put in the Inbox", false to "Mark as reviewed"), a.inbox, m) { onChange(a.copy(inbox = it)) }
            "move_to_space" -> SelectField("Space", spaces.filter { it.canWrite && it.id != space.id }.map { it.id to it.label }, a.spaceId, m) { onChange(a.copy(spaceId = it)) }
            "notify" -> {
                SelectField("Who", listOf("owner" to "The person who added it", "space_owners" to "Space owners", "space_members" to "Everyone in the space"), a.to, m) { onChange(a.copy(to = it)) }
                Field(a.title, { onChange(a.copy(title = it)) }, "Title", m)
                Field(a.message, { onChange(a.copy(message = it)) }, "Message (optional)", m)
            }
            "webhook" -> Field(a.url, { onChange(a.copy(url = it)) }, "Webhook URL", m, keyboard = KeyboardType.Uri)
            "run_ai" -> Text("Needs AI turned on for this space (AI section).", style = MaterialTheme.typography.bodySmall, modifier = Modifier.padding(top = 6.dp))
        }
    }
}
