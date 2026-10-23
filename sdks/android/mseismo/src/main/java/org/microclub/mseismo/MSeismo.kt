package org.microclub.mseismo

import android.app.Activity
import android.app.AlertDialog
import android.app.Application
import android.content.pm.ApplicationInfo
import android.net.ConnectivityManager
import android.net.Network
import android.util.Log
import java.io.File
import java.lang.ref.WeakReference
import java.util.UUID
import java.util.concurrent.Executors
import java.util.concurrent.ScheduledFuture
import java.util.concurrent.TimeUnit

// Call configure() once in Application.onCreate.
public object MSeismo {
    internal const val TAG = "MSeismo"
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
        } catch (t: Throwable) {
            this.configuration = null
            app = null
            Log.e(TAG, "configure failed; m-Seismo is off", t)
        }
    }

    // One thread for all background work: never blocks the app, needs no locks.
    private val worker = Executors.newSingleThreadScheduledExecutor { Thread(it, "mseismo").apply { isDaemon = true } }
    internal var send: (url: String, appKey: String, reportJson: String) -> SendResult = ReportTransport()::send

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

    // Main thread only
    private var promptDialog: WeakReference<AlertDialog>? = null

    // Sends a report in the background. Returns its ID, or null when the SDK is off. Never throws.
    @JvmStatic
    @JvmOverloads
    public fun report(
        title: String? = null,
        description: String? = null,
        category: Category? = null,
        severity: Severity? = null,
    ): String? = submit(Trigger.MANUAL, UserInput(title, description, category, severity))

    private fun submit(trigger: Trigger, userInput: UserInput): String? = try {
        val config = configuration
        val app = app
        if (config == null || app == null) {
            Log.w(TAG, "report ignored: m-Seismo is off")
            null
        } else {
            val id = UUID.randomUUID().toString()
            val input = userInput.takeIf { it != UserInput() }
            val occurredAt = System.currentTimeMillis()
            val recentLogs = logs.snapshot()
            val recentBreadcrumbs = breadcrumbs.snapshot()
            val recentRequests = requests.snapshot()
            worker.execute {
                safely {
                    // Read on the worker: system services can be slow.
                    val device = Device.current(app)
                    deliver(app, config, id) { installationId, release ->
                        Report(
                            id = id,
                            installationId = installationId,
                            sessionId = sessionId,
                            occurredAtMillis = occurredAt,
                            environment = config.environment,
                            release = release,
                            trigger = trigger,
                            autoSubmitted = false,
                            userInput = input,
                            device = device,
                            logs = recentLogs,
                            breadcrumbs = recentBreadcrumbs,
                            networkRequests = recentRequests,
                            bodiesCaptured = config.captureNetworkBodies,
                        )
                    }
                }
            }
            id
        }
    } catch (t: Throwable) {
        Log.e(TAG, "report failed", t)
        null
    }

    // Uses the app and configuration from when report() was called, even if configure() runs again meanwhile.
    // build gets the installation ID and release, which are read here, off the caller's thread.
    private fun deliver(app: Application, config: Configuration, id: String, build: (String, Release) -> Report) {
        val installationId = installationIdCache.get(app)
        val release = config.release ?: releaseCache.get(app)
        if (installationId == null || release == null) {
            // Storage hiccup: retry in 10 s.
            worker.schedule({ safely { deliver(app, config, id, build) } }, 10, TimeUnit.SECONDS)
            return
        }
        val redactor = Redactor(config.redactionRules, config.redactionAllowList, config.redactionDenyList)
        val json = redactor.redact(build(installationId, release)).toJsonUnder()
        try {
            queueFor(app).add(id, json)
        } catch (e: Exception) {
            // Can't save (storage full): one unsaved try beats losing it.
            Log.w(TAG, "report $id could not be saved, sending it once without saving", e)
            Log.i(TAG, "report $id: ${send(config.reportsUrl, config.appKey, json)}")
            return
        }
        drain()
    }

    private fun queueFor(app: Application): OfflineQueue {
        val dir = File(app.noBackupFilesDir, "mseismo-queue")
        return queue?.takeIf { it.dir == dir } ?: OfflineQueue(dir).also { queue = it }
    }

    // Sends what is due, then schedules the next retry.
    private fun drain() {
        val app = app ?: return
        val delay = queueFor(app).drain { json -> configuration?.let { send(it.reportsUrl, it.appKey, json) } }
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
                onTrigger = ::prompt,
            ),
        )
        watchedLifecycleOf = app
    }

    // the user confirms first.
    // shortcut: a plain Send/Cancel dialog; the report sheet (step 5) replaces it.
    private fun prompt(activity: Activity, trigger: Trigger) {
        val open = promptDialog?.get()?.let { it.isShowing && it.ownerActivity === activity } == true
        if (open || configuration == null || activity.isFinishing) return
        safely {
            val dialog = AlertDialog.Builder(activity)
                .setTitle("Report a problem?")
                .setMessage("Send a bug report about what you were doing?")
                .setPositiveButton("Send") { _, _ -> submit(trigger, UserInput()) }
                .setNegativeButton("Cancel", null)
                .show()
            promptDialog = WeakReference(dialog)
        }
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
