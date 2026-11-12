package org.microclub.mseismo

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import java.io.File
import kotlin.random.Random

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class OfflineQueueTest {
    @get:Rule
    val folder = TemporaryFolder()

    private var clock = 1_000_000L
    private val sent = mutableListOf<String>()
    private fun queue(max: Int = 100) = OfflineQueue(File(folder.root, "queue"), { clock }, Random(0), max)
    private fun answer(result: SendResult): (String, ByteArray?) -> SendResult = { json, _ -> sent += json; result }
    private fun saved() = File(folder.root, "queue").list().orEmpty().sorted()

    @Test
    fun acceptedReportIsSentAndDeleted() {
        val q = queue()
        q.add("a", """{"id":"a"}""")
        assertEquals(listOf("a.json"), saved())

        assertNull(q.drain(answer(SendResult.Accepted)))
        assertEquals(listOf("""{"id":"a"}"""), sent)
        assertEquals(emptyList<String>(), saved())
    }

    @Test
    fun rejectedReportIsDeleted() {
        val q = queue()
        q.add("a", "{}")
        assertNull(q.drain(answer(SendResult.Rejected(400, "invalid_report", "bad"))))
        assertEquals(emptyList<String>(), saved())
    }

    @Test
    fun failedReportIsKeptAndRetriedAfterGrowingWaits() {
        val q = queue()
        q.add("a", "{}")

        val first = q.drain(answer(SendResult.Retry("offline")))!!
        assertTrue("first wait $first", first in 1_000..2_000)
        assertEquals(listOf("a.json"), saved())

        assertEquals(first, q.drain(answer(SendResult.Accepted))) // not due yet
        assertEquals(1, sent.size)

        clock += first
        val second = q.drain(answer(SendResult.Retry("offline")))!!
        assertTrue("second wait $second", second in 2_000..4_000)
        assertEquals(2, sent.size)

        clock += second
        assertNull(q.drain(answer(SendResult.Accepted)))
        assertEquals(emptyList<String>(), saved())
    }

    @Test
    fun retryAfterIsHonoured() {
        val q = queue()
        q.add("a", "{}")
        assertTrue(q.drain(answer(SendResult.Retry("HTTP 429", retryAfterSeconds = 60)))!! >= 60_000)
    }

    @Test
    fun waitsAreCappedAtFiveMinutes() {
        val q = queue()
        q.add("a", "{}")
        repeat(20) {
            val wait = q.drain(answer(SendResult.Retry("offline")))!!
            assertTrue("wait $wait", wait <= 300_000)
            clock += wait
        }
    }

    @Test
    fun nothingIsSentOrDeletedWhileOff() {
        val q = queue()
        q.add("a", "{}")
        assertNull(q.drain { _, _ -> null })
        assertEquals(listOf("a.json"), saved())
    }

    @Test
    fun reportsSurviveARestart() {
        queue().add("a", """{"id":"a"}""")
        assertNull(queue().drain(answer(SendResult.Accepted)))
        assertEquals(listOf("""{"id":"a"}"""), sent)
    }

    @Test
    fun oneFailingReportDoesNotBlockTheOthers() {
        val q = queue()
        q.add("a", "a")
        File(folder.root, "queue/a.json").setLastModified(1_000)
        q.add("b", "b")
        q.drain { json, _ -> sent += json; if (json == "a") SendResult.Retry("too large for a proxy") else SendResult.Accepted }
        assertEquals(listOf("a", "b"), sent) // oldest first, b not blocked
        assertEquals(listOf("a.json"), saved())
    }

    @Test
    fun oldestReportsAreDroppedWhenFull() {
        val q = queue(max = 2)
        for ((i, id) in listOf("a", "b", "c").withIndex()) {
            q.add(id, id)
            File(folder.root, "queue/$id.json").setLastModified(1_000L * (i + 1))
        }
        assertEquals(listOf("b.json", "c.json"), saved())
    }

    @Test
    fun retryNowStillRespectsRetryAfter() {
        val q = queue()
        q.add("a", "{}")
        q.drain(answer(SendResult.Retry("HTTP 429", retryAfterSeconds = 60)))
        q.retryNow()
        assertTrue(q.drain(answer(SendResult.Accepted))!! > 0)
        assertEquals(1, sent.size)
    }

    @Test
    fun anUnreadableReportIsSkippedAndTheOthersStillGo() {
        val q = queue()
        q.add("b", "b")
        File(folder.root, "queue/a.json").mkdir() // unreadable
        File(folder.root, "queue/a.json").setLastModified(1_000)
        q.drain(answer(SendResult.Accepted))
        assertEquals(listOf("b"), sent)
    }

    @Test
    fun anUnexpectedSendErrorIsRetriedLater() {
        val q = queue()
        q.add("a", "{}")
        assertTrue(q.drain { _, _ -> throw SecurityException("no INTERNET permission") }!! > 0)
        assertEquals(listOf("a.json"), saved())
    }

    @Test
    fun halfWrittenReportsAreCleanedUp() {
        File(folder.root, "queue").mkdirs()
        File(folder.root, "queue/a.tmp").writeText("{\"id\":")
        queue()
        assertEquals(emptyList<String>(), saved())
    }

    @Test
    fun aReportDeletedElsewhereIsForgotten() {
        val q = queue()
        q.add("a", "{}")
        q.drain(answer(SendResult.Retry("offline")))
        File(folder.root, "queue/a.json").delete()
        assertNull(q.drain(answer(SendResult.Accepted)))
    }

    @Test
    fun theScreenshotIsSavedSentAndDeletedWithItsReport() {
        val q = queue()
        q.add("a", "{}", byteArrayOf(1, 2, 3))
        assertEquals(listOf("a.jpg", "a.json"), saved())
        var screenshot: ByteArray? = null
        assertNull(q.drain { _, jpeg -> screenshot = jpeg; SendResult.Accepted })
        assertEquals(listOf<Byte>(1, 2, 3), screenshot?.toList())
        assertEquals(emptyList<String>(), saved())
    }

    @Test
    fun aScreenshotWhoseReportWasNeverSavedIsCleanedUp() {
        File(folder.root, "queue").mkdirs()
        File(folder.root, "queue/a.jpg").writeBytes(byteArrayOf(1)) // crash between the two saves
        queue()
        assertEquals(emptyList<String>(), saved())
    }

    @Test
    fun retryNowSkipsTheWait() {
        val q = queue()
        q.add("a", "{}")
        q.drain(answer(SendResult.Retry("offline")))
        q.retryNow()
        assertNull(q.drain(answer(SendResult.Accepted)))
        assertEquals(2, sent.size)
    }

    // An app that crashes on every launch, before the queue can send: the crash path must not fill the phone.
    @Test
    fun crashesSavedFromAnyThreadKeepTheQueueUnderItsLimit() {
        val dir = File(folder.root, "queue")
        listOf("a", "b", "c").forEachIndexed { i, id ->
            OfflineQueue.write(dir, id, """{"id":"$id"}""", maxReports = 2)
            File(dir, "$id.json").setLastModified(1_000L * (i + 1))
        }
        assertEquals(listOf("b.json", "c.json"), saved())
    }
}
