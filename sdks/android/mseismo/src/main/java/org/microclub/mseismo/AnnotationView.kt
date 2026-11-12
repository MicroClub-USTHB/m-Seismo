package org.microclub.mseismo

import android.annotation.SuppressLint
import android.content.Context
import android.graphics.Bitmap
import android.graphics.Canvas
import android.graphics.Color
import android.graphics.Matrix
import android.graphics.Paint
import android.graphics.Path
import android.graphics.RectF
import android.view.MotionEvent
import android.view.View

@SuppressLint("ViewConstructor")
internal class AnnotationView(context: Context, private val screenshot: Bitmap) : View(context) {
    enum class Tool(val color: Int, val widthDp: Float) {
        PEN(Color.RED, 4f),
        BLACK_OUT(Color.BLACK, 28f),
    }

    var tool = Tool.PEN

    init {
        contentDescription = context.getString(R.string.mseismo_screenshot_description)
    }

    private class Stroke(val path: Path, val paint: Paint)

    private val strokes = mutableListOf<Stroke>()
    private val toScreen = Matrix()
    private val toScreenshot = Matrix()

    fun undo() {
        strokes.removeLastOrNull()
        invalidate()
    }

    // A full-size copy with the strokes; the original stays clean.
    fun render(): Bitmap {
        val result = screenshot.copy(Bitmap.Config.ARGB_8888, true)
        Canvas(result).apply { strokes.forEach { drawPath(it.path, it.paint) } }
        return result
    }

    override fun onSizeChanged(w: Int, h: Int, oldw: Int, oldh: Int) {
        val image = RectF(0f, 0f, screenshot.width.toFloat(), screenshot.height.toFloat())
        toScreen.setRectToRect(image, RectF(0f, 0f, w.toFloat(), h.toFloat()), Matrix.ScaleToFit.CENTER)
        toScreen.invert(toScreenshot)
    }

    override fun onDraw(canvas: Canvas) {
        canvas.save()
        canvas.concat(toScreen)
        canvas.drawBitmap(screenshot, 0f, 0f, null)
        strokes.forEach { canvas.drawPath(it.path, it.paint) }
        canvas.restore()
    }

    @SuppressLint("ClickableViewAccessibility")
    override fun onTouchEvent(event: MotionEvent): Boolean {
        val point = floatArrayOf(event.x, event.y).also { toScreenshot.mapPoints(it) }
        when (event.actionMasked) {
            MotionEvent.ACTION_DOWN -> strokes += Stroke(Path().apply { moveTo(point[0], point[1]) }, paintFor(tool))
            MotionEvent.ACTION_MOVE -> strokes.lastOrNull()?.path?.lineTo(point[0], point[1])
            else -> return true
        }
        invalidate()
        return true
    }

    // Strokes are in screenshot pixels: convert the width so it looks the same on screen.
    private fun paintFor(tool: Tool) = Paint(Paint.ANTI_ALIAS_FLAG).apply {
        color = tool.color
        style = Paint.Style.STROKE
        strokeCap = Paint.Cap.ROUND
        strokeJoin = Paint.Join.ROUND
        strokeWidth = tool.widthDp * resources.displayMetrics.density / toScreen.mapRadius(1f)
    }
}
