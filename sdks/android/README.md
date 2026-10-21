# MSeismo (Android SDK)

Kotlin library for the m-Seismo Android SDK. Minimum Android 7.0 (API 24).

```
sdks/android/
  mseismo/     # the SDK (Android library → AAR)
  example/     # test app, consumes :mseismo as a project dependency
```

Reports follow [`spec/report.schema.json`](../../spec/report.schema.json) and
[`spec/ingestion.md`](../../spec/ingestion.md).

## Using it

```kotlin
class MyApp : Application() {
    override fun onCreate() {
        super.onCreate()
        MSeismo.configure(this, Configuration(
            endpoint = "https://seismo.example.com",
            appKey = "msk_…",
            environment = "production",
        ))
    }
}
```

`release` defaults to the app's `versionName` and `versionCode`.

`configure` never throws. If the configuration is invalid it logs the reason
(`adb logcat -s MSeismo`) and the SDK stays off. `endpoint` must be `https://`
in release builds; `http://` is accepted only in debuggable builds, for a
local backend.

## Developing

Needs JDK 17+ and the Android SDK (`local.properties` with `sdk.dir`, which
Android Studio writes for you).

```sh
cd sdks/android
./gradlew :mseismo:testDebugUnitTest   # unit tests (JVM, no device)
./gradlew :mseismo:assembleRelease     # builds mseismo/build/outputs/aar/mseismo-release.aar
./gradlew :example:installDebug        # installs the example on a running emulator/device
```

Or open `sdks/android` in Android Studio and run the `example` configuration.

### Example app against the local backend

The example reads its endpoint and key from Gradle properties. The default
endpoint is `http://10.0.2.2:8080`, the host machine as seen from the
emulator, where `make dev-up` serves the backend.

```sh
./gradlew :example:installDebug -Pmseismo.appKey=msk_…
```

To keep them, put `mseismo.appKey=msk_…` (and `mseismo.endpoint=…` if needed)
in `~/.gradle/gradle.properties`, not in the repository.
