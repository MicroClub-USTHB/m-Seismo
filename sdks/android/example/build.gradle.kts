plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.android")
}

android {
    namespace = "org.microclub.mseismo.example"
    compileSdk = 36

    defaultConfig {
        applicationId = "org.microclub.mseismo.example"
        minSdk = 24
        targetSdk = 36
        versionCode = 1
        versionName = "0.1.0"

        // Override with -P. 10.0.2.2 is your computer, seen from the emulator.
        val endpoint = providers.gradleProperty("mseismo.endpoint").getOrElse("http://10.0.2.2:8080")
        val appKey = providers.gradleProperty("mseismo.appKey").getOrElse("msk_replace_me")
        buildConfigField("String", "MSEISMO_ENDPOINT", "\"$endpoint\"")
        buildConfigField("String", "MSEISMO_APP_KEY", "\"$appKey\"")
    }

    buildFeatures {
        buildConfig = true
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
}

kotlin {
    compilerOptions { jvmTarget.set(org.jetbrains.kotlin.gradle.dsl.JvmTarget.JVM_17) }
}

dependencies {
    implementation(project(":mseismo"))
}
