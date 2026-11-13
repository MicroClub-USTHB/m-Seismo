package org.microclub.mseismo

public enum class BreadcrumbKind { NAVIGATION, TAP, NETWORK, LIFECYCLE, CUSTOM }

internal data class LogLine(val timestampMillis: Long, val message: String)

internal data class Breadcrumb(val timestampMillis: Long, val kind: BreadcrumbKind, val message: String)

internal data class NetworkRequest(
    val timestampMillis: Long,
    val url: String,
    val method: String,
    val statusCode: Int?,
    val durationMs: Long,
    val requestBody: String?,
    val responseBody: String?,
)

// Keeps the last `capacity` items. Thread-safe.
internal class RingBuffer<T>(private val capacity: Int) {
    private val items = ArrayDeque<T>()

    @Synchronized
    fun add(item: T) {
        items.addLast(item)
        if (items.size > capacity) items.removeFirst()
    }

    @Synchronized
    fun snapshot(): List<T> = items.toList()
}
