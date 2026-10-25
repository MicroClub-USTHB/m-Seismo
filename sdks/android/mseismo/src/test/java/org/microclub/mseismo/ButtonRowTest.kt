package org.microclub.mseismo

import android.content.Context
import android.content.pm.ApplicationInfo
import android.view.View
import androidx.test.core.app.ApplicationProvider
import org.junit.Assert.assertEquals
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class ButtonRowTest {
    private val context: Context = ApplicationProvider.getApplicationContext<Context>().apply {
        // Like a real Arabic app: Android ignores right-to-left for apps that don't declare support for it.
        applicationInfo.flags = applicationInfo.flags or ApplicationInfo.FLAG_SUPPORTS_RTL
    }

    // A button that always wants 100 x 40 px.
    private fun button() = object : View(context) {
        override fun onMeasure(widthMeasureSpec: Int, heightMeasureSpec: Int) = setMeasuredDimension(100, 40)
    }

    private fun row(count: Int, width: Int, rtl: Boolean = false): List<View> {
        val buttons = List(count) { button() }
        ButtonRow(context).apply {
            layoutDirection = if (rtl) View.LAYOUT_DIRECTION_RTL else View.LAYOUT_DIRECTION_LTR
            buttons.forEach { addView(it) }
            measure(View.MeasureSpec.makeMeasureSpec(width, View.MeasureSpec.EXACTLY), View.MeasureSpec.makeMeasureSpec(0, View.MeasureSpec.UNSPECIFIED))
            layout(0, 0, width, measuredHeight)
        }
        return buttons
    }

    @Test
    fun buttonsThatFitStayOnOneLineAtTheEnd() {
        val (a, b) = row(count = 2, width = 300)
        assertEquals(listOf(100, 200), listOf(a.left, b.left)) // pushed right, in order
        assertEquals(listOf(0, 0), listOf(a.top, b.top))
    }

    @Test
    fun buttonsThatDoNotFitWrapInsteadOfShrinking() {
        val (a, b, c) = row(count = 3, width = 250)
        assertEquals(listOf(100, 100, 100), listOf(a.width, b.width, c.width)) // full size, never squeezed
        assertEquals(listOf(0, 0, 40), listOf(a.top, b.top, c.top)) // the third goes to the next line
        assertEquals(150, c.left)
    }

    @Test
    fun rightToLeftLanguagesStartFromTheRightAtTheLeftEnd() {
        val (a, b) = row(count = 2, width = 300, rtl = true)
        assertEquals(listOf(100, 0), listOf(a.left, b.left)) // the end is the left; reading order goes right to left
    }
}
