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
import java.io.File
import java.util.UUID
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.Executors
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

    // New on every launch.
    @JvmStatic
    public val sessionId: String = UUID.randomUUID().toString()

    @Volatile
    private var app: Application? = null

    // Loaded on first use to keep app start-up fast.
    private val installationIdCache = FirstUse("installation ID", load = ::loadInstallationId)
    private val releaseCache = FirstUse("release", load = ::releaseOf)

    // Null until configured.
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
    private val worker = Executors.newSingleThreadScheduledExecutor { Thread(it, "mseismo").apply { isDaemon = true } }
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
            val bodies = configuration?.captureNetworkBodies == true
            requests.add(
                NetworkRequest(
                    System.currentTimeMillis(), url.limit(MaxChars.URL), method.limit(MaxChars.METHOD), statusCode, durationMs,
                    requestBody?.takeIf { bodies }?.limit(MaxChars.BODY), responseBody?.takeIf { bodies }?.limit(MaxChars.BODY),
                ),
            )
        }
    }

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
    ): String? = submit(Trigger.MANUAL, UserInput(title, description, category, severity))

    @JvmStatic
    public fun recordError(error: Throwable): String? = submit(Trigger.NON_FATAL, userInput = null, error)

    private fun submit(trigger: Trigger, userInput: UserInput?, error: Throwable? = null): String? = try {
        draft(trigger)?.also { sendDraft(it, userInput, screenshot = null, crash = error?.let(Crash::of)) }?.id
    } catch (t: Throwable) {
        Log.e(TAG, "report failed", t)
        null
    }

    // Opens the report screen. Any thread.
    @JvmStatic
    public fun showReportScreen(activity: Activity) {
        activity.runOnUiThread { open(activity, Trigger.MANUAL) }
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
        Log.i(TAG, "crash saved as report ${draft.id}, sent on the next launch")
    }

    private fun reportOf(draft: Draft, installationId: String, release: Release, device: Device, userInput: UserInput?, crash: Crash?) =
        Report(
            id = draft.id,
            installationId = installationId,
            sessionId = sessionId,
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
            bodiesCaptured = draft.config.captureNetworkBodies,
            crash = crash,
        )

    private fun idsFor(app: Application, config: Configuration): Pair<String, Release>? {
        val installationId = installationIdCache.get(app) ?: return null
        val release = config.release ?: releaseCache.get(app) ?: return null
        return installationId to release
    }

    private fun jsonOf(config: Configuration, report: Report): String =
        Redactor(config.redactionRules, config.redactionAllowList, config.redactionDenyList).redact(report).toJsonUnder()

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
