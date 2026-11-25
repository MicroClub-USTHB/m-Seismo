package org.microclub.mseismo

import android.graphics.Color
import org.junit.Assert.assertEquals
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class AccentTest {
    private val purple = Color.rgb(0x5B, 0x3C, 0xC4)
    private val paleGrey = Color.rgb(0xF5, 0xF5, 0xF5)
    private val white = Color.WHITE

    @Test
    fun theColorTheAppChoseWins() {
        assertEquals(Color.RED, Accent.pick(Color.RED, intArrayOf(purple), white))
    }

    @Test
    fun aColorThatVanishesOnTheBackgroundIsSkipped() {
        assertEquals(purple, Accent.pick(null, intArrayOf(paleGrey, purple), white))
        assertEquals(Accent.DEFAULT, Accent.pick(null, intArrayOf(paleGrey), white))
        assertEquals(Accent.DEFAULT, Accent.pick(null, IntArray(0), white))
    }

    @Test
    fun textOnTheColorIsWhateverReadsBetter() {
        assertEquals(Color.WHITE, Accent.onColor(purple))
        assertEquals(Color.BLACK, Accent.onColor(Color.YELLOW))
    }

    @Test
    fun contrastFollowsWcag() {
        assertEquals(21.0, Accent.contrast(Color.BLACK, Color.WHITE), 0.01)
        assertEquals(1.0, Accent.contrast(purple, purple), 0.01)
    }
}
