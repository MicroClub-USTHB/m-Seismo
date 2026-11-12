package org.microclub.mseismo

import android.os.StrictMode
import android.util.Log

internal fun catchCrashes(save: (Throwable) -> Unit) {
    val previous = Thread.getDefaultUncaughtExceptionHandler()
    Thread.setDefaultUncaughtExceptionHandler { thread, error ->
        var policy: StrictMode.ThreadPolicy? = null
        try {
            policy = StrictMode.allowThreadDiskWrites()
            save(error)
        } catch (t: Throwable) {
            Log.w(MSeismo.TAG, "could not save the crash", t)
        } finally {
            policy?.let(StrictMode::setThreadPolicy)
            previous?.uncaughtException(thread, error)
        }
    }
}
