# m-Seismo Flutter example

An iOS-only example for the `m_seismo` bridge.

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
`http://127.0.0.1:8787` with `example-project-key`.

## Buttons

- **Configure SDK** connects to the fake server.
- **Open report sheet** presents the native SwiftUI form.
- **Submit headless report** sends a report without opening the sheet.
- **Record Flutter error** persists a non-fatal error through `CrashStore`.
- **Check pending crash** reports it and updates the automatic-report list.
- **Add log / breadcrumb / network** adds diagnostics to the next report.
- **Refresh pending count** and **Retry now** exercise the offline queue.

## Integration test

```sh
flutter test integration_test -d "iPhone 17"
```

The test starts its own in-process HTTP server, so the Python server is not
needed for it.
