package org.microclub.mseismo

import android.app.Activity
import android.app.Application
import android.content.pm.ApplicationInfo
import android.os.Handler
import android.os.Looper
import io.flutter.embedding.engine.plugins.FlutterPlugin
import io.flutter.embedding.engine.plugins.activity.ActivityAware
import io.flutter.embedding.engine.plugins.activity.ActivityPluginBinding
import org.microclub.mseismo.flutter.BreadcrumbKind
import org.microclub.mseismo.flutter.FlutterError
import org.microclub.mseismo.flutter.MSeismoConfig
import org.microclub.mseismo.flutter.MSeismoFlutterApi
import org.microclub.mseismo.flutter.MSeismoHostApi
import org.microclub.mseismo.flutter.NetworkRequest
import org.microclub.mseismo.flutter.ReportDraft
import org.microclub.mseismo.flutter.ReportTrigger

class MSeismoPlugin : FlutterPlugin, ActivityAware, MSeismoHostApi {
    private val mainHandler = Handler(Looper.getMainLooper())
    private var application: Application? = null
    private var activity: Activity? = null
    private var flutterApi: MSeismoFlutterApi? = null

    override fun onAttachedToEngine(binding: FlutterPlugin.FlutterPluginBinding) {
        application = binding.applicationContext as? Application
        MSeismoHostApi.setUp(binding.binaryMessenger, this)
        val api = MSeismoFlutterApi(binding.binaryMessenger)
        flutterApi = api
        MSeismo.onAutoReport = { report ->
            mainHandler.post {
                if (flutterApi === api) {
                    api.onAutoReport(TypeMapping.autoReport(report)) { }
                }
            }
        }
    }

    override fun onDetachedFromEngine(binding: FlutterPlugin.FlutterPluginBinding) {
        MSeismo.onAutoReport = null
        MSeismoHostApi.setUp(binding.binaryMessenger, null)
        flutterApi = null
        application = null
    }

    override fun onAttachedToActivity(binding: ActivityPluginBinding) {
        activity = binding.activity
        application = binding.activity.application
    }

    override fun onDetachedFromActivityForConfigChanges() {
        activity = null
    }

    override fun onReattachedToActivityForConfigChanges(binding: ActivityPluginBinding) {
        activity = binding.activity
        application = binding.activity.application
    }

    override fun onDetachedFromActivity() {
        activity = null
    }

    override fun configure(config: MSeismoConfig) {
        val app = application ?: throw FlutterError(
            "not-attached",
            "m-Seismo could not access the Android application context.",
            null,
        )
        val release = config.release?.let { Release(it, releaseOf(app).build) }
        val configuration = Configuration(
            endpoint = config.baseUrl,
            appKey = config.sdkKey,
            environment = config.environment,
            release = release,
            shakeEnabled = config.shakeEnabled,
            screenshotEnabled = config.screenshotEnabled,
        )
        val debuggable =
            app.applicationInfo.flags and ApplicationInfo.FLAG_DEBUGGABLE != 0
        try {
            configuration.validate(allowHttp = debuggable)
        } catch (exception: Exception) {
            throw FlutterError(
                "invalid-configuration",
                exception.message ?: "Invalid m-Seismo configuration.",
                null,
            )
        }
        MSeismo.configure(app, configuration)
        if (MSeismo.configuration == null) {
            throw FlutterError(
                "invalid-configuration",
                "m-Seismo rejected the configuration.",
                null,
            )
        }
    }

    override fun report(trigger: ReportTrigger) {
        val current = activity ?: throw FlutterError(
            "no-activity",
            "Opening the report screen requires an attached Android Activity.",
            null,
        )
        MSeismo.showReportScreen(current, TypeMapping.trigger(trigger))
    }

    override fun submit(draft: ReportDraft): String {
        val crash = draft.crash?.let {
            Crash(it.type, it.reason, it.stackTrace)
        }
        return MSeismo.submit(
            trigger = TypeMapping.trigger(draft.trigger),
            title = draft.title,
            description = draft.description,
            category = TypeMapping.category(draft.category),
            severity = TypeMapping.severity(draft.severity),
            screenshotJpeg = draft.screenshot,
            crash = crash,
        ) ?: ""
    }

    override fun recordError(message: String, stackTrace: String?, fatal: Boolean) {
        MSeismo.recordError(message, stackTrace, fatal)
    }

    override fun checkForPendingCrash() {
        MSeismo.checkForPendingCrash()
    }

    override fun log(message: String) {
        MSeismo.log(message)
    }

    override fun breadcrumb(kind: BreadcrumbKind, message: String) {
        MSeismo.breadcrumb(TypeMapping.breadcrumbKind(kind), message)
    }

    override fun recordNetworkRequest(request: NetworkRequest) {
        MSeismo.recordRequest(
            url = request.url,
            method = request.method,
            statusCode = request.statusCode?.toInt(),
            durationMs = request.durationMs,
            requestBody = request.requestBody,
            responseBody = request.responseBody,
        )
    }

    override fun setCapturesNetworkBodies(enabled: Boolean) {
        MSeismo.setCapturesNetworkBodies(enabled)
    }

    override fun setFeatureFlags(flags: Map<String, Any?>) {
        MSeismo.featureFlags = TypeMapping.flagValues(flags)
    }

    override fun setExperiments(experiments: Map<String, String>) {
        MSeismo.experiments = experiments
    }

    override fun setCrashlyticsInstallationId(id: String?) {
        MSeismo.crashlyticsInstallationId = id
    }

    override fun setRedactionAllowList(values: List<String>) {
        MSeismo.setRedactionAllowList(values)
    }

    override fun setRedactionDenyList(values: List<String>) {
        MSeismo.setRedactionDenyList(values)
    }

    override fun pendingReportCount(): Long =
        MSeismo.pendingReportCount().toLong()

    override fun retryNow() {
        MSeismo.retryNow()
    }

    override fun getInstallationId(): String = MSeismo.installationId ?: throw FlutterError(
        "not-configured",
        "Call configure() before reading the installation ID.",
        null,
    )

    override fun getSessionId(): String = MSeismo.sessionId

    override fun newTraceId(): String = MSeismo.newTraceId()
}