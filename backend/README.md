# backend

Go service: ingestion, enrichment, deduplication, GitHub publishing.
Status: ingestion only (`POST /v1/reports`). Contract: [`spec/API.md`](../spec/API.md).

## Run

    DATABASE_URL=postgres://user:pass@host/db go run .

- `DATABASE_URL` (required): Postgres. The schema is applied on start.
- `LISTEN_ADDR` (default `:8080`).

There is no admin tool yet. Add a project key by hand. Store the sha256 of the key:

    INSERT INTO projects (id, name) VALUES ('<uuid>', 'demo');
    INSERT INTO project_keys (key_hash, project_id)
    VALUES (decode('<sha256 hex of key>', 'hex'), '<uuid>');

## Test

    cd backend
    # set TEST_DATABASE_URL to postgres://user:pass@host/scratch, then:
    go test ./...

DB tests are skipped without `TEST_DATABASE_URL`. They truncate the tables, so use a scratch database.
