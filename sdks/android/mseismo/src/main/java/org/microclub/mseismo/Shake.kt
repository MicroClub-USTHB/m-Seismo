package org.microclub.mseismo

import android.hardware.SensorManager
import android.os.SystemClock
import kotlin.math.sqrt

// A deliberate shake: 4 hard movements over at least 0.5 s.
// Counts movements, not sensor readings: one flick gives several strong readings.
internal class ShakeDetector(private val now: () -> Long = SystemClock::elapsedRealtime) {
    private val jolts = ArrayDeque<Long>()
    private var moving = false
    private var lastShake: Long? = null

    // Values in m/s². True once per shake.
    fun onAcceleration(x: Float, y: Float, z: Float): Boolean {
        val g = sqrt(x * x + y * y + z * z) / SensorManager.GRAVITY_EARTH
        if (g < CALM_G) moving = false
        if (g < JOLT_G || moving) return false
        moving = true // the next jolt counts once the phone calms down
        val t = now()
        jolts.addLast(t)
        while (t - jolts.first() > WINDOW_MS) jolts.removeFirst()
        val last = lastShake
        if (jolts.size < JOLTS || t - jolts.first() < MIN_SPAN_MS || (last != null && t - last < COOLDOWN_MS)) return false
        jolts.clear()
        lastShake = t
        return true
    }

    private companion object {
        const val JOLT_G = 2.5
        const val CALM_G = 1.5
        const val JOLTS = 4
        const val WINDOW_MS = 1_500
        const val MIN_SPAN_MS = 500
        const val COOLDOWN_MS = 2_000
    }
}
