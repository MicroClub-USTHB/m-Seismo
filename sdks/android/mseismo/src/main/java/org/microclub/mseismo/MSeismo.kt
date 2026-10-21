package org.microclub.mseismo

import android.app.Application
import android.content.Context
import android.content.pm.ApplicationInfo
import android.os.Build
import android.util.Log
import java.io.File
import java.io.RandomAccessFile
import java.util.UUID

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
