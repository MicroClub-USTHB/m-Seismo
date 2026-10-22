package org.microclub.mseismo

import android.app.Application
import androidx.test.core.app.ApplicationProvider
import java.io.File
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.json.JSONObject
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.Shadows.shadowOf
import org.robolectric.annotation.Config

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
        val first = MSeismo.loadInstallationId(app)
        assertEquals(first, MSeismo.loadInstallationId(app))
        assertEquals(first, File(app.noBackupFilesDir, MSeismo.INSTALLATION_ID_FILE).readText())
    }

    @Test
    fun corruptInstallationIdIsReplaced() {
        File(app.noBackupFilesDir, MSeismo.INSTALLATION_ID_FILE).writeText("3f2a-cut-short")
        val id = MSeismo.loadInstallationId(app)
        assertEquals(36, id.length)
        assertEquals(id, MSeismo.loadInstallationId(app))
    }

    @Test
    fun releaseFallsBackWhenVersionNameIsMissingOrTooLong() {
        val info = app.packageManager.getPackageInfo(app.packageName, 0)

        info.versionName = null
        shadowOf(app.packageManager).installPackage(info)
        assertEquals("unknown", MSeismo.releaseOf(app).version)

        info.versionName = "1.2.3-" + "x".repeat(100)
        shadowOf(app.packageManager).installPackage(info)
        assertEquals(MAX_FIELD_LENGTH, MSeismo.releaseOf(app).version.length)
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
        val cache = MSeismo.FirstUse("test") { calls++; "value" }
        assertNull(cache.get(null))
        assertEquals("value", cache.get(app))
        assertEquals("value", cache.get(app))
        assertEquals(1, calls)
    }

    @Test
    fun firstUseWaitsBeforeRetryingAFailure() {
        var calls = 0
        val cache = MSeismo.FirstUse<String>("test", retryMs = 60_000) { calls++; error("disk unavailable") }
        assertNull(cache.get(app))
        assertNull(cache.get(app))
        assertEquals(1, calls)
    }

    @Test
    fun firstUseRetriesAFailureOnceTheWaitIsOver() {
        var calls = 0
        val cache = MSeismo.FirstUse("test", retryMs = 0) { if (calls++ == 0) error("disk unavailable") else "value" }
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
        MSeismo.send = { _, _, reportJson ->
            json = JSONObject(reportJson)
            sent.countDown()
            SendResult.Accepted
        }
        MSeismo.configure(app, valid)
        val installationId = MSeismo.installationId

        val id = MSeismo.report(title = "Broken", severity = Severity.HIGH)
        // A failed configure right after report() must not drop or change that report.
        MSeismo.configure(app, Configuration(endpoint = "not a url", appKey = "nope"))

        assertTrue(sent.await(5, TimeUnit.SECONDS))
        assertEquals(id, json.getString("id"))
        assertEquals("manual", json.getString("trigger"))
        assertEquals("Broken", json.getJSONObject("user_input").getString("title"))
        assertEquals("high", json.getJSONObject("user_input").getString("severity"))
        assertEquals(installationId, json.getString("installation_id"))
        assertEquals(MSeismo.sessionId, json.getString("session_id"))
    }

    private val valid = Configuration(endpoint = "https://seismo.example.com", appKey = "msk_test")
}
