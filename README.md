# m-Seismo

m-Seismo is an open-source internal tool from MicroClub that turns mobile and web bug reports into complete GitHub issues. Reports from our mobile and web apps usually arrive incomplete, so m-Seismo captures the context at the moment of the bug (device state, logs, user actions, screenshot, server logs, crash data) and files the issue automatically.

## Components
- `spec/`: contracts shared by every component (report schema, ingestion API, SDK conformance cases)
- `backend/`: Go service for ingestion, enrichment, deduplication and GitHub publishing
- `agent/`: Rust agent that collects and redacts server logs and diagnostics on demand
- `sdks/`: native iOS, Android and web SDKs, plus the Flutter bridge; a React Native SDK is planned
- `deploy/`: local development stack and deployment configuration

## Status
Backend management, report ingestion, automatic GitHub issue publishing, and per-app Sentry event resolution are implemented. The Crashlytics export adapter remains pending. The iOS SDK, Android SDK, web SDK, and Flutter bridge are available under `sdks/`; the Flutter bridge depends on a local checkout of the native SDKs. See [crash-provider setup](backend/docs/crash-providers.md).

## Make commands

Run these commands from the repository root.

- `make fmt`: format the Go backend and Rust agent.
- `make lint`: run the Go and Rust linters.
- `make check`: check formatting and linting, then build and test the Go backend and Rust agent.
- `make flutter-check`: format-check, analyze, and unit-test the Flutter bridge (requires Flutter).
- `make flutter-pigeon`: regenerate the Pigeon bridge files.
