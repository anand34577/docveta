package app.docveta.android

import app.docveta.android.data.ApiClient
import app.docveta.android.data.ApiException
import app.docveta.android.data.AppJson
import app.docveta.android.data.DocQuery
import app.docveta.android.data.Document
import app.docveta.android.data.MemoryStore
import app.docveta.android.data.OidcConfig
import app.docveta.android.data.Ref
import app.docveta.android.data.Repository
import app.docveta.android.data.SessionStore
import app.docveta.android.data.bulk
import app.docveta.android.data.changePassword
import app.docveta.android.data.editPages
import app.docveta.android.data.saveOidc
import java.nio.file.Files
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import kotlinx.serialization.json.put
import okhttp3.mockwebserver.Dispatcher
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import okhttp3.mockwebserver.RecordedRequest
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Before
import org.junit.Test

class DocQueryTest {
    @Test
    fun readsTheWebAppsSavedViewQuery() {
        val web = AppJson.parseToJsonElement("""{"q":"electricity","space_id":["s1"],"tag_id":["t1","t2"],"correspondent_id":["c1"],"date_from":"2025-04-01","date_to":"2026-03-31","untagged":true,"status":"failed,needs_password","sort":"-date","mode":"hybrid"}""") as JsonObject
        val q = DocQuery.fromJson(web)
        assertEquals("electricity", q.q)
        assertEquals("s1", q.spaceId)
        assertEquals(listOf("t1", "t2"), q.tagIds)
        assertEquals(listOf("c1"), q.correspondentIds)
        assertEquals("2025-04-01", q.dateFrom)
        assertTrue(q.untagged)
        assertTrue(q.semantic)
        assertEquals("-date", q.sort)
        assertEquals(7, q.filterCount) // space, two tags, sender, date range, untagged, status
        assertEquals(q, DocQuery.fromJson(q.toJson())) // and writes it back the same way
    }

    @Test
    fun clearingFiltersKeepsTheSearchAndSort() {
        val q = DocQuery(q = "rent", tagIds = listOf("t"), sort = "title", untagged = true).clearedFilters()
        assertEquals("rent", q.q)
        assertEquals("title", q.sort)
        assertEquals(0, q.filterCount)
    }
}

class RepositoryFeaturesTest {
    private lateinit var server: MockWebServer
    private val session = SessionStore(MemoryStore())
    private lateinit var repo: Repository
    private val seen = ArrayList<RecordedRequest>()

    @Before
    fun up() {
        server = MockWebServer()
        server.start()
        session.serverUrl = server.url("/").toString().trimEnd('/')
        session.token = "dvt_phone"
        session.userEmail = "a@b.c"
        repo = Repository(ApiClient(session), session, Files.createTempDirectory("docveta").toFile(), "Pixel")
    }

    @After
    fun down() = server.shutdown()

    private fun respond(handler: (RecordedRequest) -> MockResponse) {
        server.dispatcher = object : Dispatcher() {
            override fun dispatch(request: RecordedRequest): MockResponse {
                synchronized(seen) { seen.add(request) }
                return handler(request)
            }
        }
    }

    private fun body(r: RecordedRequest) = AppJson.parseToJsonElement(r.body.readUtf8()).jsonObject

    @Test
    fun sendsEveryFilterTheServerUnderstands() = runBlocking {
        respond { MockResponse().setBody("""{"items":[],"total":0}""") }
        repo.documents(DocQuery(correspondentIds = listOf("c1", "c2"), typeIds = listOf("d1"), dateFrom = "2026-01-01", untagged = true))
        val url = seen.single().requestUrl!!
        assertEquals(listOf("c1", "c2"), url.queryParameterValues("correspondent_id"))
        assertEquals(listOf("d1"), url.queryParameterValues("document_type_id"))
        assertEquals("2026-01-01", url.queryParameter("date_from"))
        assertEquals("true", url.queryParameter("untagged"))
        assertNull(url.queryParameter("date_to"))
    }

    @Test
    fun administratorsGetATokenThatCanDoAdministration() = runBlocking {
        session.signOut()
        var tokenBody: JsonObject? = null
        respond { r ->
            when ("${r.method} ${r.path}") {
                "POST /api/v1/auth/login" -> MockResponse().setBody("{}").addHeader("Set-Cookie", "docveta_session=web1; Path=/; HttpOnly")
                "GET /api/v1/me" -> {
                    val web = r.getHeader("Cookie")?.contains("docveta_session=web1") == true
                    MockResponse().setBody("""{"id":"u","email":"a@b.c","display_name":"Asha","is_admin":$web}""")
                }
                "POST /api/v1/me/tokens" -> {
                    tokenBody = body(r)
                    MockResponse().setResponseCode(201).setBody("""{"secret":"dvt_new","token":{"id":"tok1","scopes":["documents:read","documents:write","upload","admin"]}}""")
                }
                else -> MockResponse().setResponseCode(204)
            }
        }
        repo.signIn(session.serverUrl ?: server.url("/").toString().trimEnd('/'), "a@b.c", "pw")
        val scopes = tokenBody!!["scopes"]!!.jsonArray.map { it.jsonPrimitive.content }
        assertTrue("admin" in scopes)
        assertEquals("dvt_new", session.token)
        assertEquals("tok1", session.tokenId)
        assertTrue(repo.hasAdminScope)
    }

    @Test
    fun passwordChangesUseAFreshSignInNotThePhonesToken() = runBlocking {
        val paths = ArrayList<String>()
        respond { r ->
            paths.add("${r.method} ${r.path}")
            when (r.path) {
                "/api/v1/auth/login" -> MockResponse().setBody("""{"two_factor_required":true,"challenge":"ch1"}""")
                "/api/v1/auth/login/2fa" -> MockResponse().setBody("{}").addHeader("Set-Cookie", "docveta_session=web2; Path=/")
                "/api/v1/me/password" -> {
                    assertNull(r.getHeader("Authorization")) // never the stored token
                    assertTrue(r.getHeader("Cookie")!!.contains("web2"))
                    assertEquals("new-password-123", body(r)["new_password"]!!.jsonPrimitive.content)
                    MockResponse().setResponseCode(204)
                }
                else -> MockResponse().setResponseCode(204)
            }
        }
        // Without the code: the app is told to ask for it, and nothing is changed.
        try {
            repo.changePassword("old", "new-password-123", null)
            fail("should ask for the code")
        } catch (e: ApiException) {
            assertEquals("two_factor_required", e.code)
        }
        assertFalse(paths.any { it.endsWith("/me/password") })
        paths.clear()
        repo.changePassword("old", "new-password-123", "123 456")
        assertEquals(listOf("POST /api/v1/auth/login", "POST /api/v1/auth/login/2fa", "POST /api/v1/me/password", "POST /api/v1/auth/logout"), paths)
    }

    @Test
    fun bulkAndPageEditsSendTheShapesTheServerReads() = runBlocking {
        respond { r -> if (r.path!!.endsWith("/bulk")) MockResponse().setBody("""{"succeeded":3,"failed":[],"remaining":0}""") else MockResponse().setBody("""{"id":"d","space":{"id":"s","name":"S"},"title":"T"}""") }
        val r = repo.bulk(emptyList(), "update", buildJsonObject { put("inbox", false) }, select = "inbox")
        assertEquals(3, r.succeeded)
        val b = body(seen[0])
        assertEquals("inbox", b["select"]!!.jsonPrimitive.content)
        assertEquals("update", b["action"]!!.jsonPrimitive.content)
        assertEquals(JsonPrimitive(false), b["update"]!!.jsonObject["inbox"])

        repo.editPages("d", listOf(3 to 90, 1 to 0))
        val pages = body(seen[1])["pages"]!!.jsonArray.map { it.jsonObject }
        assertEquals(3, pages[0]["from"]!!.jsonPrimitive.content.toInt())
        assertEquals(90, pages[0]["rotate"]!!.jsonPrimitive.content.toInt())
        assertEquals(2, pages.size) // page 2 left out = deleted
    }

    @Test
    fun ssoSettingsKeepTheSavedSecretUnlessANewOneIsTyped() = runBlocking {
        respond { MockResponse().setBody("""{"enabled":true,"issuer":"https://id","client_id":"c","has_client_secret":true}""") }
        repo.saveOidc(OidcConfig(enabled = true, issuer = "https://id", clientId = "c", hasClientSecret = true, redirectUri = "https://x/cb"), "")
        val first = body(seen[0])
        assertFalse("client_secret" in first)
        assertFalse("redirect_uri" in first) // read-only
        repo.saveOidc(OidcConfig(enabled = true), "s3cret")
        assertEquals("s3cret", body(seen[1])["client_secret"]!!.jsonPrimitive.content)
    }

    @Test
    fun editingDetailsDoesNotDownloadTheFileAgain() {
        val d = Document(id = "d", space = Ref("s", "S"), title = "Bill", mimeType = "application/pdf", sizeBytes = 1000, pageCount = 2, version = 3)
        assertEquals(repo.viewableName(d), repo.viewableName(d.copy(title = "Bill (paid)", version = 4)))
        assertNotEquals(repo.viewableName(d), repo.viewableName(d.copy(sizeBytes = 900, pageCount = 1))) // pages were removed
        assertNotEquals(repo.viewableName(d), repo.viewableName(d.copy(hasArchive = true))) // the searchable PDF is ready
    }
}

class FindTest {
    @Test
    fun findsTextIgnoringCaseAndSpacesOnEveryPage() {
        val pages = listOf(0 to "Electricity bill\nBESCOM electricity  bill", 1 to "Amount due 1,842.50\nElec tricity bill", 2 to "")
        val found = app.docveta.android.ui.findInPageTexts(pages, "electricity bill")
        assertEquals(listOf(0, 0, 1), found.map { it.page })
        assertTrue(app.docveta.android.ui.findInPageTexts(pages, "  ").isEmpty())
    }
}
