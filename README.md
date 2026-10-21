# m-Seismo

m-Seismo is an open-source internal tool from MicroClub that turns mobile and web bug reports into complete GitHub issues. Reports from our mobile and web apps usually arrive incomplete, so m-Seismo captures the context at the moment of the bug (device state, logs, user actions, screenshot, server logs, crash data) and files the issue automatically.

## Components
- `spec/`: contracts shared by every component (report schema, ingestion API, SDK conformance cases)
- `backend/`: Go service for ingestion, enrichment, deduplication and GitHub publishing
- `agent/`: Rust agent that collects and redacts server logs and diagnostics on demand
- `sdks/`: iOS SDK and web reporting adapter; Android, Flutter and React Native SDKs are planned
- `deploy/`: local development stack and deployment configuration

## Status
Backend management, report ingestion, GitHub issue opening, and per-app Sentry event resolution are implemented. The automatic report-to-issue worker, Crashlytics export adapter, and Rust agent are still pending. See [crash-provider setup](backend/docs/crash-providers.md).
