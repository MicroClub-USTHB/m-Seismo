
plugins {
    id("com.android.library")
    kotlin("android")
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

dependencies {
    testImplementation("junit:junit:4.13.2")
}