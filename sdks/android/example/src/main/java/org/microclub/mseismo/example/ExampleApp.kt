package org.microclub.mseismo.example

import android.app.Application
import org.microclub.mseismo.Configuration
import org.microclub.mseismo.MSeismo

class ExampleApp : Application() {
    override fun onCreate() {
        super.onCreate()
        MSeismo.configure(
            this,
            Configuration(
                endpoint = BuildConfig.MSEISMO_ENDPOINT,
                appKey = BuildConfig.MSEISMO_APP_KEY,
                environment = "development",
            ),
        )
        MSeismo.log("Example app started")
    }
}
