package org.microclub.mseismo

import android.app.Activity
import android.graphics.Bitmap
import android.graphics.Typeface
import android.os.Build
import android.os.Bundle
import android.text.InputType
import android.util.Log
import android.util.TypedValue
import android.view.Gravity
import android.view.View
import android.view.ViewGroup.LayoutParams.MATCH_PARENT
import android.view.ViewGroup.LayoutParams.WRAP_CONTENT
import android.widget.FrameLayout
import android.widget.ImageButton
import android.widget.ImageView
import android.widget.LinearLayout
import android.widget.RadioButton
import android.widget.ScrollView
import android.widget.TextView
import android.window.OnBackInvokedDispatcher
import java.lang.ref.WeakReference

internal class ReportActivity : Activity() {
    private lateinit var draftId: String
    private lateinit var palette: Palette
    private var annotation: AnnotationView? = null
    private var screenshot: Bitmap? = null
    private var onForm = false
    private var sent = false

    // Built once: going back to the drawing keeps what was typed.
    private val title by lazy {
        field(R.string.mseismo_title_hint, InputType.TYPE_CLASS_TEXT or InputType.TYPE_TEXT_FLAG_CAP_SENTENCES, MaxChars.TITLE, palette)
    }
    private val description by lazy {
        field(
            R.string.mseismo_description_hint,
            InputType.TYPE_CLASS_TEXT or InputType.TYPE_TEXT_FLAG_MULTI_LINE or InputType.TYPE_TEXT_FLAG_CAP_SENTENCES,
            MaxChars.DESCRIPTION,
            palette,
        ).apply { minLines = 4; gravity = Gravity.TOP or Gravity.START }
    }
    private val categories by lazy { choices(R.string.mseismo_category, R.array.mseismo_categories, optional = false) }
    private val severities by lazy { choices(R.string.mseismo_severity, R.array.mseismo_severities, optional = true) }

    val accent: Int get() = palette.accent

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        val draft = intent.getStringExtra(DRAFT_ID)?.let(MSeismo::draftFor)
        if (draft == null) {
            finish() // the app was restarted: the draft is gone
            return
        }
        current = WeakReference(this)
        draftId = draft.id
        palette = Palette(this, MSeismo.accentColor, intent.getIntArrayExtra(APP_COLORS) ?: IntArray(0))
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            onBackInvokedDispatcher.registerOnBackInvokedCallback(OnBackInvokedDispatcher.PRIORITY_DEFAULT) { back() }
        }
        val captured = draft.screenshot
        if (captured != null) {
            annotation = AnnotationView(this, captured)
            showDrawing()
        } else {
            showForm()
        }
    }

    override fun onDestroy() {
        super.onDestroy()
        if (!::draftId.isInitialized) return
        if (isFinishing && !sent) MSeismo.discardDraft(draftId)
    }

    @Deprecated("Deprecated in Java")
    override fun onBackPressed() = back()

    // From the form, back to the drawing; from the drawing, cancel.
    private fun back() {
        if (onForm && annotation != null) showDrawing() else finish()
    }

    private fun showDrawing() {
        val canvas = annotation?.detached() ?: return
        val tools = LinearLayout(this).apply {
            background = rounded(palette.background, 999, palette.outline)
            setPadding(dp(4), dp(4), dp(4), dp(4))
            addView(toolButton(R.drawable.mseismo_ic_pen, R.string.mseismo_pen, AnnotationView.Tool.PEN))
            addView(toolButton(R.drawable.mseismo_ic_black_out, R.string.mseismo_black_out, AnnotationView.Tool.BLACK_OUT))
            addView(iconButton(R.drawable.mseismo_ic_undo, R.string.mseismo_undo, palette.textSecondary) { canvas.undo() })
        }
        selectTool(tools, canvas.tool)
        val frame = FrameLayout(this).apply {
            background = rounded(BACKDROP, 16)
            clipToOutline = true
            addView(canvas)
        }
        onForm = false
        setScreen(
            topBar(R.drawable.mseismo_ic_close, R.string.mseismo_cancel, R.string.mseismo_mark_title, pillButton(R.string.mseismo_next, palette) {
                screenshot = rendered(canvas)
                showForm()
            }),
            frame to LinearLayout.LayoutParams(MATCH_PARENT, 0, 1f).apply { setMargins(dp(16), dp(4), dp(16), dp(12)) },
            tools to LinearLayout.LayoutParams(WRAP_CONTENT, WRAP_CONTENT).apply { gravity = Gravity.CENTER_HORIZONTAL },
            textButton(R.string.mseismo_skip_screenshot, palette.textSecondary) {
                screenshot = null
                showForm()
            } to LinearLayout.LayoutParams(WRAP_CONTENT, WRAP_CONTENT).apply { gravity = Gravity.CENTER_HORIZONTAL; topMargin = dp(4) },
        )
    }

    private fun showForm() {
        val fields = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            setPadding(dp(16), dp(4), dp(16), dp(16))
            addView(title.detached(), spaced(12))
            addView(description.detached(), spaced(20))
            addView(label(R.string.mseismo_category))
            addView(categories.detached(), spaced(16))
            addView(label(R.string.mseismo_severity))
            addView(severities.detached(), spaced(16))
            screenshot?.let { addView(screenshotCard(it)) }
        }
        val backToDrawing = annotation != null
        onForm = true
        setScreen(
            topBar(
                if (backToDrawing) R.drawable.mseismo_ic_back else R.drawable.mseismo_ic_close,
                if (backToDrawing) R.string.mseismo_back else R.string.mseismo_cancel,
                R.string.mseismo_title,
                action = null,
            ),
            ScrollView(this).apply { addView(fields) } to LinearLayout.LayoutParams(MATCH_PARENT, 0, 1f),
            pillButton(R.string.mseismo_send, palette) { send() } to
                LinearLayout.LayoutParams(MATCH_PARENT, WRAP_CONTENT).apply { setMargins(dp(16), dp(8), dp(16), dp(12)) },
        )
    }

    private fun send() {
        val input = UserInput(
            title = title.text.toString().trim().ifEmpty { null },
            description = description.text.toString().trim().ifEmpty { null },
            category = Category.entries.getOrNull(categories.checkedIndex()),
            severity = Severity.entries.getOrNull(severities.checkedIndex()),
        )
        sent = true
        MSeismo.finishDraft(draftId, input, screenshot)
        finish()
    }

    private fun rendered(canvas: AnnotationView): Bitmap? = try {
        canvas.render()
    } catch (e: OutOfMemoryError) {
        Log.w(MSeismo.TAG, "no memory to draw on the screenshot; sending the report without it", e)
        null
    }

    // Keeps the content clear of the system bars.
    private fun setScreen(vararg views: Pair<View, LinearLayout.LayoutParams>) {
        val column = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            views.forEach { (view, params) -> addView(view, params) }
        }
        setContentView(FrameLayout(this).apply { fitsSystemWindows = true; addView(column) })
    }

    private fun topBar(icon: Int, iconLabel: Int, title: Int, action: View?) = LinearLayout(this).apply {
        gravity = Gravity.CENTER_VERTICAL
        minimumHeight = dp(56)
        setPadding(dp(4), 0, dp(12), 0)
        addView(iconButton(icon, iconLabel, palette.text) { back() })
        addView(
            TextView(context).apply {
                setText(title)
                setTextSize(TypedValue.COMPLEX_UNIT_SP, 18f)
                typeface = Typeface.create("sans-serif-medium", Typeface.NORMAL)
                setTextColor(palette.text)
                setPadding(dp(8), 0, dp(8), 0)
                if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.P) isAccessibilityHeading = true
            },
            LinearLayout.LayoutParams(0, WRAP_CONTENT, 1f),
        )
        action?.let(::addView)
    } to LinearLayout.LayoutParams(MATCH_PARENT, WRAP_CONTENT)

    private fun toolButton(icon: Int, label: Int, tool: AnnotationView.Tool) = iconButton(icon, label, palette.textSecondary) { view ->
        annotation?.tool = tool
        selectTool(view.parent as LinearLayout, tool)
    }.apply { tag = tool }

    private fun selectTool(tools: LinearLayout, tool: AnnotationView.Tool) {
        for (i in 0 until tools.childCount) {
            val button = tools.getChildAt(i) as ImageButton
            if (button.tag is AnnotationView.Tool) button.select(button.tag == tool, palette)
        }
    }

    // TalkBack reads e.g. "Category: Bug".
    private fun choices(label: Int, items: Int, optional: Boolean) = ButtonRow(this, gap = dp(8)).apply {
        val name = getString(label)
        resources.getStringArray(items).forEach { item ->
            addView(chip(item, optional, palette).apply { contentDescription = "$name: $item" })
        }
        val chips = (0 until childCount).map { getChildAt(it) as RadioButton }
        chips.forEach { chip -> chip.setOnCheckedChangeListener { _, checked -> if (checked) chips.filter { it !== chip }.forEach { it.isChecked = false } } }
        if (!optional) chips.firstOrNull()?.isChecked = true
    }

    private fun ButtonRow.checkedIndex() = (0 until childCount).firstOrNull { (getChildAt(it) as RadioButton).isChecked } ?: -1

    private fun label(text: Int) = TextView(this).apply {
        setText(text)
        setTextColor(palette.textSecondary)
        setPadding(0, 0, 0, dp(4))
    }

    private fun screenshotCard(shot: Bitmap) = LinearLayout(this).apply {
        gravity = Gravity.CENTER_VERTICAL
        background = rounded(palette.background, 12, palette.outline)
        setPadding(dp(8), dp(8), dp(4), dp(8))
        addView(
            ImageView(context).apply {
                setImageBitmap(shot)
                background = rounded(BACKDROP, 6)
                clipToOutline = true
                importantForAccessibility = View.IMPORTANT_FOR_ACCESSIBILITY_NO
            },
            LinearLayout.LayoutParams(dp(44), dp(72)),
        )
        addView(
            TextView(context).apply {
                setText(R.string.mseismo_screenshot_attached)
                setTextColor(palette.textSecondary)
                setPadding(dp(12), 0, dp(8), 0)
            },
            LinearLayout.LayoutParams(0, WRAP_CONTENT, 1f),
        )
        addView(textButton(R.string.mseismo_edit, palette.accentText) { showDrawing() })
    }

    private fun spaced(bottomDp: Int) = LinearLayout.LayoutParams(MATCH_PARENT, WRAP_CONTENT).apply { bottomMargin = dp(bottomDp) }

    companion object {
        const val DRAFT_ID = "org.microclub.mseismo.DRAFT_ID"
        const val APP_COLORS = "org.microclub.mseismo.APP_COLORS"

        private const val BACKDROP = 0x40808080

        // Main thread only.
        private var current: WeakReference<ReportActivity>? = null
        val isOpen: Boolean
            get() = current?.get()?.let { !it.isFinishing && !it.isDestroyed } == true
    }
}
