package org.microclub.mseismo

import android.content.Context
import android.os.Build
import java.io.File
import java.io.RandomAccessFile
import java.util.UUID

internal const val INSTALLATION_ID_FILE = "mseismo_installation_id"

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
