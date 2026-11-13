package org.microclub.mseismo

import android.os.SystemClock
import android.util.Log
import java.io.File
import java.io.IOException
import kotlin.math.min
import kotlin.random.Random

// Reports waiting to be sent: <id>.json, plus <id>.jpg when there is a screenshot.
// Kept until the backend accepts or rejects them. An instance is for one thread only; write() works from any.
internal class OfflineQueue(
    val dir: File,
    // Monotonic, so changing the phone's clock can't stall retries.
    private val now: () -> Long = SystemClock::elapsedRealtime,
    private val random: Random = Random.Default,
    private val maxReports: Int = MAX_REPORTS,
) {
    // notBefore: the backend's Retry-After.
    private class Backoff(var attempts: Int, var nextTryAt: Long, var notBefore: Long)

    val pendingCount: Int
        get() = dir.listFiles { f -> f.name.endsWith(".json") }.orEmpty().size

    // In memory: after a restart, everything is retried at once.
    private val backoffs = HashMap<String, Backoff>()

    init {
        // Left by a crash mid-save: .tmp files and screenshots without a report.
        dir.listFiles { f -> f.name.endsWith(".tmp") }?.forEach { it.delete() }
        dir.listFiles { f -> f.name.endsWith(".jpg") && !File(dir, "${f.nameWithoutExtension}.json").exists() }
            ?.forEach { it.delete() }
    }

    // The screenshot is saved first: once <id>.json exists, the report is complete.
    fun add(id: String, reportJson: String, screenshotJpeg: ByteArray? = null) {
        dir.mkdirs()
        if (screenshotJpeg != null) save(File(dir, "$id.jpg")) { it.writeBytes(screenshotJpeg) }
        write(dir, id, reportJson, maxReports)
    }

    // Returns ms until the next retry, or null if none. send returns null when the SDK is off.
    fun drain(send: (reportJson: String, screenshotJpeg: ByteArray?) -> SendResult?): Long? {
        val files = reportsIn(dir)
        // Forget retries for reports deleted elsewhere (e.g. by another process of the app).
        backoffs.keys.retainAll(files.map { it.nameWithoutExtension }.toSet())
        for (file in files) {
            val id = file.nameWithoutExtension
            if ((backoffs[id]?.nextTryAt ?: 0) > now()) continue
            val (json, screenshot) = try {
                file.readText() to screenshotOf(file).takeIf { it.exists() }?.readBytes()
            } catch (e: IOException) {
                Log.w(MSeismo.TAG, "report $id unreadable, skipped", e)
                continue
            }
            val result = try {
                send(json, screenshot) ?: return null
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

    private fun forget(file: File) {
        delete(file)
        backoffs.remove(file.nameWithoutExtension)
    }

    companion object {
        const val MAX_REPORTS = 100

        fun write(dir: File, id: String, reportJson: String, maxReports: Int = MAX_REPORTS) {
            dir.mkdirs()
            save(File(dir, "$id.json")) { it.writeText(reportJson) }
            reportsIn(dir).dropLast(maxReports).forEach {
                Log.w(MSeismo.TAG, "queue full: report ${it.nameWithoutExtension} dropped")
                delete(it)
            }
        }

        private fun reportsIn(dir: File): List<File> =
            dir.listFiles { f -> f.name.endsWith(".json") }.orEmpty().sortedWith(compareBy({ it.lastModified() }, { it.name }))

        private fun screenshotOf(report: File) = File(report.parentFile, "${report.nameWithoutExtension}.jpg")

        private fun delete(report: File) {
            report.delete()
            screenshotOf(report).delete()
        }

        // Rename is atomic, so a crash mid-write never leaves a half-written file under its real name.
        private fun save(file: File, write: (File) -> Unit) {
            val tmp = File(file.parentFile, "${file.name}.tmp")
            write(tmp)
            check(tmp.renameTo(file)) { "could not save ${file.name}" }
        }
    }
}
