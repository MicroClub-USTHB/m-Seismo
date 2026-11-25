package org.microclub.mseismo

import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import java.net.InetAddress
import java.net.ServerSocket
import kotlin.concurrent.thread

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class ReportTransportTest {
    private val server = ServerSocket(0, 1, InetAddress.getLoopbackAddress())
    private val url = "http://127.0.0.1:${server.localPort}/api/v1/reports"
    private var headers = mapOf<String, String>()
    private var body = ""

    // Answers one request with the given status, headers and body, and records what it received.
    private fun answer(status: Int, responseBody: String = "", responseHeaders: Map<String, String> = emptyMap()) {
        thread(isDaemon = true) {
            server.accept().use { socket ->
                val input = socket.getInputStream().buffered()
                val lines = generateSequence { readLine(input) }.takeWhile { it.isNotEmpty() }.drop(1)
                headers = lines.associate { it.substringBefore(":").lowercase() to it.substringAfter(":").trim() }
                body = ByteArray(headers.getValue("content-length").toInt()).also { java.io.DataInputStream(input).readFully(it) }.decodeToString()
                val bytes = responseBody.toByteArray()
                val head = "HTTP/1.1 $status X\r\nContent-Length: ${bytes.size}\r\nConnection: close\r\n" +
                    responseHeaders.entries.joinToString("") { "${it.key}: ${it.value}\r\n" } + "\r\n"
                socket.getOutputStream().apply { write(head.toByteArray() + bytes); flush() }
            }
        }
    }

    private fun readLine(input: java.io.InputStream): String =
        generateSequence { input.read().takeIf { it != -1 } }
            .takeWhile { it != '\n'.code }.map { it.toChar() }.joinToString("").trimEnd('\r')

    @After
    fun stop() = server.close()

    @Test
    fun sendsMultipartWithBearerKey() {
        answer(202, """{"id":"x","status":"accepted"}""")
        assertEquals(SendResult.Accepted, ReportTransport().send(url, "msk_test", """{"id":"x"}"""))

        assertEquals("Bearer msk_test", headers["authorization"])
        val boundary = headers.getValue("content-type").substringAfter("multipart/form-data; boundary=")
        assertEquals(
            "--$boundary\r\nContent-Disposition: form-data; name=\"report\"\r\n" +
                "Content-Type: application/json\r\n\r\n{\"id\":\"x\"}\r\n--$boundary--\r\n",
            body,
        )
    }

    @Test
    fun theScreenshotIsSentAsASecondPart() {
        answer(202)
        assertEquals(SendResult.Accepted, ReportTransport().send(url, "msk_test", """{"id":"x"}""", byteArrayOf(0x41, 0x42)))

        val boundary = headers.getValue("content-type").substringAfter("multipart/form-data; boundary=")
        assertEquals(
            "--$boundary\r\nContent-Disposition: form-data; name=\"report\"\r\nContent-Type: application/json\r\n\r\n{\"id\":\"x\"}" +
                "\r\n--$boundary\r\nContent-Disposition: form-data; name=\"screenshot\"; filename=\"screenshot.jpg\"\r\n" +
                "Content-Type: image/jpeg\r\n\r\nAB\r\n--$boundary--\r\n",
            body,
        )
    }

    @Test
    fun badReportIsRejectedWithTheBackendCode() {
        answer(400, """{"code":"invalid_report","message":"occurred_at: bad"}""")
        assertEquals(SendResult.Rejected(400, "invalid_report", "occurred_at: bad"), ReportTransport().send(url, "msk_test", "{}"))
    }

    @Test
    fun errorsWithoutTheBackendsBodyAreRetried() {
        // A proxy asking for login, or a portal page: the backend never saw the report.
        answer(407, "<html>Proxy login</html>")
        val result = ReportTransport().send(url, "msk_test", "{}")
        assertTrue("$result", result is SendResult.Retry)
    }

    @Test
    fun rateLimitIsRetriedAfterRetryAfter() {
        answer(429, """{"code":"rate_limited","message":"slow down"}""", mapOf("Retry-After" to "7"))
        val result = ReportTransport().send(url, "msk_test", "{}")
        assertTrue("$result", result is SendResult.Retry && result.retryAfterSeconds == 7L)
    }

    @Test
    fun serverErrorIsRetried() {
        answer(503, """{"code":"unavailable","message":"later"}""")
        assertTrue(ReportTransport().send(url, "msk_test", "{}") is SendResult.Retry)
    }

    @Test
    fun answersThatAreNotTheBackendsAreRetried() {
        // A Wi-Fi login page answers 200 with HTML, or redirects.
        for (status in listOf(200, 302)) {
            val server = ServerSocket(0, 1, InetAddress.getLoopbackAddress())
            thread(isDaemon = true) {
                server.accept().use { it.getOutputStream().write("HTTP/1.1 $status X\r\nContent-Length: 0\r\nConnection: close\r\n\r\n".toByteArray()) }
            }
            val result = ReportTransport().send("http://127.0.0.1:${server.localPort}/api/v1/reports", "msk_test", "{}")
            assertTrue("$status: $result", result is SendResult.Retry)
            server.close()
        }
    }

    @Test
    fun noServerIsRetried() {
        server.close()
        assertTrue(ReportTransport(connectTimeoutMs = 2_000).send(url, "msk_test", "{}") is SendResult.Retry)
    }
}
