package org.microclub.mseismo

import android.os.SystemClock
import android.util.Log
import java.io.File
import java.io.IOException
import kotlin.math.min
import kotlin.random.Random

// Reports waiting to be sent, one file each, kept until the backend accepts or rejects them. One thread only.
internal class OfflineQueue(
    val dir: File,
    // Monotonic, so changing the phone's clock can't stall retries.
    private val now: () -> Long = SystemClock::elapsedRealtime,
    private val random: Random = Random.Default,
    private val maxReports: Int = 100,
) {
    // notBefore: the backend's Retry-After.
    private class Backoff(var attempts: Int, var nextTryAt: Long, var notBefore: Long)

    // In memory: after a restart, everything is retried at once.
    private val backoffs = HashMap<String, Backoff>()

    init {
        // Left by a crash mid-save.
        dir.listFiles { f -> f.name.endsWith(".tmp") }?.forEach { it.delete() }
    }

    fun add(id: String, reportJson: String) {
        dir.mkdirs()
        val tmp = File(dir, "$id.tmp")
        tmp.writeText(reportJson)
        // Rename is atomic, so a .json file is always complete.
        check(tmp.renameTo(File(dir, "$id.json"))) { "could not save report $id" }
        files().dropLast(maxReports).forEach {
            Log.w(MSeismo.TAG, "queue full: report ${it.nameWithoutExtension} dropped")
            forget(it)
        }
    }

    // Returns ms until the next retry, or null if none. send returns null when the SDK is off.
    fun drain(send: (reportJson: String) -> SendResult?): Long? {
        val files = files()
        // Forget retries for reports deleted elsewhere (e.g. by another process of the app).
        backoffs.keys.retainAll(files.map { it.nameWithoutExtension }.toSet())
        for (file in files) {
            val id = file.nameWithoutExtension
            if ((backoffs[id]?.nextTryAt ?: 0) > now()) continue
            val json = try {
                file.readText()
            } catch (e: IOException) {
                Log.w(MSeismo.TAG, "report $id unreadable, skipped", e)
                continue
            }
            val result = try {
                send(json) ?: return null
            } catch (e: RuntimeException) {
                SendResult.Retry("unexpected error: $e")
            }
            when (result) {
                SendResult.Accepted -> {
                    Log.i(MSeismo.TAG, "report $id accepted")
                    forget(file)
                }
                is SendResult.Rejected -> {
                    Log.e(MSeismo.TAG, "report $id rejected: HTTP ${result.status} ${result.code}: ${result.message}")
                    forget(file)
                }
                is SendResult.Retry -> {
                    val backoff = backoffs.getOrPut(id) { Backoff(0, 0, 0) }
                    backoff.attempts++
                    backoff.notBefore = now() + (result.retryAfterSeconds ?: 0) * 1000
                    backoff.nextTryAt = maxOf(now() + delayMs(backoff.attempts), backoff.notBefore)
                    Log.w(MSeismo.TAG, "report $id not sent (${result.reason}), retrying in ${backoff.nextTryAt - now()} ms")
                }
            }
        }
        return backoffs.values.minOfOrNull { it.nextTryAt }?.let { maxOf(0, it - now()) }
    }

    // Network is back: retry now, but not before Retry-After.
    fun retryNow() = backoffs.values.forEach { it.nextTryAt = it.notBefore }

    // 2, 4, 8… s up to 5 min, with jitter.
    private fun delayMs(attempts: Int): Long {
        val capMs = min(300_000L, 1000L shl min(attempts, 9))
        return capMs / 2 + random.nextLong(capMs / 2 + 1)
    }

    private fun files(): List<File> =
        dir.listFiles { f -> f.name.endsWith(".json") }.orEmpty().sortedWith(compareBy({ it.lastModified() }, { it.name }))

    private fun forget(file: File) {
        file.delete()
        backoffs.remove(file.nameWithoutExtension)
    }
}
