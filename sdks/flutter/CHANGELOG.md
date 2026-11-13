## 0.1.0

- Initial iOS and Android Flutter bridge over the native m-Seismo SDKs.
- Pigeon host API for configuration, reports, errors, diagnostics, flags,
  experiments, redaction, queue state and identifiers.
- Native report sheet presentation through the existing published state.
- CoreMotion shake monitor and screenshot-trigger observation through the
  native configuration on iOS; the native Kotlin SDK owns both triggers on
  Android.
- Privacy manifest for the native installation ID's `UserDefaults` access.
- Android plugin links the Kotlin SDK sources through Gradle source sets
  and uses the native report screen.
- Example app and integration tests.
