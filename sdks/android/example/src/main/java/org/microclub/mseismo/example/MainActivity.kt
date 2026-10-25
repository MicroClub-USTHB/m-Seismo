package org.microclub.mseismo.example

import android.app.Activity
import android.os.Bundle
import android.widget.Button
import android.widget.FrameLayout
import android.widget.LinearLayout
import android.widget.TextView
import org.microclub.mseismo.BreadcrumbKind
import org.microclub.mseismo.Category
import org.microclub.mseismo.MSeismo
import org.microclub.mseismo.Severity

class MainActivity : Activity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        val config = MSeismo.configuration
        val status = TextView(this).apply {
            textSize = 16f
            text = """
                m-Seismo ${MSeismo.SDK_VERSION}

                configured: ${config != null}
                endpoint: ${config?.endpoint}
                environment: ${config?.environment}
                release: ${MSeismo.release}
                installation: ${MSeismo.installationId}
                session: ${MSeismo.sessionId}
            """.trimIndent()
        }
        val result = TextView(this).apply { textSize = 16f }
        val send = Button(this).apply {
            text = "Send test report"
            setOnClickListener {
                MSeismo.breadcrumb(BreadcrumbKind.TAP, "Send test report button")
                val id = MSeismo.report(
                    title = "Test report from the example app",
                    description = "Sent with the Send test report button.",
                    category = Category.BUG,
                    severity = Severity.LOW,
                )
                result.text = if (id == null) "m-Seismo is off, see logcat" else "Sent report $id\nResult: adb logcat -s MSeismo"
            }
        }
        val reportBug = Button(this).apply {
            text = "Report a bug"
            setOnClickListener { MSeismo.showReportScreen(this@MainActivity) }
        }
        val content = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            setPadding(48, 48, 48, 48)
            addView(status)
            addView(reportBug)
            addView(send)
            addView(result)
        }
        setContentView(FrameLayout(this).apply {
            fitsSystemWindows = true // keeps content below the top bar
            addView(content)
        })
    }
}
