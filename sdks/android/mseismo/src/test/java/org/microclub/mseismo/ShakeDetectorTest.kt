package org.microclub.mseismo

import org.junit.Assert.assertEquals
import org.junit.Test

class ShakeDetectorTest {
    private var clock = 10_000L
    private val detector = ShakeDetector { clock }
    private val hard = floatArrayOf(30f, 0f, 9.8f) // about 3.2 g
    private val calm = floatArrayOf(0f, 0f, 9.8f) // phone lying still: 1 g

    // Readings 20 ms apart, like the real sensor. Returns the number of shakes.
    private fun feed(readings: List<FloatArray>): Int =
        readings.count { r -> detector.onAcceleration(r[0], r[1], r[2]).also { clock += 20 } }

    // One movement: hard readings, then calm.
    private fun jolt(strong: Int = 3, pauseMs: Int = 180) = List(strong) { hard } + List(pauseMs / 20) { calm }

    @Test
    fun aDeliberateShakeIsDetected() {
        assertEquals(1, feed(List(4) { jolt() }.flatten()))
    }

    @Test
    fun aSmallFlickIsNot() {
        // The reported bug: one flick used to count as a shake.
        assertEquals(0, feed(jolt(strong = 10)))
    }

    @Test
    fun oneLongPushIsNot() {
        assertEquals(0, feed(jolt(strong = 35)))
    }

    @Test
    fun threeJoltsAreNotEnough() {
        assertEquals(0, feed(List(3) { jolt() }.flatten()))
    }

    @Test
    fun aQuickTrembleIsNot() {
        // 4 jolts in 0.3 s: a bump, not a shake.
        assertEquals(0, feed(List(4) { jolt(strong = 1, pauseMs = 60) }.flatten()))
    }

    @Test
    fun aGentleShakeIsNot() {
        val gentle = List(3) { floatArrayOf(20f, 0f, 9.8f) } + List(9) { calm } // about 2.3 g
        assertEquals(0, feed(List(8) { gentle }.flatten()))
    }

    @Test
    fun joltsTooFarApartAreNot() {
        assertEquals(0, feed(List(4) { jolt(pauseMs = 600) }.flatten()))
    }

    @Test
    fun aSecondShakeWithinTwoSecondsIsIgnored() {
        assertEquals(1, feed(List(8) { jolt() }.flatten()))
        clock += 2_000
        assertEquals(1, feed(List(4) { jolt() }.flatten()))
    }
}
