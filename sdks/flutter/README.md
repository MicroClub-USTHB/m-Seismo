# m-Seismo Flutter SDK

A thin Flutter bridge over the native m-Seismo Swift (iOS) and Kotlin
(Android) SDKs.

- **Status:** iOS and Android bridges implemented.
- **Native source of truth:** `sdks/ios/Sources/MSeismo` and
  `sdks/android/mseismo`.
- **Reports identity:** reports keep the native tags (`platform: "ios"` /
  `"android"` and `sdk.name: "mseismo-ios"` / `"mseismo-android"`).

## Requirements

- Flutter 3.24 or newer.
- Dart 3.6 or newer.
- iOS 13 or newer.
- Android 7.0 (API 24) or newer, `compileSdk` 36, Java 17.
- A full m-Seismo checkout. The package uses a `path:` dependency and is not
  publishable to pub.dev until the native packaging strategy changes.

The Android build is tested with Flutter 3.44.8, which is where the current
Flutter Gradle templates use AGP 9.

## How the native SDKs are linked

The iOS pod compiles the Swift SDK sources directly:

```ruby
s.source_files = 'Classes/**/*.{swift,h}',
                 '../../ios/Sources/MSeismo/**/*.swift'
```

The Android plugin compiles the Kotlin SDK sources through Gradle source sets:

```kotlin
sourceSets {
    getByName("main") {
        java.srcDirs(
            "src/main/kotlin",
            "../../android/mseismo/src/main/java",
        )
        res.srcDirs("../../android/mseismo/src/main/res")
    }
}
```

From `sdks/flutter`, `../../ios` and `../../android` resolve to `sdks/ios`
and `sdks/android`. There is no CocoaPods dependency and no Maven/AAR
dependency on the native packages. **Do not also link the native SDK
separately** from a host app: that would compile a second copy of the SDK
and create two independent SDK instances.

The plugin manifest mirrors the native Android manifest, including the
`ReportActivity` declaration and the permissions used by the native
triggers.

If CocoaPods rejects source globs outside the pod directory, use the
fallback described in [CocoaPods fallback](#cocoapods-fallback).

## Installation

Add the package with a local path:

```yaml
dependencies:
  m_seismo:
    path: /path/to/m-Seismo/sdks/flutter
```

Then run `flutter pub get`.

The iOS plugin is CocoaPods-only. If your project uses Flutter's Swift
Package Manager mode, Flutter automatically falls back to CocoaPods for
this plugin because it has a podspec and no `Package.swift`.

### Host app configuration

iOS: add motion usage text to `ios/Runner/Info.plist` so the shake trigger
can use CoreMotion:

```xml
<key>NSMotionUsageDescription</key>
<string>m-Seismo opens the bug report sheet when you shake the device.</string>
```

For a local HTTP server in debug builds, allow local networking:

```xml
<key>NSAppTransportSecurity</key>
<dict>
  <key>NSAllowsLocalNetworking</key>
  <true/>
</dict>
```

Android: the plugin declares the SDK's internet, network-state and
screen-capture permissions, so the host app does not need to add them.
For a local HTTP server in debug builds, allow cleartext traffic in the
debug manifest:

```xml
<application android:usesCleartextTraffic="true" />
```

HTTP endpoints are only accepted in debuggable builds.

## Initialization

```dart
import 'package:flutter/widgets.dart';
import 'package:m_seismo/m_seismo.dart';

Future<void> main() async {
  WidgetsFlutterBinding.ensureInitialized();

  await MSeismo.instance.configure(
    MSeismoConfig(
      baseUrl: 'https://seismo.example.com',
      sdkKey: 'msk_your_project_key',
      environment: 'production',
      release: '1.4.0',
      shakeEnabled: true,
      screenshotEnabled: true,
    ),
  );

  await MSeismo.instance.checkForPendingCrash();
  runApp(const MyApp());
}
```

`configure` forwards to the native SDK after validating the endpoint, SDK
key and release fields. Invalid configurations throw a `PlatformException`
with code `invalid-configuration`.

On iOS the native SDK observes screenshots and the bridge feeds CoreMotion
shakes into the native detector. On Android the native SDK owns both
triggers. `checkForPendingCrash()` should run at startup after
configuration so a crash from the previous launch is reported
automatically.

## API

### Reports

```dart
await MSeismo.instance.report();
await MSeismo.instance.report(trigger: ReportTrigger.longPress);

final reportId = await MSeismo.instance.submit(
  ReportDraft(
    title: 'Checkout fails',
    description: 'Tapping Pay does nothing.',
    category: ReportCategory.bug,
    severity: ReportSeverity.high,
    trigger: ReportTrigger.manual,
  ),
);
```

`report()` opens the native report form: SwiftUI on iOS and `ReportActivity`
on Android. The form submits through the native SDK, so screenshot
annotation, redaction, context enrichment and offline queueing stay native.

On Android, `report()` requires an attached `Activity`. Calling it without
one throws a `PlatformException` with code `no-activity`.

### Errors

```dart
FlutterError.onError = (details) {
  MSeismo.instance.recordError(
    details.exceptionAsString(),
    stackTrace: details.stack.toString(),
  );
  FlutterError.presentError(details);
};
```

Flutter/Dart errors are persisted by the native SDK and reported on the
next `checkForPendingCrash()` call or app launch. Pass `fatal: true` when
the error should be classified like a crash.

### Diagnostics

```dart
await MSeismo.instance.log('Opened checkout');
await MSeismo.instance.breadcrumb(BreadcrumbKind.navigation, 'Checkout');
await MSeismo.instance.setCapturesNetworkBodies(true);
await MSeismo.instance.recordNetworkRequest(
  NetworkRequest(
    url: 'https://api.example.com/pay',
    method: 'POST',
    statusCode: 500,
    durationMs: 321,
    requestBody: '{"amount":42}',
    responseBody: '{"error":"declined"}',
  ),
);
```

### Flags, experiments, redaction, queue and identifiers

```dart
await MSeismo.instance.setFeatureFlags(<String, Object?>{
  'new_checkout': true,
  'rollout': 25,
  'ratio': 0.5,
  'variant': 'control',
});
await MSeismo.instance.setExperiments(<String, String>{'checkout': 'b'});
await MSeismo.instance.setCrashlyticsInstallationId('firebase-installation-id');
await MSeismo.instance.setRedactionAllowList(<String>['support@example.com']);
await MSeismo.instance.setRedactionDenyList(<String>['internal-codename']);

final pending = await MSeismo.instance.pendingReportCount;
await MSeismo.instance.retryNow();

final installationId = await MSeismo.instance.installationId;
final sessionId = await MSeismo.instance.sessionId;
final traceId = await MSeismo.instance.newTraceId();
```

`installationId` throws a `PlatformException` with code `not-configured`
until `configure()` succeeds.

### Automatic reports

```dart
MSeismo.instance.autoReports.listen((report) {
  debugPrint('Auto-reported ${report.id}: ${report.crash?.reason}');
});
```

The stream is broadcast and is fed by the native `onAutoReport` callback
through Pigeon on both platforms.

## Limitations

- iOS and Android only. Every channel-backed member throws
  `UnsupportedError` on other platforms; the package is still web-safe to
  compile.
- Reports are tagged with the native platform and SDK name.
- Android `pendingReportCount` waits for the native worker thread and may
  block briefly if the SDK is busy sending.
- Sheet presentation is observed through the native SDK state; there is no
  new native callback.

## Development

```sh
flutter pub get
dart run pigeon --input pigeons/messages.dart
dart format lib test pigeons example/lib example/integration_test
flutter analyze
flutter test

cd example
flutter build apk --debug
```

The Flutter CI workflow also checks Pigeon drift for the Dart, Swift and
Kotlin outputs, runs the Dart checks and builds the example APK.

## CocoaPods fallback

If CocoaPods refuses to compile source files outside the pod directory,
symlink or copy the native Swift sources into `ios/Classes/Native` and
update `m_seismo.podspec` accordingly. Keep only one copy of the sources
in the build.