package org.microclub.mseismo

import android.content.Context
import android.view.View
import android.view.ViewGroup

// Wraps instead of squeezing buttons (small screens, large text).
internal class ButtonRow(context: Context, private val gap: Int = 0) : ViewGroup(context) {
    override fun onMeasure(widthMeasureSpec: Int, heightMeasureSpec: Int) {
        val available = MeasureSpec.getSize(widthMeasureSpec) - paddingLeft - paddingRight
        val childWidth = MeasureSpec.makeMeasureSpec(available, MeasureSpec.AT_MOST)
        val childHeight = MeasureSpec.makeMeasureSpec(0, MeasureSpec.UNSPECIFIED)
        val lines = lines(available) { child -> child.measure(childWidth, childHeight) }
        val height = lines.sumOf { it.height } + gap * (lines.size - 1)
        setMeasuredDimension(
            resolveSize(available + paddingLeft + paddingRight, widthMeasureSpec),
            resolveSize(height + paddingTop + paddingBottom, heightMeasureSpec),
        )
    }

    override fun onLayout(changed: Boolean, l: Int, t: Int, r: Int, b: Int) {
        val available = width - paddingLeft - paddingRight
        val rtl = layoutDirection == View.LAYOUT_DIRECTION_RTL
        var y = paddingTop
        for (line in lines(available)) {
            // In right-to-left, the start is the right.
            var x = if (rtl) paddingLeft + available else paddingLeft
            for (child in line.children) {
                if (rtl) x -= child.measuredWidth
                child.layout(x, y, x + child.measuredWidth, y + child.measuredHeight)
                x += if (rtl) -gap else child.measuredWidth + gap
            }
            y += line.height + gap
        }
    }

    private class Line(val children: MutableList<View> = mutableListOf(), var width: Int = 0, var height: Int = 0)

    private fun lines(available: Int, measure: (View) -> Unit = {}): List<Line> {
        val lines = mutableListOf(Line())
        for (i in 0 until childCount) {
            val child = getChildAt(i)
            if (child.visibility == View.GONE) continue
            measure(child)
            val line = lines.last()
            val needed = if (line.children.isEmpty()) child.measuredWidth else line.width + gap + child.measuredWidth
            if (line.children.isNotEmpty() && needed > available) lines += Line()
            lines.last().apply {
                width += if (children.isEmpty()) child.measuredWidth else gap + child.measuredWidth
                children += child
                height = maxOf(height, child.measuredHeight)
            }
        }
        return lines
    }
}
