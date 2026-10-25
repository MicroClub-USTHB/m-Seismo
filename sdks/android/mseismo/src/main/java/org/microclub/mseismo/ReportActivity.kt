package org.microclub.mseismo

import android.app.Activity
import android.graphics.Bitmap
import android.os.Build
import android.os.Bundle
import android.text.InputFilter
import android.text.InputType
import android.util.Log
import android.util.TypedValue
import android.view.Gravity
import android.view.View
import android.view.ViewGroup
import android.view.ViewGroup.LayoutParams.MATCH_PARENT
import android.widget.ArrayAdapter
import android.widget.Button
import android.widget.EditText
import android.widget.FrameLayout
import android.widget.ImageView
import android.widget.LinearLayout
import android.widget.ScrollView
import android.widget.Spinner
import android.widget.TextView
import android.window.OnBackInvokedDispatcher
import java.lang.ref.WeakReference

internal class ReportActivity : Activity() {
    private lateinit var draftId: String
    private var annotation: AnnotationView? = null
    private var screenshot: Bitmap? = null
    private var onForm = false
    private var sent = false

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        val draft = intent.getStringExtra(DRAFT_ID)?.let(MSeismo::draftFor)
        if (draft == null) {
            finish() // the app was restarted: the draft is gone
            return
        }
        current = WeakReference(this)
        draftId = draft.id
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
        val canvas = annotation ?: return
        (canvas.parent as? FrameLayout)?.removeView(canvas)
        val tools = row(
            toolButton(R.string.mseismo_pen, AnnotationView.Tool.PEN),
            toolButton(R.string.mseismo_black_out, AnnotationView.Tool.BLACK_OUT),
            button(R.string.mseismo_undo) { canvas.undo() },
        )
        val actions = row(
            button(R.string.mseismo_cancel) { finish() },
            button(R.string.mseismo_skip_screenshot) { screenshot = null; showForm() },
            button(R.string.mseismo_continue) { screenshot = rendered(canvas); showForm() },
        )
        selectTool(tools, canvas.tool)
        onForm = false
        setScreen(
            column(
                heading(R.string.mseismo_title),
                text(R.string.mseismo_mark_hint),
                FrameLayout(this).apply { setBackgroundColor(BACKDROP); addView(canvas) }.withWeight(),
                tools,
                actions,
            ),
        )
    }

    private fun showForm() {
        val title = edit(R.string.mseismo_title_hint, InputType.TYPE_CLASS_TEXT or InputType.TYPE_TEXT_FLAG_CAP_SENTENCES, MaxChars.TITLE)
        val description = edit(
            R.string.mseismo_description_hint,
            InputType.TYPE_CLASS_TEXT or InputType.TYPE_TEXT_FLAG_MULTI_LINE or InputType.TYPE_TEXT_FLAG_CAP_SENTENCES,
            MaxChars.DESCRIPTION,
        ).apply { minLines = 4; gravity = Gravity.TOP or Gravity.START }
        val category = Spinner(this).apply {
            id = View.generateViewId()
            adapter = ArrayAdapter.createFromResource(context, R.array.mseismo_categories, android.R.layout.simple_spinner_dropdown_item)
        }
        // Optional, so "Not set" comes first.
        val severities = listOf(getString(R.string.mseismo_severity_none)) + resources.getStringArray(R.array.mseismo_severities)
        val severity = Spinner(this).apply {
            id = View.generateViewId()
            adapter = ArrayAdapter(context, android.R.layout.simple_spinner_dropdown_item, severities)
        }
        val form = column(
            heading(R.string.mseismo_title),
            title,
            description,
            text(R.string.mseismo_category).apply { labelFor = category.id },
            category,
            text(R.string.mseismo_severity).apply { labelFor = severity.id },
            severity,
        )
        screenshot?.let { shot ->
            form.addView(ImageView(this).apply {
                setImageBitmap(shot)
                setBackgroundColor(BACKDROP)
                layoutParams = LinearLayout.LayoutParams(MATCH_PARENT, dp(200))
            })
            form.addView(text(R.string.mseismo_screenshot_attached))
        }
        form.addView(
            row(
                button(R.string.mseismo_back) { back() },
                button(R.string.mseismo_send) {
                    val input = UserInput(
                        title = title.text.toString().trim().ifEmpty { null },
                        description = description.text.toString().trim().ifEmpty { null },
                        category = Category.entries.getOrNull(category.selectedItemPosition),
                        severity = Severity.entries.getOrNull(severity.selectedItemPosition - 1),
                    )
                    sent = true
                    MSeismo.finishDraft(draftId, input, screenshot)
                    finish()
                },
            ),
        )
        onForm = true
        setScreen(ScrollView(this).apply { addView(form) })
    }

    private fun rendered(canvas: AnnotationView): Bitmap? = try {
        canvas.render()
    } catch (e: OutOfMemoryError) {
        Log.w(MSeismo.TAG, "no memory to draw on the screenshot; sending the report without it", e)
        null
    }

    // Keeps the content clear of the system bars.
    private fun setScreen(content: View) =
        setContentView(FrameLayout(this).apply { fitsSystemWindows = true; addView(content) })

    private fun toolButton(label: Int, tool: AnnotationView.Tool) = button(label) { view ->
        annotation?.tool = tool
        selectTool(view.parent as ViewGroup, tool)
    }.apply { tag = tool }

    private fun selectTool(tools: ViewGroup, tool: AnnotationView.Tool) {
        for (i in 0 until tools.childCount) {
            val child = tools.getChildAt(i)
            if (child.tag !is AnnotationView.Tool) continue
            child.isSelected = child.tag == tool
            child.alpha = if (child.isSelected) 1f else 0.5f
        }
    }

    private fun column(vararg views: View) = LinearLayout(this).apply {
        orientation = LinearLayout.VERTICAL
        setPadding(dp(16), dp(16), dp(16), dp(16))
        views.forEach { addView(it) }
    }

    private fun row(vararg views: View) = ButtonRow(this).apply { views.forEach { addView(it) } }

    private fun heading(text: Int) = TextView(this).apply {
        setText(text)
        setTextSize(TypedValue.COMPLEX_UNIT_SP, 22f)
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.P) isAccessibilityHeading = true
    }

    private fun text(text: Int) = TextView(this).apply {
        setText(text)
        setPadding(0, dp(8), 0, dp(4))
    }

    private fun edit(hint: Int, type: Int, maxLength: Int) = EditText(this).apply {
        setHint(hint)
        inputType = type
        filters = arrayOf(InputFilter.LengthFilter(maxLength))
    }

    private fun button(label: Int, onClick: (View) -> Unit) = Button(this, null, android.R.attr.borderlessButtonStyle).apply {
        setText(label)
        setOnClickListener(onClick)
    }

    private fun View.withWeight() = apply { layoutParams = LinearLayout.LayoutParams(MATCH_PARENT, 0, 1f) }

    private fun dp(value: Int) = (value * resources.displayMetrics.density).toInt()

    companion object {
        const val DRAFT_ID = "org.microclub.mseismo.DRAFT_ID"

        private const val BACKDROP = 0x40808080

        private var current: WeakReference<ReportActivity>? = null
        val isOpen: Boolean
            get() = current?.get()?.let { !it.isFinishing && !it.isDestroyed } == true
    }
}
