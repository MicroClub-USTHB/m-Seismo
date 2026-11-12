package org.microclub.mseismo

import org.json.JSONArray
import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import androidx.test.core.app.ApplicationProvider
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import java.io.File

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class ReportTest {
    private val report = Report(
        id = "0192a5f0-7c3e-7d4a-9b1e-3f6a2c8d4e51",
        installationId = "5b8f2c1e-9a4d-4e7b-8c3f-1d2e6a9b0c47",
        sessionId = "c41d9e2a-6b7f-4a3c-9e8d-2f1a5b6c7d80",
        occurredAtMillis = 1_760_796_192_123, // 2025-10-18T14:03:12.123Z
        environment = "staging",
        release = Release("2.4.0", "412"),
        trigger = Trigger.MANUAL,
        autoSubmitted = false,
        userInput = UserInput("Checkout button does nothing", "Tapped Pay twice.", Category.BUG, Severity.HIGH),
        device = Device("Android", "15", "Google Pixel 8", "fr-DZ"),
    )

    @Test
    fun jsonHasTheSchemaFieldsAndValues() {
        val json = JSONObject(report.toJson())
        val expected = JSONObject(
            """
            {
              "schema_version": 1,
              "id": "0192a5f0-7c3e-7d4a-9b1e-3f6a2c8d4e51",
              "installation_id": "5b8f2c1e-9a4d-4e7b-8c3f-1d2e6a9b0c47",
              "session_id": "c41d9e2a-6b7f-4a3c-9e8d-2f1a5b6c7d80",
              "occurred_at": "2025-10-18T14:03:12.123Z",
              "platform": "android",
              "sdk": { "name": "mseismo-android", "version": "${MSeismo.SDK_VERSION}" },
              "environment": "staging",
              "release": { "version": "2.4.0", "build": "412" },
              "trigger": "manual",
              "auto_submitted": false,
              "user_input": {
                "title": "Checkout button does nothing",
                "description": "Tapped Pay twice.",
                "category": "bug",
                "severity": "high"
              },
              "device": { "os_name": "Android", "os_version": "15", "model": "Google Pixel 8", "locale": "fr-DZ" },
              "bodies_captured": false
            }
            """,
        )
        assertEquals(expected.toMap(), json.toMap())
    }

    @Test
    fun jsonFollowsTheSpecSchema() {
        val schema = JSONObject(File("../../../spec/report.schema.json").readText())
        val properties = schema.getJSONObject("properties")
        val json = JSONObject(report.toJson())

        assertFieldsAllowed(json, properties, schema.getJSONArray("required"))
        val device = properties.getJSONObject("device")
        assertFieldsAllowed(json.getJSONObject("device"), device.getJSONObject("properties"), device.getJSONArray("required"))
        assertFieldsAllowed(json.getJSONObject("user_input"), properties.getJSONObject("user_input").getJSONObject("properties"), JSONArray())

        assertTrue(properties.getJSONObject("platform").getJSONArray("enum").contains("android"))
        val triggers = properties.getJSONObject("trigger").getJSONArray("enum")
        Trigger.entries.forEach { assertTrue("trigger $it", triggers.contains(it.name.lowercase())) }
        val userInput = properties.getJSONObject("user_input").getJSONObject("properties")
        Category.entries.forEach { assertTrue("category $it", userInput.getJSONObject("category").getJSONArray("enum").contains(it.name.lowercase())) }
        Severity.entries.forEach { assertTrue("severity $it", userInput.getJSONObject("severity").getJSONArray("enum").contains(it.name.lowercase())) }
        assertTrue(Regex("^\\d{4}-\\d{2}-\\d{2}T\\d{2}:\\d{2}:\\d{2}\\.\\d{3}Z$").matches(json.getString("occurred_at")))
    }

    @Test
    fun emptyUserInputIsLeftOut() {
        assertFalse(JSONObject(report.copy(userInput = null).toJson()).has("user_input"))
    }

    @Test
    fun textOverTheLimitIsCut() {
        val long = report.copy(userInput = UserInput(title = "x".repeat(300), description = "é".repeat(20_000)))
        val input = JSONObject(long.toJson()).getJSONObject("user_input")
        assertEquals(200, input.getString("title").length)
        assertEquals(10_000, input.getString("description").length)
    }

    @Test
    fun limitCountsCharactersNotUtf16Units() {
        val emoji = "😀".repeat(5) // 2 UTF-16 units each
        assertEquals("😀".repeat(3), emoji.limit(3))
        assertEquals("abc", "abc".limit(3))
    }

    private val full = report.copy(
        device = Device("Android", "15", "Google Pixel 8", "fr-DZ", 0.62, 1_073_741_824, "wifi", "portrait"),
        logs = listOf(LogLine(1_760_796_181_004, "Cart loaded with 3 items")),
        breadcrumbs = BreadcrumbKind.entries.map { Breadcrumb(1_760_796_175_512, it, "CartView") },
        networkRequests = listOf(NetworkRequest(1_760_796_189_866, "https://api.example.com/v1/payments", "POST", 502, 120, "{}", "bad gateway")),
        bodiesCaptured = true,
    )

    @Test
    fun currentDeviceFollowsTheSchema() {
        val device = Device.current(ApplicationProvider.getApplicationContext())
        val schema = JSONObject(File("../../../spec/report.schema.json").readText())
            .getJSONObject("properties").getJSONObject("device").getJSONObject("properties")
        assertEquals("Android", device.osName)
        assertTrue(device.osVersion.isNotEmpty() && device.model.isNotEmpty())
        assertTrue(schema.getJSONObject("network_type").getJSONArray("enum").contains(device.networkType!!))
        assertTrue(schema.getJSONObject("orientation").getJSONArray("enum").contains(device.orientation!!))
        device.batteryLevel?.let { assertTrue(it in 0.0..1.0) }
    }

    @Test
    fun contextFollowsTheSpecSchema() {
        val properties = JSONObject(File("../../../spec/report.schema.json").readText()).getJSONObject("properties")
        val json = JSONObject(full.toJson())

        for (name in listOf("logs", "breadcrumbs", "network_requests")) {
            val items = properties.getJSONObject(name).getJSONObject("items")
            val array = json.getJSONArray(name)
            for (i in 0 until array.length()) assertFieldsAllowed(array.getJSONObject(i), items.getJSONObject("properties"), items.getJSONArray("required"))
        }
        val kinds = properties.getJSONObject("breadcrumbs").getJSONObject("items").getJSONObject("properties").getJSONObject("kind").getJSONArray("enum")
        BreadcrumbKind.entries.forEach { assertTrue("kind $it", kinds.contains(it.name.lowercase())) }
        assertFieldsAllowed(json.getJSONObject("device"), properties.getJSONObject("device").getJSONObject("properties"), JSONArray())
        assertEquals("bad gateway", json.getJSONArray("network_requests").getJSONObject(0).getString("response_body"))
        assertTrue(json.getBoolean("bodies_captured"))
    }

    @Test
    fun bodiesAreLeftOutUnlessCaptured() {
        val request = JSONObject(full.copy(bodiesCaptured = false).toJson()).getJSONArray("network_requests").getJSONObject(0)
        assertFalse(request.has("request_body") || request.has("response_body"))
    }

    @Test
    fun aReportOverTheSizeLimitShedsContextUntilItFits() {
        val huge = full.copy(
            logs = List(200) { LogLine(1L, "x".repeat(4_000)) },
            networkRequests = List(50) { NetworkRequest(1L, "https://a.b/c", "GET", 200, 1, "y".repeat(16_000), "z".repeat(16_000)) },
        )
        val json = huge.toJsonUnder()
        assertTrue(json.toByteArray().size <= 900_000)
        assertEquals(huge.id, JSONObject(json).getString("id"))
    }

    @Test
    fun crashFollowsTheSpecSchema() {
        val crash = report.copy(trigger = Trigger.CRASH, autoSubmitted = true, userInput = null, crash = Crash.of(IllegalStateException("cart is empty")))
        val json = JSONObject(crash.toJson()).getJSONObject("crash")
        val schema = JSONObject(File("../../../spec/report.schema.json").readText()).getJSONObject("properties").getJSONObject("crash")

        assertFieldsAllowed(json, schema.getJSONObject("properties"), schema.getJSONArray("required"))
        assertEquals("java.lang.IllegalStateException", json.getString("type"))
        assertEquals("cart is empty", json.getString("reason"))
        assertTrue(json.getJSONArray("stack_trace").getString(0).startsWith("org.microclub.mseismo.ReportTest.crashFollowsTheSpecSchema"))
    }

    @Test
    fun crashStackListsItsCausesAndStopsAtTheLimit() {
        val cause = IllegalArgumentException("bad id")
        val error = RuntimeException("could not load the cart", cause)
        cause.initCause(error) // a loop: must not run forever

        val stack = Crash.of(error).stackTrace
        assertTrue("Caused by: java.lang.IllegalArgumentException: bad id" in stack)
        assertEquals(1, stack.count { it.startsWith("Caused by:") })

        val deep = RuntimeException("deep").apply { stackTrace = Array(2_000) { StackTraceElement("A", "f", "A.kt", it) } }
        assertEquals(512, Crash.of(deep).stackTrace.size)
    }

    @Test
    fun aCrashWithoutAStackStillHasOneLine() {
        val error = RuntimeException("no stack").apply { stackTrace = emptyArray() }
        assertEquals(listOf("java.lang.RuntimeException: no stack"), Crash.of(error).stackTrace)
    }

    private fun assertFieldsAllowed(json: JSONObject, allowed: JSONObject, required: JSONArray) {
        for (i in 0 until required.length()) assertTrue("missing ${required.getString(i)}", json.has(required.getString(i)))
        json.keys().forEach { assertTrue("$it is not in the schema", allowed.has(it)) }
    }

    private fun JSONArray.contains(value: String) = (0 until length()).any { getString(it) == value }

    private fun JSONObject.toMap(): Map<String, Any?> =
        keys().asSequence().associateWith { key -> get(key).let { if (it is JSONObject) it.toMap() else it } }
}
