
package io.mseismo.android.delivery

import io.mseismo.android.Configuration
import io.mseismo.android.model.Report

interface ReportTransport{
    fun send(
        report:Report,
        configuration:Configuration,
        onComplete: (Result<Unit>) -> Unit
    )
}