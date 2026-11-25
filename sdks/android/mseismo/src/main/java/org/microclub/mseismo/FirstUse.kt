package org.microclub.mseismo

import android.app.Application
import android.content.Context
import android.util.Log

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
                Log.e(MSeismo.TAG, "could not read the $name; retrying in $retryMs ms", t)
                null
            }
        }
    }
}
