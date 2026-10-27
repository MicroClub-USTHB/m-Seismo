package io.mseismo.android

import io.mseismo.android.delivery.ReportTransport
import io.mseismo.android.model.Report
import io.mseismo.android.privacy.Redactor


class MSeismo(
    private val configuration:Configuration,
    private val transport:ReportTransport,
    private val redactor:Redactor=Redactor(),
){
    fun report(
        report:Report,
        onComplete:(Result<Unit>) -> Unit
    ){
        val sanitizedReport = report.copy(
            title = redactor.redact(report.title),
            description =redactor.redact(report.description),
            deviceContext =report.deviceContext?.mapValues {(_,value) ->
                redactor.redact(value)
            },

            recentLogs = report.recentLogs?.map{redactor.redact(it) },
            breadcrumbs = report.breadcrumbs?.map{redactor.redact(it)},
            networkRequests = report.networkRequests?.map{redactor.redact(it)}
        )

        transport.send(
            report=sanitizedReport,
            configuration=configuration,
            onComplete=onComplete
        )

    }
}