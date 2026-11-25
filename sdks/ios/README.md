# MSeismo (iOS SDK)

Swift Package for the m-Seismo iOS SDK.

```
sdks/ios/
  Package.swift
  Sources/MSeismo/       # the SDK itself
  Tests/MSeismoTests/
  Example/                # xcodegen-generated example app, consumes MSeismo via local path
    project.yml
    ExampleApp/
```

## Developing

```sh
cd sdks/ios
swift build
swift test
```

### Example app

`Example/ExampleApp.xcodeproj` is generated, not committed (`xcodegen` is the
source of truth: `Example/project.yml`). Regenerate after cloning or whenever
`project.yml` changes:

```sh
brew install xcodegen   # once
cd sdks/ios/Example
xcodegen generate
open ExampleApp.xcodeproj
```

### Testing the network layer / offline queue against something real

`Example/fake_server.py` is a throwaway local HTTP server (not part of the
SDK or CI) for exercising `URLSessionTransport`/`OfflineQueue` against a real
server instead of only mocks:

```sh
cd sdks/ios/Example
python3 fake_server.py          # logs every /reports POST it receives
python3 fake_server.py 2        # fails the first 2 requests with 503, to watch retry/backoff
```

The example app points at `http://127.0.0.1:8787` (the SDK adds
`/api/v1/reports`) and submits a test
report automatically at launch (`ExampleApp.swift`) — no UI interaction
needed, just run the server and launch the app.

## CI

- `.github/workflows/ios-ci.yml` — on every push/PR touching `sdks/ios/**`:
  builds and tests the package, then generates and builds the example app.
- `.github/workflows/ios-release.yml` — on a pushed tag matching `ios-v*`
  (e.g. `ios-v0.1.0`): rebuilds in release config, runs tests, and publishes
  a GitHub Release. Other SDKs in this repo (Android, Flutter, RN) will get
  their own tag prefix later, so one SDK's release never fires on another's
  changes.

To cut a release:

```sh
git tag ios-v0.1.0
git push origin ios-v0.1.0
```

## Known limitation: this package is not yet remotely installable via SPM

`Package.swift` lives at `sdks/ios/`, not at the repository root, which is
where Swift Package Manager expects it when a consumer adds this repo as a
`.package(url: ..., from: ...)` dependency. Right now this package is only
consumable via a local path (as the example app does) or a git submodule.

If/when this SDK needs to be remotely installable by other teams, the
release workflow will need an added step that mirrors `sdks/ios/` to a
dedicated repo (or branch) at its root and tags it there — not done yet,
since it needs a target repo and push credentials decided first.
