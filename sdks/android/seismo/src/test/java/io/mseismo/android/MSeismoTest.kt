package io.mseismo.android

import io.mseismo.android.delivery.ReportTransport
import io.mseismo.android.model.Report
import java.net.URL
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class MSeismoTest {

    private class FakeTransport : ReportTransport {
        var receivedReport: Report? = null
        var receivedConfiguration: Configuration? = null

        override fun send(
            report: Report,
            configuration: Configuration,
            onComplete: (Result<Unit>) -> Unit
        ) {
            receivedReport = report
            receivedConfiguration = configuration
            onComplete(Result.success(Unit))
        }
    }

    @Test
    fun report_redactsSensitiveTextBeforeSending() {
        val configuration = Configuration(
            endpoint = URL("https://example.com/reports"),
            projectKey = "test-project"
        )

        val transport = FakeTransport()
        val seismo = MSeismo(configuration, transport)

        val originalReport = Report(
            title = "problem reported by user@example.com",
            description = "contact user@example.com for details",
            recentLogs = listOf("login attempted by user@example.com"),
            deviceContext = mapOf("note" to "User: user@example.com")
        )
        seismo.report(originalReport) {}

        val sentReport = transport.receivedReport!!

        assertEquals(
            "problem reported by [REDACTED]",
            sentReport.title
        )

        assertEquals(
            "contact [REDACTED] for details",
            sentReport.description
        )

        assertEquals(
            listOf("login attempted by [REDACTED]"),
            sentReport.recentLogs
        )

        assertEquals(
            mapOf("note" to "User: [REDACTED]"),
            sentReport.deviceContext

        )

        assertEquals(
            "problem reported by user@example.com",
            originalReport.title
        )
    }
}