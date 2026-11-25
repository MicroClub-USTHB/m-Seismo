package org.microclub.mseismo

import android.annotation.SuppressLint
import android.content.Context
import android.content.res.ColorStateList
import android.graphics.Color
import android.graphics.drawable.Drawable
import android.graphics.drawable.GradientDrawable
import android.graphics.drawable.InsetDrawable
import android.graphics.drawable.RippleDrawable
import android.graphics.drawable.StateListDrawable
import android.os.Build
import android.text.InputFilter
import android.view.Gravity
import android.view.View
import android.view.ViewGroup
import android.widget.Button
import android.widget.EditText
import android.widget.ImageButton
import android.widget.RadioButton

internal class Palette(context: Context, chosen: Int?, appColors: IntArray) {
    val background = context.themeColor(android.R.attr.colorBackground) ?: Color.WHITE
    val accent = Accent.pick(chosen, appColors, background)
    val onAccent = Accent.onColor(accent)
    val text = context.themeColor(android.R.attr.textColorPrimary) ?: Color.BLACK
    val textSecondary = context.themeColor(android.R.attr.textColorSecondary) ?: Color.GRAY
    val outline = withAlpha(text, 0x52)

    // Accent text only where it is readable.
    val accentText = if (Accent.contrast(accent, background) >= 3.0) accent else text
}

internal fun Context.dp(value: Int) = (value * resources.displayMetrics.density).toInt()

internal fun withAlpha(color: Int, alpha: Int) = (color and 0x00FFFFFF) or (alpha shl 24)

internal fun Context.rounded(fill: Int, radiusDp: Int, stroke: Int = Color.TRANSPARENT, strokeDp: Int = 1) = GradientDrawable().apply {
    setColor(fill)
    cornerRadius = dp(radiusDp).toFloat()
    if (stroke != Color.TRANSPARENT) setStroke(dp(strokeDp), stroke)
}

private fun Context.oval(fill: Int) = GradientDrawable().apply {
    shape = GradientDrawable.OVAL
    setColor(fill)
}

private fun ripple(color: Int, content: Drawable?, mask: Drawable?) = RippleDrawable(ColorStateList.valueOf(withAlpha(color, 0x33)), content, mask)

internal fun <T : View> T.detached(): T = apply { (parent as? ViewGroup)?.removeView(this) }

internal fun Context.pillButton(label: Int, palette: Palette, onClick: () -> Unit) = Button(this).apply {
    setText(label)
    isAllCaps = false
    stateListAnimator = null
    setTextColor(palette.onAccent)
    background = ripple(palette.onAccent, rounded(palette.accent, 999), null)
    minHeight = dp(48)
    minimumHeight = dp(48)
    setPadding(dp(20), 0, dp(20), 0)
    setOnClickListener { onClick() }
}

internal fun Context.textButton(label: Int, color: Int, onClick: () -> Unit) = Button(this, null, android.R.attr.borderlessButtonStyle).apply {
    setText(label)
    isAllCaps = false
    setTextColor(color)
    setOnClickListener { onClick() }
}

// 48 dp: Android's minimum touch size.
internal fun Context.iconButton(icon: Int, label: Int, tint: Int, onClick: (View) -> Unit) = ImageButton(this).apply {
    setImageResource(icon)
    contentDescription = getString(label)
    imageTintList = ColorStateList.valueOf(tint)
    background = ripple(tint, null, oval(Color.WHITE))
    minimumWidth = dp(48)
    minimumHeight = dp(48)
    setOnClickListener(onClick)
}

internal fun ImageButton.select(selected: Boolean, palette: Palette) {
    isSelected = selected
    val tint = if (selected) palette.onAccent else palette.textSecondary
    imageTintList = ColorStateList.valueOf(tint)
    background = ripple(tint, if (selected) context.oval(palette.accent) else null, context.oval(Color.WHITE))
}

// Stops at the schema's limit, so nothing is cut when sent.
internal fun Context.field(hint: Int, type: Int, maxLength: Int, palette: Palette) = EditText(this).apply {
    setHint(hint)
    inputType = type
    filters = arrayOf(InputFilter.LengthFilter(maxLength))
    background = StateListDrawable().apply {
        addState(intArrayOf(android.R.attr.state_focused), rounded(Color.TRANSPARENT, 12, palette.accent, 2))
        addState(intArrayOf(), rounded(Color.TRANSPARENT, 12, palette.outline))
    }
    setPadding(dp(14), dp(12), dp(14), dp(12))
    highlightColor = withAlpha(palette.accent, 0x55)
    if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) textCursorDrawable?.mutate()?.setTint(palette.accent)
}

// Tapping a selected optional chip unselects it.
@SuppressLint("ViewConstructor")
private class Chip(context: Context, private val optional: Boolean) : RadioButton(context) {
    override fun toggle() {
        if (optional && isChecked) isChecked = false else super.toggle()
    }
}

internal fun Context.chip(text: String, optional: Boolean, palette: Palette): RadioButton = Chip(this, optional).apply {
    this.text = text
    buttonDrawable = null
    gravity = Gravity.CENTER
    setTextColor(ColorStateList(arrayOf(intArrayOf(android.R.attr.state_checked), intArrayOf()), intArrayOf(palette.onAccent, palette.text)))
    // 40 dp drawn in a 48 dp touch area.
    background = InsetDrawable(
        StateListDrawable().apply {
            addState(intArrayOf(android.R.attr.state_checked), rounded(palette.accent, 999))
            addState(intArrayOf(), rounded(Color.TRANSPARENT, 999, palette.outline))
        },
        0, dp(4), 0, dp(4),
    )
    minHeight = dp(48)
    minimumHeight = dp(48)
    minWidth = dp(48)
    setPadding(dp(16), 0, dp(16), 0)
}
