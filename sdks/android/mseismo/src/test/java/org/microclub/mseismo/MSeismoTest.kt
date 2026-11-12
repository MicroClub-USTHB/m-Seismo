package org.microclub.mseismo

import android.app.Activity
import android.app.Application
import android.content.ComponentCallbacks2
import android.os.Looper
import androidx.test.core.app.ApplicationProvider
import org.json.JSONArray
import org.json.JSONObject
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.Robolectric
import org.robolectric.RobolectricTestRunner
import org.robolectric.Shadows.shadowOf
import org.robolectric.annotation.Config
import java.io.File
import java.time.Duration
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicInteger

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class MSeismoTest {
    private val app: Application = ApplicationProvider.getApplicationContext()
    private val realSend = MSeismo.send

    @After
    fun restoreSend() {
        MSeismo.send = realSend
    }

    @Test
    fun installationIdIsCreatedOnceAndKeptOutOfBackups() {
        val first = loadInstallationId(app)
        assertEquals(first, loadInstallationId(app))
        assertEquals(first, File(app.noBackupFilesDir, INSTALLATION_ID_FILE).readText())
    }

    @Test
    fun corruptInstallationIdIsReplaced() {
        File(app.noBackupFilesDir, INSTALLATION_ID_FILE).writeText("3f2a-cut-short")
        val id = loadInstallationId(app)
        assertEquals(36, id.length)
        assertEquals(id, loadInstallationId(app))
    }

    @Test
    fun releaseFallsBackWhenVersionNameIsMissingOrTooLong() {
        val info = app.packageManager.getPackageInfo(app.packageName, 0)

        info.versionName = null
        shadowOf(app.packageManager).installPackage(info)
        assertEquals("unknown", releaseOf(app).version)

        info.versionName = "1.2.3-" + "x".repeat(100)
        shadowOf(app.packageManager).installPackage(info)
        assertEquals(MAX_FIELD_LENGTH, releaseOf(app).version.length)
    }

    @Test
    fun configureNeverThrowsAndTurnsOffOnInvalidConfiguration() {
        MSeismo.configure(app, valid)
        MSeismo.configure(app, Configuration(endpoint = "not a url", appKey = "nope"))
        assertNull(MSeismo.configuration)
        assertNull(MSeismo.installationId)
        assertNull(MSeismo.release)
    }

    @Test
    fun firstUseLoadsOnceAndKeepsTheValue() {
        var calls = 0
        val cache = FirstUse("test") { calls++; "value" }
        assertNull(cache.get(null))
        assertEquals("value", cache.get(app))
        assertEquals("value", cache.get(app))
        assertEquals(1, calls)
    }

    @Test
    fun firstUseWaitsBeforeRetryingAFailure() {
        var calls = 0
        val cache = FirstUse<String>("test", retryMs = 60_000) { calls++; error("disk unavailable") }
        assertNull(cache.get(app))
        assertNull(cache.get(app))
        assertEquals(1, calls)
    }

    @Test
    fun firstUseRetriesAFailureOnceTheWaitIsOver() {
        var calls = 0
        val cache = FirstUse("test", retryMs = 0) { if (calls++ == 0) error("disk unavailable") else "value" }
        assertNull(cache.get(app))
        assertEquals("value", cache.get(app))
        assertEquals(2, calls)
    }

    @Test
    fun configureProvidesReleaseAndInstallationId() {
        MSeismo.configure(app, valid)
        assertEquals(valid, MSeismo.configuration)
        assertNotNull(MSeismo.release)
        assertNotNull(MSeismo.installationId)
    }

    @Test
    fun configuredReleaseWinsOverPackageRelease() {
        val release = Release("9.9.9", "99")
        MSeismo.configure(app, valid.copy(release = release))
        assertEquals(release, MSeismo.release)
    }

    @Test
    fun reportIsIgnoredWhileOff() {
        MSeismo.configure(app, Configuration(endpoint = "not a url", appKey = "nope"))
        assertNull(MSeismo.report(title = "x"))
    }

    @Test
    fun reportIsSentInTheBackground() {
        val sent = CountDownLatch(1)
        var json = JSONObject()
        MSeismo.send = { _, _, reportJson, _ ->
            json = JSONObject(reportJson)
            sent.countDown()
            SendResult.Accepted
        }
        MSeismo.configure(app, valid)
        val installationId = MSeismo.installationId

        val id = MSeismo.report(title = "Broken", severity = Severity.HIGH)

        assertTrue(sent.await(5, TimeUnit.SECONDS))
        assertEquals(id, json.getString("id"))
        assertEquals("manual", json.getString("trigger"))
        assertEquals("Broken", json.getJSONObject("user_input").getString("title"))
        assertEquals("high", json.getJSONObject("user_input").getString("severity"))
        assertEquals(installationId, json.getString("installation_id"))
        assertEquals(MSeismo.sessionId, json.getString("session_id"))
    }

    @Test
    fun failedReportIsKeptAndSentByItselfLater() {
        val attempts = AtomicInteger()
        val failed = CountDownLatch(1)
        val accepted = CountDownLatch(1)
        var acceptedId = ""
        MSeismo.send = { _, _, reportJson, _ ->
            if (attempts.getAndIncrement() == 0) {
                failed.countDown()
                SendResult.Retry("offline")
            } else {
                acceptedId = JSONObject(reportJson).getString("id")
                accepted.countDown()
                SendResult.Accepted
            }
        }
        MSeismo.configure(app, valid)

        val id = MSeismo.report(title = "Sent offline")

        assertTrue(failed.await(5, TimeUnit.SECONDS))
        assertTrue(File(app.noBackupFilesDir, "mseismo-queue/$id.json").exists())
        // Robolectric's clock is frozen: advance it past the retry wait.
        var waited = 0
        while (!accepted.await(200, TimeUnit.MILLISECONDS)) {
            assertTrue("not retried", ++waited < 50)
            shadowOf(Looper.getMainLooper()).idleFor(Duration.ofSeconds(1))
        }
        assertEquals(id, acceptedId)
    }

    @Test
    fun reportIsStillSentWhenItCannotBeSaved() {
        val sent = CountDownLatch(1)
        MSeismo.send = { _, _, _, _ -> sent.countDown(); SendResult.Accepted }
        File(app.noBackupFilesDir, "mseismo-queue").writeText("a file where the folder should be")
        MSeismo.configure(app, valid)

        MSeismo.report(title = "Phone storage full")

        assertTrue(sent.await(5, TimeUnit.SECONDS))
    }

    @Test
    fun reportCarriesRedactedContext() {
        val sent = CountDownLatch(1)
        var json = JSONObject()
        MSeismo.send = { _, _, reportJson, _ -> json = JSONObject(reportJson); sent.countDown(); SendResult.Accepted }
        MSeismo.configure(app, valid)

        MSeismo.log("signed in as jane@example.com")
        MSeismo.breadcrumb(BreadcrumbKind.TAP, "Pay button")
        MSeismo.recordRequest("https://api.example.com/pay", "POST", 502, 120, requestBody = "card 4111 1111 1111 1111")
        MSeismo.report(title = "Pay fails")

        assertTrue(sent.await(5, TimeUnit.SECONDS))
        val logs = json.getJSONArray("logs").toString()
        assertTrue(logs, "signed in as [REDACTED]" in logs && "jane@example.com" !in logs)
        assertTrue("Pay button" in json.getJSONArray("breadcrumbs").toString())
        val request = json.getJSONArray("network_requests").let { it.getJSONObject(it.length() - 1) }
        assertEquals(502, request.getInt("status_code"))
        assertTrue("bodies are off by default", !request.has("request_body") && !json.getBoolean("bodies_captured"))
        assertTrue(json.getJSONObject("device").has("network_type"))
    }

    @Test
    fun hugeLogLinesAreCutBeforeBeingKeptInMemory() {
        MSeismo.log("x".repeat(1_000_000))
        assertEquals(4_000, MSeismo.logs.snapshot().last().message.length)
    }

    @Test
    fun screensAndBackgroundingAreRecorded() {
        MSeismo.configure(app, valid)
        Robolectric.buildActivity(Activity::class.java).setup()
        app.onTrimMemory(ComponentCallbacks2.TRIM_MEMORY_UI_HIDDEN)

        val sent = CountDownLatch(1)
        var crumbs = ""
        MSeismo.send = { _, _, reportJson, _ -> crumbs = JSONObject(reportJson).getJSONArray("breadcrumbs").toString(); sent.countDown(); SendResult.Accepted }
        MSeismo.report()
        assertTrue(sent.await(5, TimeUnit.SECONDS))
        assertTrue(crumbs, """"kind":"navigation","message":"Activity"""" in crumbs)
        assertTrue(crumbs, """"message":"background"""" in crumbs)
    }

    private val valid = Configuration(endpoint = "https://seismo.example.com", appKey = "msk_test")

    private val queueDir get() = File(app.noBackupFilesDir, "mseismo-queue")

    @Test
    fun aCrashIsSavedWithItsContextAndSentOnTheNextLaunch() {
        val sent = CountDownLatch(1)
        var json = JSONObject()
        MSeismo.send = { _, _, reportJson, _ ->
            JSONObject(reportJson).takeIf { it.getString("trigger") == "crash" }?.let { json = it; sent.countDown() }
            SendResult.Accepted
        }
        MSeismo.configure(app, valid)
        MSeismo.configure(app, valid) // twice: still one report per crash
        MSeismo.breadcrumb(BreadcrumbKind.TAP, "Pay button")

        crash(IllegalStateException("no account for ali@example.com"))

        assertEquals("saved right away, one file", 1, queueDir.listFiles { f -> f.name.endsWith(".json") }!!.size)
        MSeismo.configure(app, valid) // the next launch
        assertTrue(sent.await(5, TimeUnit.SECONDS))
        assertTrue(json.getBoolean("auto_submitted"))
        assertEquals("java.lang.IllegalStateException", json.getJSONObject("crash").getString("type"))
        assertEquals("no account for [REDACTED]", json.getJSONObject("crash").getString("reason"))
        assertTrue("what happened before the crash", json.getJSONArray("breadcrumbs").toString().contains("Pay button"))
    }

    @Test
    fun aCrashWhileTheSdkIsOffSavesNothing() {
        MSeismo.configure(app, valid)
        MSeismo.configure(app, Configuration(endpoint = "not a url", appKey = "nope"))

        crash(IllegalStateException("boom"))

        assertTrue(queueDir.listFiles().isNullOrEmpty())
    }

    @Test
    fun recordErrorSendsANonFatalReportRightAway() {
        val sent = CountDownLatch(1)
        var json = JSONObject()
        MSeismo.send = { _, _, reportJson, _ ->
            JSONObject(reportJson).takeIf { it.getString("trigger") == "non_fatal" }?.let { json = it; sent.countDown() }
            SendResult.Accepted
        }
        MSeismo.configure(app, valid)

        val id = MSeismo.recordError(IllegalArgumentException("bad coupon"))

        assertTrue(sent.await(5, TimeUnit.SECONDS))
        assertEquals(id, json.getString("id"))
        assertTrue(json.getBoolean("auto_submitted"))
        assertEquals("bad coupon", json.getJSONObject("crash").getString("reason"))
    }

    @Test
    fun composeScreensAreRecordedOnceEachTime() {
        val sent = CountDownLatch(1)
        var crumbs = JSONArray()
        MSeismo.send = { _, _, reportJson, _ ->
            JSONObject(reportJson).takeIf { it.getString("trigger") == "manual" }?.let { crumbs = it.getJSONArray("breadcrumbs"); sent.countDown() }
            SendResult.Accepted
        }
        MSeismo.configure(app, valid)

        listOf("Calls", "Calls", "Calls", "Local logs", "Calls").forEach(MSeismo::screen) // recompositions repeat it

        MSeismo.report()
        assertTrue(sent.await(5, TimeUnit.SECONDS))
        val screens = (0 until crumbs.length()).map { crumbs.getJSONObject(it).getString("message") }.filter { it == "Calls" || it == "Local logs" }
        assertEquals(listOf("Calls", "Local logs", "Calls"), screens)
    }
}
