package org.microclub.mseismo

import android.os.StrictMode
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertSame
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

// What Java does with an exception nobody caught.
internal fun crash(error: Throwable) = Thread.getDefaultUncaughtExceptionHandler()!!.uncaughtException(Thread.currentThread(), error)

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class CrashCaptureTest {
    private val before = Thread.getDefaultUncaughtExceptionHandler()
    private var handedOn: Throwable? = null

    init {
        // Stands for Crashlytics, or Android closing the app.
        Thread.setDefaultUncaughtExceptionHandler { _, error -> handedOn = error }
    }

    @After
    fun restore() = Thread.setDefaultUncaughtExceptionHandler(before)

    @Test
    fun theCrashIsSavedThenHandedOn() {
        var saved: Throwable? = null
        catchCrashes { saved = it }
        val error = IllegalStateException("boom")

        crash(error)

        assertSame(error, saved)
        assertSame("Crashlytics and Android still get the crash", error, handedOn)
    }

    @Test
    fun aFailureWhileSavingStillHandsTheCrashOn() {
        catchCrashes { throw OutOfMemoryError("no memory left to build the report") }
        val error = IllegalStateException("boom")

        crash(error)

        assertSame(error, handedOn)
    }

    // Many apps forbid disk access on the main thread in debug builds, and saving needs it.
    @Test
    fun savingMayUseTheDiskUnderStrictModeAndTheAppsSettingIsPutBack() {
        val strict = StrictMode.ThreadPolicy.Builder().detectDiskReads().detectDiskWrites().penaltyDeath().build()
        StrictMode.setThreadPolicy(strict)
        var whileSaving = ""
        catchCrashes { whileSaving = StrictMode.getThreadPolicy().toString() }
        try {
            crash(IllegalStateException("boom"))

            assertNotEquals("disk allowed while saving", strict.toString(), whileSaving)
            assertEquals("the app's setting is back", strict.toString(), StrictMode.getThreadPolicy().toString())
        } finally {
            StrictMode.setThreadPolicy(StrictMode.ThreadPolicy.LAX)
        }
    }
}
