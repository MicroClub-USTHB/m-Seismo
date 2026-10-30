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

Reports also start without any code from you (FR-05, FR-07):

- **Shake**: a firm shake, back and forth twice. Light shakes and small flicks are
  ignored on purpose. On by default; `Configuration(shakeEnabled = false)` turns it off.
- **Screenshot**: taking a screenshot, on Android 14+ only. Off by default;
  turn it on with `Configuration(screenshotEnabled = true)`.

Both only listen while a screen of your app is showing, and never send on
their own: they open the report screen below, where the user can still cancel. The SDK
declares the `DETECT_SCREEN_CAPTURE` permission for the screenshot trigger;
it is granted at install, with no prompt.

### The report screen

Shake, screenshot, or a call to `MSeismo.showReportScreen(activity)` (for a
"Report a bug" button) opens the report screen. It first captures the screen
the user was on, so they can circle the problem with the pen or hide private
parts with the black-out marker, then shows a short form: title, description,
category and severity. "Send report" uploads it with the drawn-on
screenshot; "Skip screenshot" sends it without one. `MSeismo.report(...)`
still sends a report straight away, without the screen.

The screen takes your app's brand color on its own: the `colorPrimary` of the
theme of the screen the user was on. It follows the phone's light or dark mode.
To choose the color yourself, set `MSeismo.accentColor` (0xAARRGGBB), as often
as your colors change.

Screens your app marks with `FLAG_SECURE` are never captured. To translate the
screen, override the `mseismo_*` strings in your own `values-xx/strings.xml`
and keep the same number of items in `mseismo_categories` and
`mseismo_severities`.

### Jetpack Compose

Compose keeps its colors in code and its screens inside one activity, so tell
m-Seismo about both. In Kotlin Multiplatform, do it from `androidMain`.

```kotlin
// Inside your theme: the report screen follows your light, dark and theme changes.
val primary = MaterialTheme.colorScheme.primary
SideEffect { MSeismo.accentColor = primary.toArgb() }

// Where your navigation changes screen: recorded as a breadcrumb.
LaunchedEffect(currentRoute) { MSeismo.screen(currentRoute) }
```

`MSeismo.screen` records the same screen only once in a row, so calling it
again on a recomposition or rotation is fine.

### Crashes

When your app crashes, m-Seismo saves a crash report on the spot, with the
exception, its stack trace and the context below, then lets the crash go on
as usual, so Crashlytics still gets it (FR-08). The report is sent the next
time the app opens, with no screen and nothing for the user to do. Errors you
catch yourself can be sent right away with `MSeismo.recordError(e)` (FR-09).

Only crashes in Kotlin or Java code are caught; native (NDK) crashes and ANRs
are not. Release builds shrunk with R8 send obfuscated stack traces, as they
do to Crashlytics.

Each report also carries context, already redacted on the device:

- **Device**: OS, model, language, battery, free memory, network type and
  orientation, read automatically.
- **Logs**: the last 200 lines you pass to `MSeismo.log("Cart loaded")`.
- **Breadcrumbs**: the last 50 user actions. Screens opened and the app
  going to the background are recorded automatically; add your own with
  `MSeismo.breadcrumb(BreadcrumbKind.TAP, "Pay button")`.
- **Network requests**: the last 50 you pass to
  `MSeismo.recordRequest(url, method, statusCode, durationMs)`, for example
  from an OkHttp interceptor. Request and response bodies are only kept when
  `Configuration(captureNetworkBodies = true)`.

Before anything leaves the phone, emails, phone numbers, card numbers and
`Bearer` tokens are replaced with `[REDACTED]`. Change this with
`redactionRules` (default `Configuration.DEFAULT_REDACTION_RULES`),
`redactionAllowList` (never redacted) and `redactionDenyList` (always
redacted).

Reports are saved on the device (`no_backup/mseismo-queue`, one file per
report) before they are sent, and kept until the backend accepts or rejects
them. Without a connection, or while the backend is down, they are retried
after 2, 4, 8… seconds (up to 5 minutes), as soon as the network comes back,
and on the next app launch. At most 100 are kept; the oldest go first.

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
