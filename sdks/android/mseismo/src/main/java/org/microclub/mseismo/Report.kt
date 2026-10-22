package org.microclub.mseismo

import android.os.Build
import org.json.JSONObject
import java.text.SimpleDateFormat
import java.util.Date
import java.util.Locale
import java.util.TimeZone

public enum class Category { BUG, CRASH, UI, OTHER }

public enum class Severity { LOW, MEDIUM, HIGH, CRITICAL }

internal enum class Trigger { SHAKE, SCREENSHOT, LONG_PRESS, MANUAL, CRASH, NON_FATAL }

internal data class UserInput(
    val title: String? = null,
    val description: String? = null,
    val category: Category? = null,
    val severity: Severity? = null,
)

internal data class Device(
    val osName: String,
    val osVersion: String,
    val model: String,
    val locale: String?,
) {
    companion object {
        fun current(): Device = Device(
            osName = "Android",
            osVersion = Build.VERSION.RELEASE.orEmpty().ifEmpty { "unknown" },
            model = "${Build.MANUFACTURER} ${Build.MODEL}".trim().ifEmpty { "unknown" },
            locale = Locale.getDefault().toLanguageTag(),
        )
    }
}

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
) {
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
                input.title?.let { put("title", it.limit(200)) }
                input.description?.let { put("description", it.limit(10_000)) }
                input.category?.let { put("category", it.wire) }
                input.severity?.let { put("severity", it.wire) }
            })
        }
        put("device", JSONObject().apply {
            put("os_name", device.osName.limit(32))
            put("os_version", device.osVersion.limit(32))
            put("model", device.model.limit(64))
            device.locale?.let { put("locale", it.limit(35)) }
        })
    }.toString()
}

private val Enum<*>.wire: String get() = name.lowercase(Locale.ROOT)

internal fun String.limit(max: Int): String =
    if (codePointCount(0, length) <= max) this else substring(0, offsetByCodePoints(0, max))

internal fun timestamp(millis: Long): String =
    SimpleDateFormat("yyyy-MM-dd'T'HH:mm:ss.SSS'Z'", Locale.ROOT)
        .apply { timeZone = TimeZone.getTimeZone("UTC") }
        .format(Date(millis))
