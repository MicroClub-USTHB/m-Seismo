package org.microclub.mseismo

import android.os.SystemClock
import android.util.Log
import java.io.File
import java.io.IOException
import kotlin.math.min
import kotlin.random.Random

// Reports waiting to be sent: <id>.json, plus <id>.jpg when there is a screenshot.
// Kept until the backend accepts or rejects them. One thread only.
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
        // Left by a crash mid-save: .tmp files and screenshots without a report.
        dir.listFiles { f -> f.name.endsWith(".tmp") }?.forEach { it.delete() }
        dir.listFiles { f -> f.name.endsWith(".jpg") && !File(dir, "${f.nameWithoutExtension}.json").exists() }
            ?.forEach { it.delete() }
    }

    // The screenshot is saved first: once <id>.json exists, the report is complete.
    fun add(id: String, reportJson: String, screenshotJpeg: ByteArray? = null) {
        dir.mkdirs()
        if (screenshotJpeg != null) save(File(dir, "$id.jpg")) { it.writeBytes(screenshotJpeg) }
        save(File(dir, "$id.json")) { it.writeText(reportJson) }
        files().dropLast(maxReports).forEach {
            Log.w(MSeismo.TAG, "queue full: report ${it.nameWithoutExtension} dropped")
            forget(it)
        }
    }

    // Returns ms until the next retry, or null if none. send returns null when the SDK is off.
    fun drain(send: (reportJson: String, screenshotJpeg: ByteArray?) -> SendResult?): Long? {
        val files = files()
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

    private fun files(): List<File> =
        dir.listFiles { f -> f.name.endsWith(".json") }.orEmpty().sortedWith(compareBy({ it.lastModified() }, { it.name }))

    // Rename is atomic, so a crash mid-write never leaves a half-written file under its real name.
    private fun save(file: File, write: (File) -> Unit) {
        val tmp = File(dir, "${file.name}.tmp")
        write(tmp)
        check(tmp.renameTo(file)) { "could not save ${file.name}" }
    }

    private fun screenshotOf(report: File) = File(dir, "${report.nameWithoutExtension}.jpg")

    private fun forget(file: File) {
        file.delete()
        screenshotOf(file).delete()
        backoffs.remove(file.nameWithoutExtension)
    }
}
