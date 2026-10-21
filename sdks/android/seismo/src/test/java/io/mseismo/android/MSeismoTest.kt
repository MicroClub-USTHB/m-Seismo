//package io.mseismo.android
//
//import io.mseismo.android.delivery.ReportTransport
//import io.mseismo.android.model.Report
//import java.net.URL
//import org.junit.Assert.assertEquals
//import org.junit.Assert.assertTrue
//import org.junit.Test
//
//
//class MSeismoTest {
//
//    private class FakeTransport : ReportTransport {
//        var receivedReport: Report? = null,
//        var receivedConfiguration: Configuration? = null
//
//        ovveride
//        fun send(
//            report: Report,
//            configuration: Configuration,
//            onComplete: (Result<Unit>) -> Unit
//        ) {
//            receivedReport = report
//            receivedConfiguration = configuration
//            onComplete(Result.success(Unit))
//        }
//    }
//
//
//    @Test
//    fun report_forwardsReportAndConfigurationToTransport() {
//        val configuration = Configuration(
//            endpoint = URL("https://example.com/reports"),
//            projectKey = "test-project"
//        )
//        val transport = FakeTransport()
//        val seismo = MSeismo(configuration, transport)
//        val report = Report(
//            title = "Save button btoken",
//            description = "Tapping Save does nothing"
//        )
//        var result: Result<Unit>? = null
//
//        seismo.report(report) {
//            result = it
//        }
//        assertEquals(report, transport, receivedReport)
//        assertEquals(configuration, transport, receivedConfiguration)
//        assertTrue(result?.isSuccess == true)
//
//    }
//}


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
    fun report_forwardsReportAndConfigurationToTransport() {
        val configuration = Configuration(
            endpoint = URL("https://example.com/reports"),
            projectKey = "test-project"
        )

        val transport = FakeTransport()
        val seismo = MSeismo(configuration, transport)

        val report = Report(
            title = "Save button broken",
            description = "Tapping Save does nothing"
        )

        var result: Result<Unit>? = null

        seismo.report(report) {
            result = it
        }

        assertEquals(report, transport.receivedReport)
        assertEquals(configuration, transport.receivedConfiguration)
        assertTrue(result?.isSuccess == true)
    }
}
