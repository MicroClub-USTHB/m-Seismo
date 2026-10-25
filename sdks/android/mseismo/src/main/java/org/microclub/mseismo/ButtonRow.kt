package org.microclub.mseismo

import android.content.Context
import android.view.View
import android.view.ViewGroup

internal class ButtonRow(context: Context) : ViewGroup(context) {
    override fun onMeasure(widthMeasureSpec: Int, heightMeasureSpec: Int) {
        val available = MeasureSpec.getSize(widthMeasureSpec) - paddingLeft - paddingRight
        val childWidth = MeasureSpec.makeMeasureSpec(available, MeasureSpec.AT_MOST)
        val childHeight = MeasureSpec.makeMeasureSpec(0, MeasureSpec.UNSPECIFIED)
        val height = lines(available) { child -> child.measure(childWidth, childHeight) }.sumOf { it.height }
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
            // Aligned to the end, which is the left in right-to-left languages.
            var x = if (rtl) paddingLeft + line.width else paddingLeft + available - line.width
            for (child in line.children) {
                if (rtl) x -= child.measuredWidth
                child.layout(x, y, x + child.measuredWidth, y + child.measuredHeight)
                if (!rtl) x += child.measuredWidth
            }
            y += line.height
        }
    }

    private class Line(val children: MutableList<View> = mutableListOf(), var width: Int = 0, var height: Int = 0)

    private fun lines(available: Int, measure: (View) -> Unit = {}): List<Line> {
        val lines = mutableListOf(Line())
        for (i in 0 until childCount) {
            val child = getChildAt(i)
            if (child.visibility == View.GONE) continue
            measure(child)
            if (lines.last().children.isNotEmpty() && lines.last().width + child.measuredWidth > available) lines += Line()
            lines.last().apply {
                children += child
                width += child.measuredWidth
                height = maxOf(height, child.measuredHeight)
            }
        }
        return lines
    }
}
