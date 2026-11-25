package org.microclub.mseismo

import android.graphics.Bitmap
import android.graphics.Color
import android.os.SystemClock
import android.view.MotionEvent
import androidx.test.core.app.ApplicationProvider
import org.junit.Assert.assertEquals
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
@GraphicsMode(GraphicsMode.Mode.NATIVE)
class AnnotationViewTest {
    // A 200x400 white screenshot shown in a 100x200 view: everything on screen is half size.
    private val screenshot = Bitmap.createBitmap(200, 400, Bitmap.Config.ARGB_8888).apply { eraseColor(Color.WHITE) }
    private val view = AnnotationView(ApplicationProvider.getApplicationContext(), screenshot).apply {
        measure(100, 200)
        layout(0, 0, 100, 200)
    }

    private fun drag(fromX: Float, fromY: Float, toX: Float, toY: Float) {
        val t = SystemClock.uptimeMillis()
        view.onTouchEvent(MotionEvent.obtain(t, t, MotionEvent.ACTION_DOWN, fromX, fromY, 0))
        view.onTouchEvent(MotionEvent.obtain(t, t + 10, MotionEvent.ACTION_MOVE, toX, toY, 0))
        view.onTouchEvent(MotionEvent.obtain(t, t + 20, MotionEvent.ACTION_UP, toX, toY, 0))
    }

    @Test
    fun aStrokeLandsWhereItWasDrawnOnTheFullSizeScreenshot() {
        drag(10f, 100f, 90f, 100f) // across the middle of the view
        val result = view.render()
        assertEquals(200, result.width)
        assertEquals(Color.RED, result.getPixel(100, 200)) // the middle of the screenshot
        assertEquals(Color.WHITE, result.getPixel(100, 20))
    }

    @Test
    fun blackOutCoversWhatIsUnderIt() {
        view.tool = AnnotationView.Tool.BLACK_OUT
        drag(10f, 100f, 90f, 100f)
        assertEquals(Color.BLACK, view.render().getPixel(100, 200))
    }

    @Test
    fun undoRemovesTheLastStroke() {
        drag(10f, 100f, 90f, 100f)
        view.undo()
        assertEquals(Color.WHITE, view.render().getPixel(100, 200))
    }

    @Test
    fun theOriginalScreenshotIsNotChanged() {
        drag(10f, 100f, 90f, 100f)
        view.render()
        assertEquals(Color.WHITE, screenshot.getPixel(100, 200))
    }
}
