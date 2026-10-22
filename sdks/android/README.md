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

Send a report from anywhere, for example a "Report a bug" button. It is sent
in the background and returns the report ID, or `null` when the SDK is off:

```kotlin
MSeismo.report(
    title = "Checkout button does nothing",
    description = "Tapped Pay twice.",
    category = Category.BUG,
    severity = Severity.HIGH,
)
```

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

The example reads its endpoint and key from Gradle properties. Start the
backend with `make dev-up` from the repository root, then get a key:
`scripts/dev-key.sh` creates an app linked to a project on the fake GitHub
and prints its SDK key.

```sh
KEY=$(scripts/dev-key.sh)

# Emulator: 10.0.2.2 is your computer, the default endpoint.
./gradlew :example:installDebug -Pmseismo.appKey=$KEY

# Phone over USB: forward the port, then use 127.0.0.1.
adb reverse tcp:8080 tcp:8080
./gradlew :example:installDebug -Pmseismo.endpoint=http://127.0.0.1:8080 -Pmseismo.appKey=$KEY
```

Tap **Send test report**, then check `adb logcat -s MSeismo` for
`report … accepted`. The issue appears on the fake GitHub at
`http://localhost:9090/_fake/issues`.

To keep them, put `mseismo.appKey=msk_…` (and `mseismo.endpoint=…` if needed)
in `~/.gradle/gradle.properties`, not in the repository.
