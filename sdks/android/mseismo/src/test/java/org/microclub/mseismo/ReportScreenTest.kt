package org.microclub.mseismo

import android.app.Activity
import android.app.Application
import android.content.Intent
import android.hardware.Sensor
import android.hardware.SensorManager
import android.os.Looper
import android.view.View
import android.view.ViewGroup
import android.widget.EditText
import android.widget.TextView
import androidx.test.core.app.ApplicationProvider
import org.json.JSONObject
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.Robolectric
import org.robolectric.RobolectricTestRunner
import org.robolectric.Shadows.shadowOf
import org.robolectric.android.controller.ActivityController
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import org.robolectric.shadows.SensorBuilder
import org.robolectric.shadows.SensorEventBuilder
import java.time.Duration
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
@GraphicsMode(GraphicsMode.Mode.NATIVE) // real bitmaps, so the screen capture works
class ReportScreenTest {
    private val app: Application = ApplicationProvider.getApplicationContext()
    private val realSend = MSeismo.send
    private val valid = Configuration(endpoint = "https://seismo.example.com", appKey = "msk_test")
    private val sent = CountDownLatch(1)
    private var sentJson = JSONObject()
    private var sentScreenshot: ByteArray? = null
    private var sends = 0
    private val opened = mutableListOf<ActivityController<ReportActivity>>()

    init {
        MSeismo.send = { _, _, json, screenshot ->
            sends++
            sentJson = JSONObject(json)
            sentScreenshot = screenshot
            sent.countDown()
            SendResult.Accepted
        }
        app.getSystemService(SensorManager::class.java).let {
            shadowOf(it).addSensor(SensorBuilder.newBuilder().setType(Sensor.TYPE_ACCELEROMETER).build())
        }
    }

    // A report screen left open would rightly block the next test from opening one.
    @After
    fun cleanUp() {
        MSeismo.send = realSend
        opened.forEach { if (!it.get().isDestroyed) it.pause().stop().destroy() }
    }

    // 4 hard jolts over 0.8 s. Robolectric's clock only moves when told to.
    private fun shakePhone() {
        val sensors = app.getSystemService(SensorManager::class.java)
        val accelerometer = sensors.getDefaultSensor(Sensor.TYPE_ACCELEROMETER)!!
        repeat(4) {
            for (values in listOf(floatArrayOf(30f, 0f, 9.8f), floatArrayOf(0f, 0f, 9.8f))) {
                shadowOf(sensors).sendSensorEventToListeners(SensorEventBuilder.newBuilder(accelerometer, values).build())
            }
            shadowOf(Looper.getMainLooper()).idleFor(Duration.ofMillis(200))
        }
    }

    private fun appScreen(): ActivityController<Activity> = Robolectric.buildActivity(Activity::class.java).setup()

    // The report screen the SDK asked to open, already showing; null if none was opened.
    private fun reportScreen(from: Activity): ActivityController<ReportActivity>? {
        shadowOf(Looper.getMainLooper()).idle() // the screen capture finishes asynchronously
        val intent: Intent = shadowOf(from).nextStartedActivity ?: return null
        return Robolectric.buildActivity(ReportActivity::class.java, intent).setup().also { opened += it }
    }

    private fun Activity.tap(text: String) {
        val view = find(window.decorView) { it is TextView && it.text.toString() == text }
        assertNotNull("no \"$text\" on screen", view)
        view!!.performClick()
        shadowOf(Looper.getMainLooper()).idle()
    }

    private fun Activity.type(hint: String, text: String) {
        (find(window.decorView) { it is EditText && it.hint == hint } as EditText).setText(text)
    }

    // The form's second dropdown is severity.
    private fun Activity.choose(severity: Int) {
        val dropdowns = mutableListOf<android.widget.Spinner>()
        fun collect(v: View) {
            if (v is android.widget.Spinner) dropdowns += v
            if (v is ViewGroup) for (i in 0 until v.childCount) collect(v.getChildAt(i))
        }
        collect(window.decorView)
        dropdowns[1].setSelection(severity)
    }

    private fun find(view: View, match: (View) -> Boolean): View? {
        if (match(view)) return view
        if (view is ViewGroup) for (i in 0 until view.childCount) find(view.getChildAt(i), match)?.let { return it }
        return null
    }

    @Test
    fun shakeOpensTheScreenshotThenTheFormAndSendsEverything() {
        MSeismo.configure(app, valid)
        val screen = appScreen()

        shakePhone()
        val report = reportScreen(screen.get())!!.get()
        assertEquals("nothing is sent before Send", 0, sends)

        report.tap("Continue") // keeps the screenshot
        report.type("Title", "Pay button does nothing")
        report.choose(severity = 3) // "Not set", Low, Medium, High
        report.tap("Send")

        assertTrue(sent.await(5, TimeUnit.SECONDS))
        assertEquals("shake", sentJson.getString("trigger"))
        assertEquals("Pay button does nothing", sentJson.getJSONObject("user_input").getString("title"))
        assertEquals("high", sentJson.getJSONObject("user_input").getString("severity"))
        assertTrue("the screenshot goes with the report", sentScreenshot!!.size > 100)
        assertTrue(report.isFinishing)
    }

    @Test
    fun skipScreenshotSendsWithoutIt() {
        MSeismo.configure(app, valid)
        val screen = appScreen()
        MSeismo.showReportScreen(screen.get())
        val report = reportScreen(screen.get())!!.get()

        report.tap("Skip screenshot")
        report.tap("Send")

        assertTrue(sent.await(5, TimeUnit.SECONDS))
        assertEquals("manual", sentJson.getString("trigger"))
        assertTrue("severity left at Not set", !sentJson.getJSONObject("user_input").has("severity"))
        assertNull(sentScreenshot)
    }

    @Test
    fun cancelSendsNothingAndForgetsTheReport() {
        MSeismo.configure(app, valid)
        val screen = appScreen()
        MSeismo.showReportScreen(screen.get())
        val controller = reportScreen(screen.get())!!
        val draftId = controller.get().intent.getStringExtra(ReportActivity.DRAFT_ID)!!

        controller.get().tap("Cancel")
        controller.pause().stop().destroy()

        assertEquals(0, sends)
        assertNull("the screenshot must not stay in memory", MSeismo.draftFor(draftId))
    }

    @Test
    fun backFromTheFormReturnsToTheDrawing() {
        MSeismo.configure(app, valid)
        val screen = appScreen()
        MSeismo.showReportScreen(screen.get())
        val report = reportScreen(screen.get())!!.get()

        report.tap("Continue")
        report.tap("Back")

        report.tap("Undo") // only on the drawing screen
        assertTrue(!report.isFinishing)
    }

    @Test
    fun aSecureScreenIsNeverCaptured() {
        MSeismo.configure(app, valid)
        val screen = appScreen()
        screen.get().window.addFlags(android.view.WindowManager.LayoutParams.FLAG_SECURE) // e.g. a card entry screen
        MSeismo.showReportScreen(screen.get())
        val report = reportScreen(screen.get())!!.get()

        report.tap("Send") // straight to the form: there is no screenshot to draw on

        assertTrue(sent.await(5, TimeUnit.SECONDS))
        assertNull(sentScreenshot)
    }

    @Test
    fun thePhonesBackButtonOnTheFormReturnsToTheDrawing() {
        MSeismo.configure(app, valid)
        val screen = appScreen()
        MSeismo.showReportScreen(screen.get())
        val report = reportScreen(screen.get())!!.get()

        report.tap("Continue")
        @Suppress("DEPRECATION") report.onBackPressed()

        report.tap("Undo") // only on the drawing screen
        assertTrue(!report.isFinishing)
    }

    @Test
    fun aSecondTriggerWhileTheScreenIsOpenDoesNotOpenAnother() {
        MSeismo.configure(app, valid)
        val screen = appScreen()
        MSeismo.showReportScreen(screen.get())
        reportScreen(screen.get())!!

        MSeismo.showReportScreen(screen.get())

        assertNull(reportScreen(screen.get()))
    }

    @Test
    fun aClosingScreenThatIsNotDestroyedYetDoesNotBlockTheNextReport() {
        MSeismo.configure(app, valid)
        val screen = appScreen()
        MSeismo.showReportScreen(screen.get())
        reportScreen(screen.get())!!.get().finish() // Send or Cancel; Android destroys it a moment later

        MSeismo.showReportScreen(screen.get())

        assertNotNull(reportScreen(screen.get()))
    }

    @Test
    fun aScreenDestroyedWhileHiddenDoesNotKeepItsScreenshotForever() {
        MSeismo.configure(app, valid)
        val screen = appScreen()
        MSeismo.showReportScreen(screen.get())
        val hidden = reportScreen(screen.get())!!
        val oldDraft = hidden.get().intent.getStringExtra(ReportActivity.DRAFT_ID)!!
        hidden.pause().stop().destroy() // the system reclaims it in the background, without finishing it

        MSeismo.showReportScreen(screen.get())
        reportScreen(screen.get())

        assertNull(MSeismo.draftFor(oldDraft))
    }

    // A small phone with large text, as many users set it: every button must stay fully on screen.
    @Test
    @Config(qualifiers = "w320dp-h640dp", fontScale = 1.3f)
    fun everyButtonFitsOnASmallScreenWithLargeText() {
        MSeismo.configure(app, valid)
        val screen = appScreen()
        MSeismo.showReportScreen(screen.get())
        val report = reportScreen(screen.get())!!.get()

        for (label in listOf("Pen", "Black out", "Undo", "Cancel", "Skip screenshot", "Continue")) report.assertFullyVisible(label)
        report.tap("Continue")
        for (label in listOf("Back", "Send")) report.assertFullyVisible(label)
    }

    // On screen, at least 48 dp wide (Android's minimum touch size), and its whole text shown.
    private fun Activity.assertFullyVisible(text: String) {
        val view = find(window.decorView) { it is TextView && it.text.toString() == text } as TextView
        val left = IntArray(2).also { view.getLocationInWindow(it) }[0]
        val shown = view.transformationMethod?.getTransformation(view.text, view) ?: view.text
        val textWidth = view.paint.measureText(shown.toString())
        val room = view.width - view.totalPaddingLeft - view.totalPaddingRight
        val where = "x=$left w=${view.width}, screen ${window.decorView.width}"
        assertTrue("\"$text\" is off screen: $where", left >= 0 && left + view.width <= window.decorView.width)
        assertTrue("\"$text\" is too small to tap: $where", view.width >= 48 * resources.displayMetrics.density)
        assertTrue("\"$text\" is cut off: needs $textWidth, has $room", textWidth <= room + 1)
    }

    @Test
    fun theTitleStopsAtTheLimitInsteadOfBeingCutWhenSent() {
        MSeismo.configure(app, valid)
        val screen = appScreen()
        MSeismo.showReportScreen(screen.get())
        val report = reportScreen(screen.get())!!.get()
        report.tap("Skip screenshot")

        report.type("Title", "x".repeat(300))

        val title = find(report.window.decorView) { it is EditText && it.hint == "Title" } as EditText
        assertEquals(200, title.text.length)
    }

    // Dark mode on a small screen: the night theme loads and every button fits. (Right-to-left: see ButtonRowTest.)
    @Test
    @Config(qualifiers = "w320dp-h640dp-night")
    fun worksInDarkModeOnASmallScreen() {
        MSeismo.configure(app, valid)
        val screen = appScreen()
        MSeismo.showReportScreen(screen.get())
        val report = reportScreen(screen.get())!!.get()

        for (label in listOf("Pen", "Black out", "Undo", "Cancel", "Skip screenshot", "Continue")) report.assertFullyVisible(label)
        report.tap("Continue")
        for (label in listOf("Back", "Send")) report.assertFullyVisible(label)
    }

    @Test
    fun shakeDoesNothingWhenTurnedOff() {
        MSeismo.configure(app, valid.copy(shakeEnabled = false))
        val screen = appScreen()
        shakePhone()
        assertNull(reportScreen(screen.get()))
    }

    @Test
    fun shakeIsIgnoredOnceTheScreenIsHidden() {
        MSeismo.configure(app, valid)
        val screen = appScreen().pause()
        shakePhone()
        assertNull(reportScreen(screen.get()))
    }

    @Test
    fun theSelectedToolStaysSelectedAfterGoingBackFromTheForm() {
        MSeismo.configure(app, valid)
        val screen = appScreen()
        MSeismo.showReportScreen(screen.get())
        val report = reportScreen(screen.get())!!.get()

        report.tap("Black out")
        report.tap("Continue")
        report.tap("Back")

        val blackOut = find(report.window.decorView) { it is TextView && it.text.toString() == "Black out" }!!
        val pen = find(report.window.decorView) { it is TextView && it.text.toString() == "Pen" }!!
        assertEquals("the button shown selected is the tool that draws", 1f, blackOut.alpha)
        assertEquals(0.5f, pen.alpha)
    }

    // Many phones switch to dark mode on a schedule: that must not wipe what the user typed.
    @Test
    fun switchingToDarkModeKeepsWhatTheUserTyped() {
        MSeismo.configure(app, valid)
        val screen = appScreen()
        MSeismo.showReportScreen(screen.get())
        val controller = reportScreen(screen.get())!!
        val report = controller.get()
        report.tap("Skip screenshot")
        report.type("Title", "Half-written report")

        val night = android.content.res.Configuration(report.resources.configuration).apply {
            uiMode = android.content.res.Configuration.UI_MODE_NIGHT_YES or android.content.res.Configuration.UI_MODE_TYPE_NORMAL
        }
        controller.configurationChange(night)

        assertTrue("same screen, not rebuilt", controller.get() === report)
        val title = find(report.window.decorView) { it is EditText && it.hint == "Title" } as EditText
        assertEquals("Half-written report", title.text.toString())
    }

    @Test
    fun talkBackKnowsTheSelectedToolAndWhatEachDropdownIs() {
        MSeismo.configure(app, valid)
        val screen = appScreen()
        MSeismo.showReportScreen(screen.get())
        val report = reportScreen(screen.get())!!.get()

        report.tap("Black out")
        val blackOut = find(report.window.decorView) { it is TextView && it.text.toString() == "Black out" }!!
        val pen = find(report.window.decorView) { it is TextView && it.text.toString() == "Pen" }!!
        assertTrue(blackOut.isSelected)
        assertTrue(!pen.isSelected)

        report.tap("Continue")
        for (label in listOf("Category", "Severity")) {
            val text = find(report.window.decorView) { it is TextView && it.text.toString() == label } as TextView
            val dropdown = report.window.decorView.findViewById<View>(text.labelFor)
            assertTrue("$label labels its dropdown", dropdown is android.widget.Spinner)
        }
    }
}
