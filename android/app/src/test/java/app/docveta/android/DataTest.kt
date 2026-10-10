package app.docveta.android

import app.docveta.android.data.ApiClient
import app.docveta.android.data.ApiException
import app.docveta.android.data.AppJson
import app.docveta.android.data.Document
import app.docveta.android.data.DocumentList
import app.docveta.android.data.MemoryStore
import app.docveta.android.data.SessionStore
import app.docveta.android.data.normalizeServerUrl
import app.docveta.android.data.pkceChallenge
import app.docveta.android.data.zip
import app.docveta.android.upload.TusUploader
import java.io.File
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.async
import kotlinx.coroutines.cancelAndJoin
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
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

class MarkdownTest {
    @Test
    fun parsesTheBlocksModelsWrite() {
        val md = """Your bill is due on **5 August** [1].

- Amount: ₹1,842 [1]
- Number: `1234`

| Month | Amount |
|---|---|
| July | 1,620 |

```
code
```"""
        val blocks = app.docveta.android.ui.parseMarkdown(md)
        assertEquals(4, blocks.size)
        assertTrue(blocks[0] is app.docveta.android.ui.MdBlock.Para)
        assertEquals(listOf("Amount: ₹1,842 [1]", "Number: `1234`"), (blocks[1] as app.docveta.android.ui.MdBlock.ListBlock).items)
        val table = blocks[2] as app.docveta.android.ui.MdBlock.Table
        assertEquals(listOf("Month", "Amount"), table.head)
        assertEquals(listOf(listOf("July", "1,620")), table.rows)
        assertEquals("code", (blocks[3] as app.docveta.android.ui.MdBlock.Code).text)
    }
}

class PkceTest {
    @Test
    fun isBase64UrlSha256LikeTheServer() { // value from Go oauth2.S256ChallengeFromVerifier / Python hashlib
        assertEquals("UQUQZVXmrIBVkFPCR2BV5cEdaWlPA652WGZSUxI0uxs", pkceChallenge("dBjftJeZ4CVP-mJ92K1D_JKKzM8B9U7gbKvqzRZgd4E"))
    }
}

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
    fun theSameFileFetchedTwiceAtOnceArrivesOnce(): Unit = runBlocking {
        // Opening a document used to fetch its file twice; the second fetch then failed to save
        // it ("Something went wrong, try again") although the first had put it in place.
        val bytes = ByteArray(300_000) { (it % 251).toByte() }
        repeat(2) { server.enqueue(MockResponse().setBody(okio.Buffer().write(bytes)).throttleBody(60_000, 100, java.util.concurrent.TimeUnit.MILLISECONDS)) }
        val dir = java.nio.file.Files.createTempDirectory("docveta-dl").toFile()
        val dest = File(dir, "doc.pdf")
        val api = ApiClient(session)
        listOf(1, 2).map { async(Dispatchers.IO) { api.download("/documents/x/file", emptyMap(), dest) } }.forEach { it.await() }
        assertTrue(dest.readBytes().contentEquals(bytes))
        assertEquals(listOf("doc.pdf"), dir.list()!!.toList()) // no half-files left behind
        dir.deleteRecursively()
    }

    @Test
    fun asksForAZipOfSeveralDocumentsWithAPost(): Unit = runBlocking {
        server.enqueue(MockResponse().setHeader("Content-Type", "application/zip").setBody("PK-zip-bytes"))
        val cache = java.nio.file.Files.createTempDirectory("docveta-zip").toFile()
        val repo = app.docveta.android.data.Repository(ApiClient(session), session, cache)
        val f = repo.zip(listOf("a", "b"))
        assertEquals("PK-zip-bytes", f.readText())
        assertTrue(f.name.startsWith("docveta-documents-") && f.name.endsWith(".zip"))
        val r = server.takeRequest()
        assertEquals("POST", r.method)
        assertEquals("/api/v1/documents/archive", r.path)
        assertEquals("""{"ids":["a","b"]}""", r.body.readUtf8())
        cache.deleteRecursively()
    }

    @Test
    fun leavingStopsADownloadWithoutAnError(): Unit = runBlocking {
        server.enqueue(MockResponse().setBody(okio.Buffer().write(ByteArray(2_000_000))).throttleBody(20_000, 100, java.util.concurrent.TimeUnit.MILLISECONDS))
        val dir = java.nio.file.Files.createTempDirectory("docveta-dl").toFile()
        val dest = File(dir, "big.pdf")
        var failure: Throwable? = null
        val job = launch(Dispatchers.IO) {
            try {
                ApiClient(session).download("/documents/x/file", emptyMap(), dest)
            } catch (e: ApiException) {
                failure = e
            }
        }
        delay(400)
        val started = System.currentTimeMillis()
        job.cancelAndJoin()
        assertTrue("the download went on after it was cancelled", System.currentTimeMillis() - started < 3000) // all of it would take 10 s
        assertNull(failure)
        assertTrue(dir.list()!!.isEmpty())
        dir.deleteRecursively()
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
