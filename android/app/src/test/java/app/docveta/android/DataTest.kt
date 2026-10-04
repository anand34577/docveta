package app.docveta.android

import app.docveta.android.data.ApiClient
import app.docveta.android.data.ApiException
import app.docveta.android.data.AppJson
import app.docveta.android.data.Document
import app.docveta.android.data.DocumentList
import app.docveta.android.data.MemoryStore
import app.docveta.android.data.SessionStore
import app.docveta.android.data.normalizeServerUrl
import app.docveta.android.upload.TusUploader
import java.io.File
import kotlinx.coroutines.runBlocking
import okhttp3.mockwebserver.Dispatcher
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import okhttp3.mockwebserver.RecordedRequest
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Before
import org.junit.Test

class ServerUrlTest {
    @Test
    fun addsTheRightScheme() {
        assertEquals("https://docs.example.com", normalizeServerUrl("docs.example.com"))
        assertEquals("http://192.168.1.20:8080", normalizeServerUrl("192.168.1.20:8080"))
        assertEquals("http://localhost:8080", normalizeServerUrl("localhost:8080/"))
        assertEquals("https://docs.example.com", normalizeServerUrl("https://docs.example.com/api/v1/"))
        assertEquals("https://x.example.com/docveta", normalizeServerUrl("x.example.com/docveta"))
        assertNull(normalizeServerUrl("   "))
    }
}

class ModelsTest {
    @Test
    fun readsADocumentTheServerSent() {
        val json = """{"items":[{"id":"1","space":{"id":"s","name":"Family"},"title":"Rent","document_date":"2026-04-01",
            "tags":[{"id":"t","name":"Home","color":"green"}],"status":"ready","mime_type":"application/pdf","size_bytes":10,
            "future_field":{"a":1},"custom_fields":[{"field_id":"f","name":"Amount","data_type":"monetary","value":18250.5}],
            "snippet":[{"text":"pay "},{"text":"rent","hit":true}]}],"total":1,"next_cursor":null}"""
        val l = AppJson.decodeFromString<DocumentList>(json)
        val d: Document = l.items.single()
        assertEquals("Rent", d.title)
        assertEquals("Home", d.tags.single().name)
        assertTrue(d.isPdf)
        assertEquals(1, l.total)
        assertNull(l.nextCursor)
        assertTrue(d.snippet[1].hit)
    }
}

class ApiTest {
    private lateinit var server: MockWebServer
    private val session = SessionStore(MemoryStore())

    @Before
    fun up() {
        server = MockWebServer()
        server.start()
        session.serverUrl = server.url("/").toString().trimEnd('/')
        session.token = "dvt_secret"
    }

    @After
    fun down() = server.shutdown()

    @Test
    fun sendsTheTokenAndTheOriginTheServerChecks() = runBlocking {
        server.enqueue(MockResponse().setBody("""{"id":"x","email":"a@b.c","display_name":"A"}"""))
        val api = ApiClient(session)
        val me = api.get<app.docveta.android.data.Me>("/me")
        assertEquals("A", me.displayName)
        val r = server.takeRequest()
        assertEquals("Bearer dvt_secret", r.getHeader("Authorization"))
        assertEquals("http://${server.hostName}:${server.port}", r.getHeader("Origin")) // the host name differs between machines
        assertEquals("/api/v1/me", r.path)
    }

    @Test
    fun turnsProblemsIntoReadableErrors() = runBlocking {
        server.enqueue(MockResponse().setResponseCode(422).setBody("""{"title":"Enter a name","code":"validation","errors":[{"field":"name","message":"Required"}]}"""))
        try {
            ApiClient(session).get<app.docveta.android.data.Me>("/me")
            fail("should have thrown")
        } catch (e: ApiException) {
            assertEquals(422, e.status)
            assertEquals("Enter a name", e.message)
            assertEquals("Required", e.fieldMessage("name"))
        }
    }

    @Test
    fun forgetsTheSignInWhenTheServerRefusesTheToken() = runBlocking {
        server.enqueue(MockResponse().setResponseCode(401).setBody("""{"title":"Please sign in","code":"unauthorized"}"""))
        var told = false
        val api = ApiClient(session, onUnauthorized = { told = true })
        try {
            api.get<app.docveta.android.data.Me>("/me")
        } catch (_: ApiException) {
        }
        assertTrue(told)
    }

    @Test
    fun reportsANetworkFailureAsStatusZero() = runBlocking {
        server.shutdown()
        try {
            ApiClient(session).get<app.docveta.android.data.Me>("/me")
            fail("should have thrown")
        } catch (e: ApiException) {
            assertTrue(e.isNetwork)
        }
    }
}

class TusTest {
    private lateinit var server: MockWebServer
    private val session = SessionStore(MemoryStore())
    private lateinit var file: File

    @Before
    fun up() {
        server = MockWebServer()
        server.start()
        session.serverUrl = server.url("/").toString().trimEnd('/')
        session.token = "t"
        file = File.createTempFile("scan", ".pdf").apply { writeBytes(ByteArray(10) { it.toByte() }); deleteOnExit() }
    }

    @After
    fun down() = server.shutdown()

    @Test
    fun uploadsInChunksAndReturnsTheDocumentId() = runBlocking {
        var offset = 0L
        server.dispatcher = object : Dispatcher() {
            override fun dispatch(request: RecordedRequest): MockResponse = when (request.method) {
                "POST" -> MockResponse().setResponseCode(201).setHeader("Location", "/api/v1/uploads/u1")
                "PATCH" -> {
                    assertEquals(offset, request.getHeader("Upload-Offset")!!.toLong())
                    offset += request.bodySize
                    MockResponse().setResponseCode(204).setHeader("Upload-Offset", offset.toString()).apply { if (offset >= 10) setHeader("Docveta-Document-Id", "doc-42") }
                }
                else -> MockResponse().setResponseCode(404)
            }
        }
        val progress = ArrayList<Long>()
        val id = TusUploader(ApiClient(session), chunkSize = 4).upload(file, mapOf("filename" to "scan.pdf", "space_id" to "s1")) { sent, _ -> progress.add(sent) }
        assertEquals("doc-42", id)
        assertEquals(10L, progress.last())
        val create = server.takeRequest()
        assertEquals("10", create.getHeader("Upload-Length"))
        assertTrue(create.getHeader("Upload-Metadata")!!.contains("filename "))
        assertEquals(3, generateSequence { server.takeRequest(10, java.util.concurrent.TimeUnit.MILLISECONDS) }.count()) // 4 + 4 + 2 bytes
    }

    @Test
    fun resumesFromWhatTheServerAlreadyHas() = runBlocking {
        val patches = ArrayList<Pair<Long, Long>>()
        server.dispatcher = object : Dispatcher() {
            override fun dispatch(request: RecordedRequest): MockResponse = when (request.method) {
                "HEAD" -> MockResponse().setResponseCode(200).setHeader("Upload-Offset", "6")
                "PATCH" -> {
                    val off = request.getHeader("Upload-Offset")!!.toLong()
                    patches.add(off to request.bodySize)
                    MockResponse().setResponseCode(204).setHeader("Upload-Offset", (off + request.bodySize).toString()).setHeader("Docveta-Document-Id", "doc-7")
                }
                else -> MockResponse().setResponseCode(500)
            }
        }
        val id = TusUploader(ApiClient(session), chunkSize = 100).upload(file, emptyMap(), uploadUrl = session.serverUrl + "/api/v1/uploads/old")
        assertEquals("doc-7", id)
        assertEquals(listOf(6L to 4L), patches) // only the missing four bytes were sent
    }

    @Test
    fun startsOverWhenTheServerForgotTheUpload() = runBlocking {
        val seen = ArrayList<String>()
        server.dispatcher = object : Dispatcher() {
            override fun dispatch(request: RecordedRequest): MockResponse {
                seen.add(request.method!!)
                return when (request.method) {
                    "HEAD" -> MockResponse().setResponseCode(404)
                    "POST" -> MockResponse().setResponseCode(201).setHeader("Location", server.url("/api/v1/uploads/new").toString())
                    else -> MockResponse().setResponseCode(204).setHeader("Upload-Offset", "10").setHeader("Docveta-Document-Id", "d")
                }
            }
        }
        TusUploader(ApiClient(session)).upload(file, emptyMap(), uploadUrl = session.serverUrl + "/api/v1/uploads/gone")
        assertEquals(listOf("HEAD", "POST", "PATCH"), seen)
    }

    @Test
    fun surfacesADuplicateAsAnErrorTheQueueCanShow() = runBlocking {
        server.dispatcher = object : Dispatcher() {
            override fun dispatch(request: RecordedRequest): MockResponse = when (request.method) {
                "POST" -> MockResponse().setResponseCode(201).setHeader("Location", "/api/v1/uploads/u")
                else -> MockResponse().setResponseCode(409).setBody("""{"title":"You already have this document","code":"duplicate_document","extra":{"document_id":"abc","title":"Rent 2026"}}""")
            }
        }
        try {
            TusUploader(ApiClient(session)).upload(file, emptyMap())
            fail("should have thrown")
        } catch (e: ApiException) {
            assertEquals("duplicate_document", e.code)
            assertEquals("abc", e.extra["document_id"])
        }
    }
}
