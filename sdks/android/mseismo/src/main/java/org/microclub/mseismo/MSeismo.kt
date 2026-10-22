package org.microclub.mseismo

import android.app.Application
import android.content.Context
import android.content.pm.ApplicationInfo
import android.os.Build
import android.util.Log
import java.io.File
import java.io.RandomAccessFile
import java.util.UUID
import java.util.concurrent.Executors

// Call configure() once in Application.onCreate.
public object MSeismo {
    internal const val TAG = "MSeismo"
    internal const val INSTALLATION_ID_FILE = "mseismo_installation_id"
    public const val SDK_NAME: String = "mseismo-android"
    public val SDK_VERSION: String = BuildConfig.SDK_VERSION

    @Volatile
    public var configuration: Configuration? = null
        private set

    // New on every launch.
    public val sessionId: String = UUID.randomUUID().toString()

    @Volatile
    private var app: Application? = null

    // Loaded on first use to keep app start-up fast.
    private val installationIdCache = FirstUse("installation ID", load = ::loadInstallationId)
    private val releaseCache = FirstUse("release", load = ::releaseOf)

    // Null until configured.
    public val installationId: String?
        get() = installationIdCache.get(app)

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
        } catch (t: Throwable) {
            this.configuration = null
            app = null
            Log.e(TAG, "configure failed; m-Seismo is off", t)
        }
    }

    private val sender = Executors.newSingleThreadExecutor { Thread(it, "mseismo-send").apply { isDaemon = true } }
    internal var send: (url: String, appKey: String, reportJson: String) -> SendResult = ReportTransport()::send

    // Sends a report in the background. Returns its ID, or null when the SDK is off. Never throws.
    @JvmStatic
    @JvmOverloads
    public fun report(
        title: String? = null,
        description: String? = null,
        category: Category? = null,
        severity: Severity? = null,
    ): String? = try {
        val config = configuration
        val app = app
        if (config == null || app == null) {
            Log.w(TAG, "report ignored: m-Seismo is off")
            null
        } else {
            val id = UUID.randomUUID().toString()
            val occurredAt = System.currentTimeMillis()
            val device = Device.current()
            val input = UserInput(title, description, category, severity).takeIf { it != UserInput() }
            sender.execute { deliver(app, config, id, occurredAt, Trigger.MANUAL, input, device) }
            id
        }
    } catch (t: Throwable) {
        Log.e(TAG, "report failed", t)
        null
    }

    // Uses the app and configuration from when report() was called, even if configure() runs again meanwhile.
    private fun deliver(
        app: Application,
        config: Configuration,
        id: String,
        occurredAt: Long,
        trigger: Trigger,
        input: UserInput?,
        device: Device,
    ) {
        try {
            val installationId = installationIdCache.get(app)
            val release = config.release ?: releaseCache.get(app)
            if (installationId == null || release == null) {
                // shortcut: dropped, like a failed send below; the offline queue will keep and retry it.
                Log.e(TAG, "report $id dropped: installation ID or release unavailable")
                return
            }
            val report = Report(
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
            )
            when (val result = send(config.reportsUrl, config.appKey, report.toJson())) {
                SendResult.Accepted -> Log.i(TAG, "report $id accepted")
                // shortcut: not retried yet, the report is lost; the offline queue will retry it.
                is SendResult.Retry -> Log.w(TAG, "report $id not sent: ${result.reason}")
                is SendResult.Rejected ->
                    Log.e(TAG, "report $id rejected: HTTP ${result.status} ${result.code}: ${result.message}")
            }
        } catch (t: Throwable) {
            Log.e(TAG, "report $id failed", t)
        }
    }

    // noBackupFilesDir: backups would copy the ID to other phones. The lock stops two processes making two IDs.
    internal fun loadInstallationId(context: Context): String {
        val file = File(context.noBackupFilesDir, INSTALLATION_ID_FILE)
        RandomAccessFile(file, "rw").use { raf ->
            val lock = raf.channel.lock()
            try {
                val stored = raf.readLine()?.trim()
                if (stored != null && isUuid(stored)) return stored
                // Missing or corrupt: make a new one.
                val id = UUID.randomUUID().toString()
                raf.setLength(0)
                raf.writeBytes(id)
                raf.fd.sync()
                return id
            } finally {
                lock.release()
            }
        }
    }

    private fun isUuid(value: String): Boolean =
        value.length == 36 && runCatching { UUID.fromString(value) }.isSuccess

    internal fun releaseOf(context: Context): Release {
        val info = context.packageManager.getPackageInfo(context.packageName, 0)
        val build = if (Build.VERSION.SDK_INT >= 28) info.longVersionCode else {
            @Suppress("DEPRECATION")
            info.versionCode.toLong()
        }
        val version = info.versionName.orEmpty().take(MAX_FIELD_LENGTH).ifEmpty { "unknown" }
        return Release(version = version, build = build.toString())
    }

    // Caches a value. After a failure, waits retryMs before trying again.
    internal class FirstUse<T : Any>(
        private val name: String,
        private val retryMs: Long = 10_000,
        private val load: (Context) -> T,
    ) {
        @Volatile
        private var value: T? = null
        private var retryAtNanos: Long? = null

        fun get(app: Application?): T? {
            if (app == null) return null
            value?.let { return it }
            synchronized(this) {
                value?.let { return it }
                val retryAt = retryAtNanos
                if (retryAt != null && System.nanoTime() - retryAt < 0) return null
                return try {
                    load(app).also { value = it }
                } catch (t: Throwable) {
                    retryAtNanos = System.nanoTime() + retryMs * 1_000_000
                    Log.e(TAG, "could not read the $name; retrying in $retryMs ms", t)
                    null
                }
            }
        }
    }
}
