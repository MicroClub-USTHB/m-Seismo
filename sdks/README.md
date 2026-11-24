# sdks

The SDK family follows a native-first architecture: each mobile platform keeps its own SDK implementation, and the cross-platform bridges stay as thin wrappers over those native SDKs.

- `ios/`: Swift Package (`MSeismo`) + example app. See `ios/README.md`.
- `flutter/`: iOS-first Flutter bridge package that speaks to the native iOS SDK via platform channels. Android is intentionally deferred until the Kotlin SDK exists. See `flutter/README.md`.
- `web/`: Web SDK (bug-reporting form and automatic diagnostics). See `web/README.md`.
- `web-sentry/`: dependency-free browser reporting adapter with optional Sentry event references. See `web-sentry/README.md`.
