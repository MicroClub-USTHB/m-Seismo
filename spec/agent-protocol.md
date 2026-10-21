# Agent protocol

How the coordinator gets backend logs from agents, and how the backend
asks the coordinator for them.

- **Messages:** [`proto/seismo/v1/`](proto/seismo/v1/) (proto3).
- **Plan and rationale:** `agent/PLAN.md`.

Status: v1 draft. Changes need review from the agent, coordinator and
backend owners.

## 1. Topology

```
agent ──dials──▶ coordinator ──dials──▶ backend
```

Everything dials out. Agents register with the coordinator, the
coordinator attaches to the backend. The backend never learns about
individual agents, and agents can never issue queries.

| Hop | Service | Defined in | Server |
|---|---|---|---|
| agent → coordinator | `Agent.Connect` | `agent.proto` | coordinator |
| coordinator → backend | `Coordinator.Attach` | `coordinator.proto` | backend |

Both are bidirectional streams, so the dialing side can receive requests
without opening a port. Shared types live in `common.proto`.

## 2. Authentication

Both hops use TLS (rustls on the Rust side).

| Hop | Credential | Where |
|---|---|---|
| agent → coordinator | registration token, one per deployment | `authorization: Bearer …` metadata |
| coordinator → backend | `mco_…` key, one per app | `authorization: Bearer …` metadata |

- The backend derives the app from the `mco_` key. Nothing in a message
  can name an app.
- `msk_` SDK keys are unrelated and never accepted here.
- A bad credential fails the stream with `UNAUTHENTICATED` before any
  message is read.

## 3. Agent hop

### 3.1 Register

The agent's first message is `Register`:

| Field | Notes |
|---|---|
| `protocol_version` | Integer, currently `1` |
| `agent_id` | Stable across restarts and reconnects. From config, falls back to the hostname |
| `source` | `docker`, `kubernetes` or `loki` |
| `services` | Service names the agent can see. A routing hint, v1 queries every agent |

There is no ack. Rejections are a gRPC status on the stream:

| Status | Cause |
|---|---|
| `FAILED_PRECONDITION` | `protocol_version` not supported |
| `ALREADY_EXISTS` | Another live stream already uses `agent_id` |
| `UNAUTHENTICATED` | Bad token |

A reconnect after the old stream closed replaces its entry. If the old
stream is dead but not yet noticed, the reconnect gets `ALREADY_EXISTS`
and retries with backoff until keepalive drops the stale entry.

### 3.2 Queries

- The coordinator sends `Query` with a coordinator-generated `query_id`.
  Several queries can be in flight on one stream.
- Every reply echoes `query_id`: one `QueryResult`, or one `QueryError`
  if no result could be produced at all (bad query, internal failure).
  Per-workload problems belong in the `QueryResult`.
- `Cancel{query_id}` is best effort. The agent drops the work and sends
  nothing back. A reply that arrives for a cancelled or unknown
  `query_id` is ignored.
- The result is a single message, not chunked.

### 3.3 Budget

The coordinator splits the byte budget evenly across connected agents and
puts the share in `Query.limits.max_bytes_total`. The agent uses the lower
of that and its own configured ceiling, and sets `truncated` when it cut
anything. Lines are cut oldest first: the newest ones survive.

Limits the agent enforces (see `agent/src/model.rs`):

| Limit | Value |
|---|---|
| `max_lines_per_workload` | 1 to 10 000 |
| `max_bytes_total` | 1 KiB to 8 MiB |
| `context_lines` | up to 100 |

The agent rejects a `Query` outside these with a `QueryError`.
XXX: no `SourceErrorKind` fits "bad request" yet, add `INVALID_QUERY`
when the agent service mode lands.

### 3.4 Liveness

HTTP/2 keepalive pings on the connection. A closed stream drops the agent
from the registry, which lives in memory only. Queries in flight to a
dropped agent end as `DISCONNECTED`. The agent reconnects with backoff
and registers again.

## 4. Backend hop

### 4.1 Attach

The coordinator opens `Attach` with the `mco_` key in metadata and sends
`Hello{protocol_version, coordinator_id}` first. Rejections:
`UNAUTHENTICATED` for a bad key, `FAILED_PRECONDITION` for an unsupported
version.

One live stream per app. A new `Attach` for the same app replaces the old
one.

### 4.2 Queries

- The backend sends `QueryLogs{query_id, window, trace_id?, deadline}`
  when the pipeline needs backend logs.
- The coordinator fans out to every connected agent under that deadline
  (minus a margin to aggregate) and replies with **one**
  `AggregateResult`, or an `AggregateError` for a coordinator fault.
- No early partials in v1. A slow agent is covered by the per-agent
  status.
- No agent connected is not an error: it's an `AggregateResult` with
  zero sections.

## 5. Aggregate

One `AgentSection` per agent, no merging across agents. Each section has
an `AgentStatus`:

| Status | Meaning | `result` |
|---|---|---|
| `OK` | Answered in time | set |
| `TRUNCATED` | Answered, but cut logs | set, `truncated = true` |
| `TIMEOUT` | No answer before the deadline | unset |
| `DISCONNECTED` | Stream closed before the answer | unset |
| `ERROR` | Agent sent a `QueryError` | unset, `detail` has the reason |

Inside a `QueryResult`, a workload whose logs are gone has status
`UNAVAILABLE` and an `error` with a `SourceErrorKind` (the contract) and
free-text `detail` (for humans, never parsed). Receivers treat unknown
enum values as `UNSPECIFIED`.

`warnings` is a top-level list. v1 has one code, `DUPLICATE_SERVICE`: the
same service is claimed by two or more agents. Both sections are returned
as is, nothing is deduped.

## 6. Limits

| Limit | Value |
|---|---|
| Log budget for the whole aggregate | 8 MiB |
| gRPC message cap (both hops) | 10 MiB (budget + overhead) |

Redaction happens in the agent only. Raw logs never leave the host.

## 7. Versioning

- `protocol_version` in `Register` and `Hello` is a plain integer. An
  unsupported version is rejected, the coordinator does not try to
  degrade.
- The proto package (`seismo.v1`) only changes with a breaking wire
  change.
- Adding a field or an enum value does not change the version. Removing
  or retyping a field, or changing an enum's meaning, does.

## 8. Open points

1. **Services routing.** `Register.services` is carried but unused;
   narrowing by service needs no protocol change.
2. **Tempo and Mimir.** `QueryResult` is logs only. More signal kinds
   are added as new fields beside `workloads`.
