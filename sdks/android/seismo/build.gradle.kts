
plugins {
    id("com.android.library")
}

android {
    namespace = "io.mseismo.android"
    compileSdk {
        version = release(36) {
            minorApiLevel = 1
        }
    }

    defaultConfig {
        minSdk = 24
    }
}
