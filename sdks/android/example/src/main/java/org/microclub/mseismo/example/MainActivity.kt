package org.microclub.mseismo.example

import android.app.Activity
import android.os.Bundle
import android.widget.FrameLayout
import android.widget.TextView
import org.microclub.mseismo.MSeismo

class MainActivity : Activity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        val config = MSeismo.configuration
        val text = TextView(this).apply {
            setPadding(48, 48, 48, 48)
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
        setContentView(FrameLayout(this).apply {
            fitsSystemWindows = true // keeps text below the top bar
            addView(text)
        })
    }
}
