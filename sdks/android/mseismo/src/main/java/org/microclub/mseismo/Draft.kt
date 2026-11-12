package org.microclub.mseismo

import android.app.Application
import android.graphics.Bitmap

// Captured at the trigger, kept while the user fills in the form.
internal data class Draft(
    val id: String,
    val app: Application,
    val config: Configuration,
    val trigger: Trigger,
    val occurredAt: Long,
    val logs: List<LogLine>,
    val breadcrumbs: List<Breadcrumb>,
    val requests: List<NetworkRequest>,
    val screenshot: Bitmap? = null,
)
