# m-Seismo Flutter example

An iOS and Android example for the `m_seismo` bridge.

## Run

Start the native SDK's fake server from the repository root:

```sh
python3 sdks/ios/Example/fake_server.py
```

Then run the app:

```sh
cd sdks/flutter/example
flutter pub get
flutter run
```

Press **Configure SDK** first. The example points at
`http://127.0.0.1:8787` on iOS and `http://10.0.2.2:8787` on the Android
emulator, with the key `msk_flutter_example`. Android debug builds allow
cleartext traffic for this local server.

## Buttons

- **Configure SDK** connects to the fake server.
- **Open report sheet** opens the native report form (SwiftUI on iOS,
  `ReportActivity` on Android).
- **Submit headless report** sends a report without opening the form.
- **Record Flutter error** persists a non-fatal error through the native
  SDK.
- **Check pending crash** reports it and updates the automatic-report list.
- **Add log / breadcrumb / network** adds diagnostics to the next report.
- **Refresh pending count** and **Retry now** exercise the offline queue.

## Integration test

```sh
# iOS simulator
flutter test integration_test -d "iPhone 17"

# Android emulator
flutter test integration_test -d emulator-5554
```

The test starts its own in-process HTTP server, so the Python server is not
needed for it. The expected report platform and SDK name follow the target
platform.