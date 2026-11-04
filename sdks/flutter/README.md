# m-Seismo Flutter SDK

A thin, iOS-only Flutter bridge over the native m-Seismo Swift SDK.

- **Status:** iOS bridge implemented. Android is deferred until the native
  Kotlin SDK exists.
- **Native source of truth:** `sdks/ios/Sources/MSeismo`.
- **Reports identity:** reports are still tagged `platform: "ios"` and
  `sdk.name: "mseismo-ios"`. Tagging them as Flutter would require a change
  to the native Swift SDK, which this bridge deliberately does not make.

## Requirements

- Flutter 3.24 or newer.
- Dart 3.6 or newer.
- iOS 13 or newer.
- A full m-Seismo checkout. The package uses a `path:` dependency and is not
  publishable to pub.dev until the native packaging strategy changes.

## How the native SDK is linked

The plugin pod compiles the existing Swift SDK sources directly:

```ruby
s.source_files = 'Classes/**/*.{swift,h}',
                 '../../ios/Sources/MSeismo/**/*.swift'
```

From `sdks/flutter/ios`, `../../ios` resolves to `sdks/ios`. There is no
`pod 'MSeismo'` dependency and no separate native package link in the host
app. **Do not also link the native MSeismo package separately** from a host
app: that would compile a second copy of the SDK and create two independent
SDK instances.

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

The plugin is CocoaPods-only. If your project uses Flutter's Swift Package
Manager mode, Flutter will automatically fall back to CocoaPods for this
plugin because it has a podspec and no `Package.swift`.

### Host app configuration

Add motion usage text to `ios/Runner/Info.plist` so the shake trigger can
use CoreMotion:

```xml
<key>NSMotionUsageDescription</key>
<string>m-Seismo opens the bug report sheet when you shake the device.</string>
```

When testing against a local server over HTTP, also allow local networking:

```xml
<key>NSAppTransportSecurity</key>
<dict>
  <key>NSAllowsLocalNetworking</key>
  <true/>
</dict>
```

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

`configure` forwards to the native `MSeismo.shared.configure`, starts or
stops the bridge's CoreMotion shake monitor, and lets the native SDK observe
screenshots. `checkForPendingCrash()` should run at startup after
configuration so a crash from the previous launch is reported automatically.

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

`report()` opens the native SwiftUI report form. The form submits through
the native SDK, so screenshot annotation, redaction, context enrichment and
offline queueing stay native.

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

Flutter/Dart errors are persisted through the native public `CrashStore`
and reported on the next `checkForPendingCrash()` call or app launch. Pass
`fatal: true` when the error should be classified like a crash.

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

### Automatic reports

```dart
MSeismo.instance.autoReports.listen((report) {
  debugPrint('Auto-reported ${report.id}: ${report.crash?.reason}');
});
```

The stream is broadcast and is fed by the native `onAutoReport` callback
through Pigeon.

## Limitations

- iOS only. Every channel-backed member throws `UnsupportedError` on other
  platforms; the package is still web-safe to compile.
- Reports are tagged `platform: "ios"` and `sdk.name: "mseismo-ios"`.
- Sheet presentation is observed through the native
  `@Published isReportSheetPresented`; there is no new native callback.
- Logs, breadcrumbs, and network entries receive native timestamps. The
  bridge cannot supply custom timestamps.
- Shake cannot be exercised in the iOS Simulator. Test it on a physical
  device with `NSMotionUsageDescription` set.
- The package is a monorepo/local-path dependency, not a pub.dev package.
- Do not link the native `MSeismo` Swift package in an app that also uses
  this plugin.

## Testing

From `sdks/flutter`:

```sh
flutter pub get
dart run pigeon --input pigeons/messages.dart
dart format lib test pigeons example/lib example/integration_test
flutter analyze
flutter test
```

On macOS with Xcode:

```sh
cd example
flutter pub get
flutter build ios --simulator --no-codesign
flutter test integration_test -d "iPhone 17"
```

The integration test starts an in-process HTTP server and verifies the
multipart payload, offline-queue submission, automatic crash reporting and
feature-flag scalar types.

Manual checks:

1. Start the fake server: `python3 sdks/ios/Example/fake_server.py`.
2. Run the example app and press **Configure SDK**.
3. Verify **Open report sheet**, **Submit headless report**, screenshot
   trigger (Cmd+S in the simulator), Cancel, Send and swipe-dismiss.
4. On a physical device, verify shake opens the sheet.
5. Turn off the network, submit a report, and confirm it is sent by
   **Retry now** or after the app relaunches.

## Regenerating Pigeon

Pigeon is pinned to `25.3.0` so generated output is stable:

```sh
dart run pigeon --input pigeons/messages.dart
dart format lib/src/messages.g.dart
```

Commit `lib/src/messages.g.dart` and `ios/Classes/Messages.g.swift` together
with the Pigeon source.

## CocoaPods fallback

If CocoaPods refuses the `../../ios/Sources/MSeismo/**/*.swift` glob:

1. Add `ios/MSeismo/MSeismo.podspec` under this package with
   `s.source_files = '../../../ios/Sources/MSeismo/**/*.swift'`.
2. Make `ios/m_seismo.podspec` depend on `MSeismo`.
3. In the host app's `Podfile`, add
   `pod 'MSeismo', :path => '../../ios/MSeismo'`.

This still keeps every file under `sdks/flutter/`; no changes are made to
`sdks/ios/`.
