# spec

Contracts shared by every component (report schema, ingestion API, SDK conformance cases). No component owns this folder; changes need review from everyone who consumes it.

| File | What |
|---|---|
| [`ingestion.md`](ingestion.md) | How an SDK sends a report and how the backend answers: endpoint, authentication, processing order, errors, limits, versioning |
| [`agent-protocol.md`](agent-protocol.md) | How agents, the coordinator and the backend exchange server-side logs: streams, registration, queries, aggregate, errors |
| [`proto/`](proto/) | The protocol messages (proto3) |
| [`report.schema.json`](report.schema.json) | The report format, version 1 (JSON Schema 2020-12) |
| [`examples/`](examples/) | Example reports; files named `invalid-*` must be rejected |
