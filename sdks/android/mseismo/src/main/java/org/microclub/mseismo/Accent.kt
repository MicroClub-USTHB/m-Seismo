package org.microclub.mseismo

import android.annotation.SuppressLint
import android.app.Activity
import android.content.Context
import android.graphics.Color
import kotlin.math.pow

internal object Accent {
    val DEFAULT = Color.rgb(0x1A, 0x73, 0xE8)

    // Best first. Material's colorPrimary is an app resource, so it is found by name.
    @SuppressLint("DiscouragedApi")
    fun candidates(activity: Activity): IntArray {
        val material = activity.resources.getIdentifier("colorPrimary", "attr", activity.packageName)
        return listOf(material, android.R.attr.colorPrimary, android.R.attr.colorAccent)
            .filter { it != 0 }
            .mapNotNull { activity.themeColor(it) }
            .filter { Color.alpha(it) == 255 }
            .toIntArray()
    }

    // Skips colors that would vanish on the background.
    fun pick(chosen: Int?, candidates: IntArray, background: Int): Int =
        chosen ?: candidates.firstOrNull { contrast(it, background) >= 2.0 } ?: DEFAULT

    fun onColor(color: Int): Int = if (contrast(color, Color.WHITE) >= contrast(color, Color.BLACK)) Color.WHITE else Color.BLACK

    // WCAG: 1 (same) to 21 (black on white).
    fun contrast(a: Int, b: Int): Double {
        val (light, dark) = listOf(luminance(a), luminance(b)).sortedDescending()
        return (light + 0.05) / (dark + 0.05)
    }

    private fun luminance(color: Int): Double {
        fun channel(c: Int) = (c / 255.0).let { if (it <= 0.03928) it / 12.92 else ((it + 0.055) / 1.055).pow(2.4) }
        return 0.2126 * channel(Color.red(color)) + 0.7152 * channel(Color.green(color)) + 0.0722 * channel(Color.blue(color))
    }
}

internal fun Context.themeColor(attr: Int): Int? {
    val values = obtainStyledAttributes(intArrayOf(attr))
    return try {
        if (values.hasValue(0)) values.getColor(0, 0) else null
    } catch (e: UnsupportedOperationException) {
        null
    } finally {
        values.recycle()
    }
}
