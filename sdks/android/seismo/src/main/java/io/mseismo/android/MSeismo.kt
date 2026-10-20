package io.mseismo.android

import io.mseismo.android.delivery.ReportTransport
import io.mseismo.android.model.Report


class MSeismo(
    private val configuration:Configuration,
    private val transport:ReportTransport,
){
    fun report(
        report:Report,
        onComplete:(Result<Unit>) -> Unit
    ){
        transport.send(
            report=report,
            configuration=configuration,
            onComplete=onComplete
        )

    }
}