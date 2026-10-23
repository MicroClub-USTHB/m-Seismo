package org.microclub.mseismo

import android.app.Activity
import android.app.Application
import android.content.ComponentCallbacks2

// Screens opened and background/foreground, as breadcrumbs.
internal fun recordLifecycle(app: Application, record: (BreadcrumbKind, String) -> Unit) {
    var hidden = true
    var lastScreen: String? = null
    app.registerActivityLifecycleCallbacks(object : ActivityCallbacks {
        override fun onActivityResumed(activity: Activity) {
            if (hidden) record(BreadcrumbKind.LIFECYCLE, "foreground")
            hidden = false
            val screen = activity.javaClass.simpleName
            // A rotation resumes the same screen: record it once.
            if (screen != lastScreen) record(BreadcrumbKind.NAVIGATION, screen)
            lastScreen = screen
        }
    })
    // UI_HIDDEN: no screen of the app is visible any more.
    app.registerComponentCallbacks(object : ComponentCallbacks2 {
        override fun onTrimMemory(level: Int) {
            if (level == ComponentCallbacks2.TRIM_MEMORY_UI_HIDDEN && !hidden) {
                hidden = true
                record(BreadcrumbKind.LIFECYCLE, "background")
            }
        }

        override fun onConfigurationChanged(newConfig: android.content.res.Configuration) = Unit

        @Suppress("OVERRIDE_DEPRECATION") // required by the interface
        override fun onLowMemory() = Unit
    })
}
