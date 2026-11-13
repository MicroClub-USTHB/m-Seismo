package org.microclub.mseismo

import org.json.JSONArray
import org.json.JSONObject
import java.text.SimpleDateFormat
import java.util.Date
import java.util.Locale
import java.util.TimeZone

// Text limits from spec/report.schema.json, in characters.
internal object MaxChars {
    const val TITLE = 200
    const val DESCRIPTION = 10_000
    const val LOG_MESSAGE = 4_000
    const val BREADCRUMB = 1_000
    const val URL = 2_048
    const val METHOD = 16
    const val BODY = 16_000
    const val OS_NAME = 32
    const val OS_VERSION = 32
    const val MODEL = 64
    const val LOCALE = 35
    const val CRASH_TYPE = 256
    const val CRASH_REASON = 4_000
    const val STACK_FRAME = 1_000
}

internal const val NO_STACK_TRACE_FRAME = "(no stack trace available)"
internal const val MAX_FEATURE_FLAGS = 200
internal const val MAX_EXPERIMENTS = 100
internal const val MAX_EXPERIMENT_VARIANT = 128
internal const val MAX_CRASHLYTICS_ID = 128

public enum class Category { BUG, CRASH, UI, OTHER }

public enum class Severity { LOW, MEDIUM, HIGH, CRITICAL }

public enum class Trigger { SHAKE, SCREENSHOT, LONG_PRESS, MANUAL, CRASH, NON_FATAL }

public data class Crash(val type: String, val reason: String?, val stackTrace: List<String>) {
    public companion object {
        internal const val MAX_FRAMES = 512

        public fun of(error: Throwable): Crash {
            val lines = mutableListOf<String>()
            val seen = HashSet<Throwable>() // a cause chain can loop
            var current: Throwable? = error
            while (current != null && seen.add(current) && lines.size < MAX_FRAMES) {
                if (current !== error) lines += "Caused by: $current"
                current.stackTrace.mapTo(lines) { it.toString() }
                current = current.cause
            }
            return Crash(error.javaClass.name, error.message, lines.take(MAX_FRAMES).ifEmpty { listOf(error.toString()) })
        }
    }
}

public data class AutoReport(
    val id: String,
    val title: String?,
    val description: String?,
    val category: Category?,
    val severity: Severity?,
    val trigger: Trigger,
    val occurredAtMillis: Long,
    val crash: Crash?,
)

internal data class UserInput(
    val title: String? = null,
    val description: String? = null,
    val category: Category? = null,
    val severity: Severity? = null,
)

// One report, as in spec/report.schema.json (version 1).
internal data class Report(
    val id: String,
    val installationId: String,
    val sessionId: String,
    val occurredAtMillis: Long,
    val environment: String,
    val release: Release,
    val trigger: Trigger,
    val autoSubmitted: Boolean,
    val userInput: UserInput?,
    val device: Device,
    val logs: List<LogLine> = emptyList(),
    val breadcrumbs: List<Breadcrumb> = emptyList(),
    val networkRequests: List<NetworkRequest> = emptyList(),
    val bodiesCaptured: Boolean = false,
    val crash: Crash? = null,
    val featureFlags: Map<String, FlagValue> = emptyMap(),
    val experiments: Map<String, String> = emptyMap(),
    val crashlyticsInstallationId: String? = null,
) {
    // Limits applied again here: redaction can make text longer.
    fun toJson(): String = JSONObject().apply {
        put("schema_version", 1)
        put("id", id)
        put("installation_id", installationId)
        put("session_id", sessionId)
        put("occurred_at", timestamp(occurredAtMillis))
        put("platform", "android")
        put("sdk", JSONObject().put("name", MSeismo.SDK_NAME).put("version", MSeismo.SDK_VERSION))
        put("environment", environment)
        put("release", JSONObject().put("version", release.version).put("build", release.build))
        put("trigger", trigger.wire)
        put("auto_submitted", autoSubmitted)
        userInput?.let { input ->
            put("user_input", JSONObject().apply {
                input.title?.let { put("title", it.limit(MaxChars.TITLE)) }
                input.description?.let { put("description", it.limit(MaxChars.DESCRIPTION)) }
                input.category?.let { put("category", it.wire) }
                input.severity?.let { put("severity", it.wire) }
            })
        }
        put("device", JSONObject().apply {
            put("os_name", device.osName.limit(MaxChars.OS_NAME))
            put("os_version", device.osVersion.limit(MaxChars.OS_VERSION))
            put("model", device.model.limit(MaxChars.MODEL))
            device.locale?.let { put("locale", it.limit(MaxChars.LOCALE)) }
            device.batteryLevel?.let { put("battery_level", it) }
            device.availableMemoryBytes?.let { put("available_memory_bytes", it) }
            device.networkType?.let { put("network_type", it) }
            device.orientation?.let { put("orientation", it) }
        })
        featureFlagsJson()?.let { put("feature_flags", it) }
        experimentsJson()?.let { put("experiments", it) }
        crashlyticsInstallationId?.takeIf { it.isNotEmpty() }?.let {
            put("crashlytics", JSONObject().put("installation_id", it.limit(MAX_CRASHLYTICS_ID)))
        }
        crash?.let { crash ->
            put("crash", JSONObject().apply {
                put("type", crash.type.ifEmpty { "unknown" }.limit(MaxChars.CRASH_TYPE))
                crash.reason?.let { put("reason", it.limit(MaxChars.CRASH_REASON)) }
                val frames = crash.stackTrace.take(Crash.MAX_FRAMES).ifEmpty { listOf(NO_STACK_TRACE_FRAME) }
                put("stack_trace", JSONArray(frames.map { it.limit(MaxChars.STACK_FRAME) }))
            })
        }
        if (logs.isNotEmpty()) put("logs", JSONArray(logs.takeLast(500).map {
            JSONObject().put("timestamp", timestamp(it.timestampMillis)).put("message", it.message.limit(MaxChars.LOG_MESSAGE))
        }))
        if (breadcrumbs.isNotEmpty()) put("breadcrumbs", JSONArray(breadcrumbs.takeLast(200).map {
            JSONObject().put("timestamp", timestamp(it.timestampMillis)).put("kind", it.kind.wire).put("message", it.message.limit(MaxChars.BREADCRUMB))
        }))
        if (networkRequests.isNotEmpty()) put("network_requests", JSONArray(networkRequests.takeLast(100).map {
            JSONObject().apply {
                put("timestamp", timestamp(it.timestampMillis))
                put("url", it.url.limit(MaxChars.URL))
                put("method", it.method.limit(MaxChars.METHOD).ifEmpty { "UNKNOWN" })
                it.statusCode?.takeIf { code -> code in 100..599 }?.let { code -> put("status_code", code) }
                put("duration_ms", maxOf(0, it.durationMs))
                if (bodiesCaptured) {
                    it.requestBody?.let { body -> put("request_body", body.limit(MaxChars.BODY)) }
                    it.responseBody?.let { body -> put("response_body", body.limit(MaxChars.BODY)) }
                }
            }
        }))
        put("bodies_captured", bodiesCaptured)
    }.toString()

    // The backend refuses reports over 1 MB: drop the bulkiest context until it fits.
    fun toJsonUnder(maxBytes: Int = 900_000): String {
        var report = this
        while (true) {
            val json = report.toJson()
            if (json.toByteArray().size <= maxBytes) return json
            report = with(report) {
                when {
                    bodiesCaptured -> copy(bodiesCaptured = false)
                    networkRequests.isNotEmpty() -> copy(networkRequests = networkRequests.drop(maxOf(1, networkRequests.size / 2)))
                    logs.isNotEmpty() -> copy(logs = logs.drop(maxOf(1, logs.size / 2)))
                    breadcrumbs.isNotEmpty() -> copy(breadcrumbs = breadcrumbs.drop(maxOf(1, breadcrumbs.size / 2)))
                    else -> return json
                }
            }
        }
    }

    private fun featureFlagsJson(): JSONObject? {
        val entries = featureFlags.entries
            .filter { it.key.isNotEmpty() }
            .filter {
                when (val value = it.value) {
                    is FlagValue.NumberValue -> value.value.isFinite()
                    else -> true
                }
            }
            .sortedBy { it.key }
            .take(MAX_FEATURE_FLAGS)
        if (entries.isEmpty()) return null
        return JSONObject().apply {
            for ((name, value) in entries) {
                put(name, when (value) {
                    is FlagValue.StringValue -> value.value
                    is FlagValue.NumberValue -> value.value
                    is FlagValue.BooleanValue -> value.value
                })
            }
        }
    }

    private fun experimentsJson(): JSONObject? {
        val entries = experiments.entries
            .filter { it.key.isNotEmpty() }
            .sortedBy { it.key }
            .take(MAX_EXPERIMENTS)
        if (entries.isEmpty()) return null
        return JSONObject().apply {
            for ((name, variant) in entries) put(name, variant.limit(MAX_EXPERIMENT_VARIANT))
        }
    }
}

private val Enum<*>.wire: String get() = name.lowercase(Locale.ROOT)

internal fun String.limit(max: Int): String =
    if (codePointCount(0, length) <= max) this else substring(0, offsetByCodePoints(0, max))

internal fun timestamp(millis: Long): String =
    SimpleDateFormat("yyyy-MM-dd'T'HH:mm:ss.SSS'Z'", Locale.ROOT)
        .apply { timeZone = TimeZone.getTimeZone("UTC") }
        .format(Date(millis))
