# m-Seismo

m-Seismo is an open-source internal tool from MicroClub that turns mobile bug reports into complete GitHub issues. Reports from our mobile apps usually arrive incomplete, so m-Seismo captures the context at the moment of the bug (device state, logs, user actions, screenshot, server logs, crash data) and files the issue automatically.

## Components
- `spec/`: contracts shared by every component (report schema, ingestion API, SDK conformance cases)
- `backend/`: Go service for ingestion, enrichment, deduplication and GitHub publishing
- `agent/`: Rust agent that collects and redacts server logs and diagnostics on demand
- `sdks/`: mobile SDKs (iOS and Android first, Flutter and React Native later)
- `deploy/`: local development stack and deployment configuration

## Status
Early setup. Nothing runs yet.

## Make commands

Run these commands from the repository root.

- `make fmt`: format the Go backend and Rust agent.
- `make lint`: run the Go and Rust linters.
- `make check`: check formatting and linting, then build and test the Go backend and Rust agent.
