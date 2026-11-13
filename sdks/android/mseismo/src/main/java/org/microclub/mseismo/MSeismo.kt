package org.microclub.mseismo

import android.app.Activity
import android.app.Application
import android.content.Intent
import android.content.pm.ApplicationInfo
import android.graphics.Bitmap
import android.net.ConnectivityManager
import android.net.Network
import android.os.SystemClock
import android.util.Log
import org.json.JSONArray
import org.json.JSONObject
import java.io.File
import java.util.UUID
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.Executors
import java.util.concurrent.FutureTask
import java.util.concurrent.ScheduledFuture
import java.util.concurrent.TimeUnit

// Call configure() once in Application.onCreate.
public object MSeismo {
    internal const val TAG = "MSeismo"
    private const val MAX_SCREENSHOT_BYTES = 8 * 1024 * 1024
    public const val SDK_NAME: String = "mseismo-android"
    @JvmStatic
    public val SDK_VERSION: String = BuildConfig.SDK_VERSION

    @JvmStatic
    @Volatile
    public var configuration: Configuration? = null
        private set

    @JvmStatic
    @Volatile
    public var featureFlags: Map<String, FlagValue> = emptyMap()

    @JvmStatic
    @Volatile
    public var experiments: Map<String, String> = emptyMap()

    @JvmStatic
    @Volatile
    public var crashlyticsInstallationId: String? = null

    @JvmStatic
    @Volatile
    public var onAutoReport: ((AutoReport) -> Unit)? = null

    // New on every launch.
    @JvmStatic
    public val sessionId: String = UUID.randomUUID().toString()

    @Volatile
    private var capturesNetworkBodiesOverride: Boolean? = null

    @Volatile
    private var redactionAllowListOverride: List<String>? = null

    @Volatile
    private var redactionDenyListOverride: List<String>? = null

    @Volatile
    private var app: Application? = null

    // Loaded on first use to keep app start-up fast.
    private val installationIdCache = FirstUse("installation ID", load = ::loadInstallationId)
    private val releaseCache = FirstUse("release", load = ::releaseOf)

    // Null until configure() succeeds.
    @JvmStatic
    public val installationId: String?
        get() = installationIdCache.get(app)

    @JvmStatic
    public val release: Release?
        get() = configuration?.release ?: releaseCache.get(app)

    // Never throws: an invalid configuration turns the SDK off.
    @JvmStatic
    @Synchronized
    public fun configure(application: Application, configuration: Configuration) {
        try {
            val debuggable = application.applicationInfo.flags and ApplicationInfo.FLAG_DEBUGGABLE != 0
            configuration.validate(allowHttp = debuggable)
            // Set app first so a visible configuration always has an installation ID.
            app = application
            this.configuration = configuration
            capturesNetworkBodiesOverride = null
            redactionAllowListOverride = null
            redactionDenyListOverride = null
            Log.i(TAG, "configured: environment=${configuration.environment}")
            // Send what the last launch left, and retry when the network returns.
            worker.execute { safely { drain() } }
            watchNetwork(application)
            watchLifecycle(application)
            if (!catchingCrashes) {
                catchCrashes(::saveCrash)
                catchingCrashes = true
            }
        } catch (t: Throwable) {
            this.configuration = null
            app = null
            Log.e(TAG, "configure failed; m-Seismo is off", t)
        }
    }

    // One thread for all background work: never blocks the app, needs no locks.
    @Volatile
    private var workerThread: Thread? = null
    private val worker = Executors.newSingleThreadScheduledExecutor {
        Thread(it, "mseismo").apply { isDaemon = true; workerThread = this }
    }
    internal var send: (url: String, appKey: String, reportJson: String, screenshotJpeg: ByteArray?) -> SendResult =
        ReportTransport()::send

    // Text is cut when recorded, so huge lines can't fill memory.
    internal val logs = RingBuffer<LogLine>(200)
    private val breadcrumbs = RingBuffer<Breadcrumb>(50)
    private val requests = RingBuffer<NetworkRequest>(50)

    // Works before configure(). Never throws.
    @JvmStatic
    public fun log(message: String) {
        safely { logs.add(LogLine(System.currentTimeMillis(), message.limit(MaxChars.LOG_MESSAGE))) }
    }

    // Never throws.
    @JvmStatic
    public fun breadcrumb(kind: BreadcrumbKind, message: String) {
        safely { breadcrumbs.add(Breadcrumb(System.currentTimeMillis(), kind, message.limit(MaxChars.BREADCRUMB))) }
    }

    // For Compose screens. Repeats are ignored, so it can run on every recomposition. Never throws.
    @JvmStatic
    public fun screen(name: String) {
        safely {
            synchronized(screenLock) {
                if (name == lastScreen) return@safely
                lastScreen = name
            }
            breadcrumb(BreadcrumbKind.NAVIGATION, name)
        }
    }

    private val screenLock = Any()
    private var lastScreen: String? = null

    @JvmStatic
    @Volatile
    public var accentColor: Int? = null

    // Call it from your HTTP client, e.g. an OkHttp interceptor. Bodies are dropped unless captureNetworkBodies. Never throws.
    @JvmStatic
    @JvmOverloads
    public fun recordRequest(
        url: String,
        method: String,
        statusCode: Int?,
        durationMs: Long,
        requestBody: String? = null,
        responseBody: String? = null,
    ) {
        safely {
            val bodies = capturesNetworkBodies()
            requests.add(
                NetworkRequest(
                    System.currentTimeMillis(), url.limit(MaxChars.URL), method.limit(MaxChars.METHOD), statusCode, durationMs,
                    requestBody?.takeIf { bodies }?.limit(MaxChars.BODY), responseBody?.takeIf { bodies }?.limit(MaxChars.BODY),
                ),
            )
        }
    }

    @JvmStatic
    public fun setCapturesNetworkBodies(enabled: Boolean) {
        capturesNetworkBodiesOverride = enabled
    }

    @JvmStatic
    public fun setRedactionAllowList(values: List<String>) {
        redactionAllowListOverride = values.toList()
    }

    @JvmStatic
    public fun setRedactionDenyList(values: List<String>) {
        redactionDenyListOverride = values.toList()
    }

    private fun capturesNetworkBodies(): Boolean =
        capturesNetworkBodiesOverride ?: configuration?.captureNetworkBodies == true

    internal fun resetRuntimeOverrides() {
        capturesNetworkBodiesOverride = null
        redactionAllowListOverride = null
        redactionDenyListOverride = null
        featureFlags = emptyMap()
        experiments = emptyMap()
        crashlyticsInstallationId = null
        onAutoReport = null
    }

    internal fun idleWorkerForTests() {
        val task = FutureTask {
            nextDrain?.cancel(false)
            nextDrain = null
        }
        worker.execute(task)
        try {
            task.get(5, TimeUnit.SECONDS)
        } catch (t: Throwable) {
            Log.w(TAG, "worker did not become idle", t)
        }
    }

    @JvmStatic
    public fun pendingReportCount(): Int {
        val app = app ?: return 0
        if (Thread.currentThread() === workerThread) return queueFor(app).pendingCount
        val task = FutureTask { queueFor(app).pendingCount }
        worker.execute(task)
        return try {
            task.get(5, TimeUnit.SECONDS)
        } catch (t: Throwable) {
            Log.w(TAG, "could not count pending reports", t)
            0
        }
    }

    @JvmStatic
    public fun retryNow() {
        val app = app ?: return
        worker.execute { safely { queueFor(app).retryNow(); drain() } }
    }

    @JvmStatic
    public fun newTraceId(): String = UUID.randomUUID().toString()

    // Worker thread only.
    private var queue: OfflineQueue? = null
    private var nextDrain: ScheduledFuture<*>? = null
    private var watchingNetwork = false
    private var watchedLifecycleOf: Application? = null
    private var catchingCrashes = false

    private val drafts = ConcurrentHashMap<String, Draft>()

    private var openingSince: Long? = null

    // Sends a report in the background. Returns its ID, or null when the SDK is off. Never throws.
    @JvmStatic
    @JvmOverloads
    public fun report(
        title: String? = null,
        description: String? = null,
        category: Category? = null,
        severity: Severity? = null,
    ): String? = submitDraft(Trigger.MANUAL, UserInput(title, description, category, severity))

    @JvmStatic
    public fun recordError(error: Throwable): String? = submitDraft(Trigger.NON_FATAL, userInput = null, error)

    @JvmStatic
    public fun recordError(message: String, stackTrace: String?, fatal: Boolean) {
        safely {
            val app = app ?: return@safely
            val frames = stackTrace.orEmpty().lineSequence().map { it.trimEnd() }.filter { it.isNotEmpty() }.toList()
            savePendingError(app, PendingError(message, frames, fatal, System.currentTimeMillis(), sessionId))
        }
    }

    @JvmStatic
    public fun checkForPendingCrash() {
        try {
            val app = app ?: return
            val config = configuration ?: return
            if (idsFor(app, config) == null) return
            val pending = takePendingError(app) ?: return
            val trigger = if (pending.fatal) Trigger.CRASH else Trigger.NON_FATAL
            val crash = Crash("FlutterError", pending.message, pending.stackTrace).normalized()
            val id = submit(
                trigger = trigger,
                crash = crash,
                occurredAtMillis = pending.occurredAtMillis,
                autoSubmitted = true,
                sessionId = pending.sessionId,
            ) ?: return
            notifyAutoReport(autoReportOf(id, trigger, pending.occurredAtMillis, crash))
        } catch (t: Throwable) {
            Log.e(TAG, "could not send the pending error", t)
        }
    }

    @JvmStatic
    @JvmOverloads
    public fun submit(
        trigger: Trigger = Trigger.MANUAL,
        title: String? = null,
        description: String? = null,
        category: Category? = null,
        severity: Severity? = null,
        screenshotJpeg: ByteArray? = null,
        crash: Crash? = null,
        occurredAtMillis: Long = System.currentTimeMillis(),
        autoSubmitted: Boolean = crash != null,
        id: String? = null,
        sessionId: String? = null,
    ): String? {
        return try {
            val config = configuration
            val app = app
            if (config == null || app == null) {
                Log.w(TAG, "report ignored: m-Seismo is off")
                return null
            }
            if ((trigger == Trigger.CRASH || trigger == Trigger.NON_FATAL) && crash == null) {
                Log.w(TAG, "report ignored: $trigger needs crash details")
                return null
            }
            val reportId = id ?: UUID.randomUUID().toString()
            val reportCrash = crash?.normalized()
            val (installationId, release) = idsFor(app, config) ?: return null
            val input = UserInput(title, description, category, severity)
            val flags = featureFlags.toMap()
            val experimentValues = experiments.toMap()
            val crashlyticsId = crashlyticsInstallationId
            val report = Report(
                id = reportId,
                installationId = installationId,
                sessionId = sessionId ?: this.sessionId,
                occurredAtMillis = occurredAtMillis,
                environment = config.environment,
                release = release,
                trigger = trigger,
                autoSubmitted = autoSubmitted,
                userInput = input.takeIf { it != UserInput() },
                device = Device.current(app),
                logs = logs.snapshot(),
                breadcrumbs = breadcrumbs.snapshot(),
                networkRequests = requests.snapshot(),
                bodiesCaptured = capturesNetworkBodies(),
                crash = reportCrash,
                featureFlags = flags,
                experiments = experimentValues,
                crashlyticsInstallationId = crashlyticsId,
            )
            val jpeg = screenshotJpeg?.takeIf { it.size <= MAX_SCREENSHOT_BYTES }
            val json = jsonOf(config, report)
            worker.execute {
                safely {
                    try {
                        queueFor(app).add(reportId, json, jpeg)
                    } catch (e: Exception) {
                        Log.w(TAG, "report $reportId could not be saved, sending it once without saving", e)
                        Log.i(TAG, "report $reportId: ${send(config.reportsUrl, config.appKey, json, jpeg)}")
                        return@safely
                    }
                    drain()
                }
            }
            reportId
        } catch (t: Throwable) {
            Log.e(TAG, "report failed", t)
            null
        }
    }

    private fun submitDraft(trigger: Trigger, userInput: UserInput?, error: Throwable? = null): String? = try {
        draft(trigger)?.also { draft ->
            val crash = error?.let(Crash::of)
            if (crash != null && (trigger == Trigger.CRASH || trigger == Trigger.NON_FATAL)) {
                notifyAutoReport(autoReportOf(draft.id, trigger, draft.occurredAt, crash))
            }
            sendDraft(draft, userInput, screenshot = null, crash = crash)
        }?.id
    } catch (t: Throwable) {
        Log.e(TAG, "report failed", t)
        null
    }

    // Opens the report screen. Any thread.
    @JvmStatic
    public fun showReportScreen(activity: Activity): Unit = showReportScreen(activity, Trigger.MANUAL)

    @JvmStatic
    public fun showReportScreen(activity: Activity, trigger: Trigger) {
        activity.runOnUiThread { open(activity, trigger) }
    }

    private fun open(activity: Activity, trigger: Trigger) {
        try {
            // One screen at a time, even if the user shakes again during the capture.
            val opening = openingSince?.let { SystemClock.elapsedRealtime() - it < 3_000 } == true
            if (opening || ReportActivity.isOpen || activity.isFinishing) return
            val draft = draft(trigger) ?: return
            openingSince = SystemClock.elapsedRealtime()
            captureScreen(activity) { screenshot ->
                openingSince = null
                if (activity.isFinishing || activity.isDestroyed) return@captureScreen
                try {
                    drafts.clear()
                    drafts[draft.id] = draft.copy(screenshot = screenshot)
                    val intent = Intent(activity, ReportActivity::class.java)
                        .putExtra(ReportActivity.DRAFT_ID, draft.id)
                        .putExtra(ReportActivity.APP_COLORS, Accent.candidates(activity))
                    activity.startActivity(intent)
                } catch (t: Throwable) {
                    drafts.remove(draft.id) // frees the screenshot
                    Log.e(TAG, "could not open the report screen", t)
                }
            }
        } catch (t: Throwable) {
            openingSince = null
            Log.e(TAG, "could not open the report screen", t)
        }
    }

    internal fun draftFor(id: String): Draft? = drafts[id]

    internal fun finishDraft(id: String, userInput: UserInput, screenshot: Bitmap?) {
        drafts.remove(id)?.let { sendDraft(it, userInput, screenshot) }
    }

    internal fun discardDraft(id: String) {
        drafts.remove(id)
    }

    private fun draft(trigger: Trigger): Draft? {
        val config = configuration
        val app = app
        if (config == null || app == null) {
            Log.w(TAG, "report ignored: m-Seismo is off")
            return null
        }
        return Draft(
            id = UUID.randomUUID().toString(),
            app = app,
            config = config,
            trigger = trigger,
            occurredAt = System.currentTimeMillis(),
            logs = logs.snapshot(),
            breadcrumbs = breadcrumbs.snapshot(),
            requests = requests.snapshot(),
            sessionId = sessionId,
            bodiesCaptured = capturesNetworkBodies(),
            featureFlags = featureFlags.toMap(),
            experiments = experiments.toMap(),
            crashlyticsInstallationId = crashlyticsInstallationId,
        )
    }

    private fun sendDraft(draft: Draft, userInput: UserInput?, screenshot: Bitmap?, crash: Crash? = null) {
        worker.execute {
            safely {
                val device = Device.current(draft.app)
                val jpeg = screenshot?.toJpeg()?.takeIf { it.size <= MAX_SCREENSHOT_BYTES }
                deliver(draft.app, draft.config, draft.id, jpeg) { installationId, release ->
                    reportOf(draft, installationId, release, device, userInput, crash)
                }
            }
        }
    }

    // On the crashing thread, not the worker: the app closes right after this returns.
    private fun saveCrash(error: Throwable) {
        val draft = draft(Trigger.CRASH) ?: return
        val (installationId, release) = idsFor(draft.app, draft.config) ?: run {
            Log.w(TAG, "crash not saved: installation ID or release unavailable")
            return
        }
        val report = reportOf(draft, installationId, release, Device.current(draft.app), userInput = null, Crash.of(error))
        OfflineQueue.write(queueDir(draft.app), draft.id, jsonOf(draft.config, report))
        notifyAutoReport(autoReportOf(draft.id, draft.trigger, draft.occurredAt, report.crash))
        Log.i(TAG, "crash saved as report ${draft.id}, sent on the next launch")
    }

    private fun reportOf(draft: Draft, installationId: String, release: Release, device: Device, userInput: UserInput?, crash: Crash?) =
        Report(
            id = draft.id,
            installationId = installationId,
            sessionId = draft.sessionId,
            occurredAtMillis = draft.occurredAt,
            environment = draft.config.environment,
            release = release,
            trigger = draft.trigger,
            autoSubmitted = crash != null, // crashes are sent without the report screen
            userInput = userInput?.takeIf { it != UserInput() },
            device = device,
            logs = draft.logs,
            breadcrumbs = draft.breadcrumbs,
            networkRequests = draft.requests,
            bodiesCaptured = draft.bodiesCaptured,
            crash = crash,
            featureFlags = draft.featureFlags,
            experiments = draft.experiments,
            crashlyticsInstallationId = draft.crashlyticsInstallationId,
        )

    private fun idsFor(app: Application, config: Configuration): Pair<String, Release>? {
        val installationId = installationIdCache.get(app) ?: return null
        val release = config.release ?: releaseCache.get(app) ?: return null
        return installationId to release
    }

    private fun jsonOf(config: Configuration, report: Report): String {
        val allowList = redactionAllowListOverride ?: config.redactionAllowList
        val denyList = redactionDenyListOverride ?: config.redactionDenyList
        return Redactor(config.redactionRules, allowList, denyList).redact(report).toJsonUnder()
    }

    // Uses the app and configuration from when report() was called, even if configure() runs again meanwhile.
    // build gets the installation ID and release, which are read here, off the caller's thread.
    private fun deliver(app: Application, config: Configuration, id: String, screenshot: ByteArray?, build: (String, Release) -> Report) {
        val (installationId, release) = idsFor(app, config) ?: run {
            // Storage hiccup: retry in 10 s.
            worker.schedule({ safely { deliver(app, config, id, screenshot, build) } }, 10, TimeUnit.SECONDS)
            return
        }
        val json = jsonOf(config, build(installationId, release))
        try {
            queueFor(app).add(id, json, screenshot)
        } catch (e: Exception) {
            Log.w(TAG, "report $id could not be saved, sending it once without saving", e)
            Log.i(TAG, "report $id: ${send(config.reportsUrl, config.appKey, json, screenshot)}")
            return
        }
        drain()
    }

    private fun queueDir(app: Application) = File(app.noBackupFilesDir, "mseismo-queue")

    private fun queueFor(app: Application): OfflineQueue {
        val dir = queueDir(app)
        return queue?.takeIf { it.dir == dir } ?: OfflineQueue(dir).also { queue = it }
    }

    // Sends what is due, then schedules the next retry.
    private fun drain() {
        val app = app ?: return
        val delay = queueFor(app).drain { json, screenshot -> configuration?.let { send(it.reportsUrl, it.appKey, json, screenshot) } }
        nextDrain?.cancel(false)
        nextDrain = delay?.let { worker.schedule({ safely { drain() } }, it, TimeUnit.MILLISECONDS) }
    }

    private fun Crash.normalized(): Crash = copy(
        type = type.ifEmpty { "unknown" },
        stackTrace = stackTrace.take(Crash.MAX_FRAMES).ifEmpty { listOf(NO_STACK_TRACE_FRAME) },
    )

    private fun autoReportOf(id: String, trigger: Trigger, occurredAtMillis: Long, crash: Crash?): AutoReport {
        val description = crash?.let {
            listOfNotNull(
                it.reason?.takeIf(String::isNotEmpty),
                it.stackTrace.takeIf { frames -> frames.isNotEmpty() }?.joinToString("\n"),
            ).joinToString("\n\n").ifEmpty { null }
        }
        return AutoReport(
            id = id,
            title = crash?.type?.takeIf(String::isNotEmpty),
            description = description,
            category = crash?.let { Category.CRASH },
            severity = when {
                crash == null -> null
                trigger == Trigger.CRASH -> Severity.CRITICAL
                trigger == Trigger.NON_FATAL -> Severity.HIGH
                else -> null
            },
            trigger = trigger,
            occurredAtMillis = occurredAtMillis,
            crash = crash,
        )
    }

    private fun notifyAutoReport(report: AutoReport) {
        val callback = onAutoReport ?: return
        try {
            callback(report)
        } catch (t: Throwable) {
            Log.w(TAG, "onAutoReport failed", t)
        }
    }

    private val pendingErrorLock = Any()

    private fun pendingErrorFile(app: Application) = File(app.noBackupFilesDir, "mseismo-pending-error.json")

    private fun savePendingError(app: Application, error: PendingError) {
        try {
            val file = pendingErrorFile(app)
            file.parentFile?.mkdirs()
            val json = JSONObject()
                .put("message", error.message)
                .put("stack_trace", JSONArray(error.stackTrace))
                .put("fatal", error.fatal)
                .put("occurred_at", error.occurredAtMillis)
                .put("session_id", error.sessionId)
            synchronized(pendingErrorLock) {
                val tmp = File(file.parentFile, "${file.name}.tmp")
                tmp.writeText(json.toString())
                check(tmp.renameTo(file)) { "could not save ${file.name}" }
            }
        } catch (t: Throwable) {
            Log.w(TAG, "could not save the error", t)
        }
    }

    private fun takePendingError(app: Application): PendingError? = synchronized(pendingErrorLock) {
        val file = pendingErrorFile(app)
        if (!file.exists()) return@synchronized null
        try {
            val json = JSONObject(file.readText())
            val frames = json.optJSONArray("stack_trace")?.let { array ->
                (0 until array.length()).mapNotNull { array.optString(it).takeIf(String::isNotEmpty) }
            }.orEmpty()
            PendingError(
                message = json.optString("message", ""),
                stackTrace = frames,
                fatal = json.optBoolean("fatal", false),
                occurredAtMillis = json.optLong("occurred_at", System.currentTimeMillis()),
                sessionId = json.optString("session_id", sessionId),
            )
        } catch (t: Throwable) {
            Log.w(TAG, "could not read the pending error", t)
            null
        } finally {
            file.delete()
        }
    }

    private fun watchNetwork(app: Application) = worker.execute {
        safely {
            if (watchingNetwork) return@safely
            val connectivity = app.getSystemService(ConnectivityManager::class.java) ?: return@safely
            connectivity.registerDefaultNetworkCallback(object : ConnectivityManager.NetworkCallback() {
                override fun onAvailable(network: Network) {
                    worker.execute { safely { queue?.retryNow(); drain() } }
                }
            })
            watchingNetwork = true
        }
    }

    @Synchronized
    private fun watchLifecycle(app: Application) {
        if (watchedLifecycleOf === app) return
        recordLifecycle(app, ::breadcrumb)
        app.registerActivityLifecycleCallbacks(
            Triggers(
                shakeEnabled = { configuration?.shakeEnabled == true },
                screenshotEnabled = { configuration?.screenshotEnabled == true },
                onTrigger = ::open,
            ),
        )
        watchedLifecycleOf = app
    }

    // Logs background failures instead of crashing the app.
    private inline fun safely(block: () -> Unit) {
        try {
            block()
        } catch (t: Throwable) {
            Log.e(TAG, "background work failed", t)
        }
    }
}

private data class PendingError(
    val message: String,
    val stackTrace: List<String>,
    val fatal: Boolean,
    val occurredAtMillis: Long,
    val sessionId: String,
)
