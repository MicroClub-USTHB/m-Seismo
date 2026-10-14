# Backend

Go service for ingestion, enrichment, deduplication, and GitHub publishing.

## Commands

Run these commands from the repository root:

- `make backend-fmt`: format Go source files with `gofmt`.
- `make backend-fmt-check`: verify that Go source files are formatted.
- `make backend-lint`: run `golangci-lint` with the repository's configured linters, including `gosec` and `bodyclose`.
- `make backend-build`: build the Go backend.
- `make backend-test`: run Go tests.
- `make backend-check`: run the backend formatting check and linting, build, and tests.

## Status

Skeleton.
