package org.microclub.mseismo

import org.json.JSONObject
import java.io.ByteArrayOutputStream
import java.io.IOException
import java.net.HttpURLConnection
import java.net.URL
import java.util.UUID

internal sealed interface SendResult {
    data object Accepted : SendResult

    // 429, 5xx, no answer, or something other than the backend answering (e.g. a Wi-Fi login page).
    data class Retry(val reason: String, val retryAfterSeconds: Long? = null) : SendResult

    // Any other 4xx from the backend: retrying would fail the same way, so the report is dropped.
    data class Rejected(val status: Int, val code: String?, val message: String?) : SendResult
}

// Sends one report as multipart/form-data (spec/ingestion.md §1).
internal class ReportTransport(
    private val connectTimeoutMs: Int = 15_000,
    private val readTimeoutMs: Int = 30_000,
) {
    fun send(url: String, appKey: String, reportJson: String, screenshotJpeg: ByteArray? = null): SendResult {
        val boundary = "mseismo-" + UUID.randomUUID()
        val body = multipart(boundary, reportJson, screenshotJpeg)
        val connection = try {
            URL(url).openConnection() as HttpURLConnection
        } catch (e: IOException) {
            return SendResult.Retry("could not open a connection: ${e.message}")
        }
        return try {
            connection.requestMethod = "POST"
            connection.connectTimeout = connectTimeoutMs
            connection.readTimeout = readTimeoutMs
            connection.doOutput = true
            connection.instanceFollowRedirects = false
            connection.setFixedLengthStreamingMode(body.size)
            connection.setRequestProperty("Authorization", "Bearer $appKey")
            connection.setRequestProperty("Content-Type", "multipart/form-data; boundary=$boundary")
            connection.outputStream.use { it.write(body) }
            result(connection)
        } catch (e: IOException) {
            SendResult.Retry("network error: ${e.message}")
        } finally {
            connection.disconnect()
        }
    }

    private fun result(connection: HttpURLConnection): SendResult {
        val status = connection.responseCode
        if (status == 202) return SendResult.Accepted
        if (status == 429 || status !in 400..499) {
            return SendResult.Retry("HTTP $status", connection.getHeaderField("Retry-After")?.trim()?.toLongOrNull())
        }
        val error = runCatching {
            JSONObject(connection.errorStream?.bufferedReader()?.use { it.readText() }.orEmpty())
        }.getOrNull()
        val code = error?.optString("code")?.ifEmpty { null }
        // Backend errors always have a code; without one, a proxy or Wi-Fi portal answered.
        if (code == null) return SendResult.Retry("HTTP $status, not from the backend")
        return SendResult.Rejected(status, code, error.optString("message").ifEmpty { null })
    }

    private fun multipart(boundary: String, reportJson: String, screenshotJpeg: ByteArray?): ByteArray {
        val out = ByteArrayOutputStream()
        fun text(s: String) = out.write(s.toByteArray(Charsets.UTF_8))
        text("--$boundary\r\nContent-Disposition: form-data; name=\"report\"\r\nContent-Type: application/json\r\n\r\n")
        text(reportJson)
        if (screenshotJpeg != null) {
            text("\r\n--$boundary\r\nContent-Disposition: form-data; name=\"screenshot\"; filename=\"screenshot.jpg\"\r\n")
            text("Content-Type: image/jpeg\r\n\r\n")
            out.write(screenshotJpeg)
        }
        text("\r\n--$boundary--\r\n")
        return out.toByteArray()
    }
}
