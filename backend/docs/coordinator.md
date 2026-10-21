# Coordinator

The backend side of the [agent protocol](../../spec/agent-protocol.md): the
backend serves `Coordinator.Attach` over gRPC, each app's log coordinator
dials in, and the worker asks it for the server logs around a report. The
code lives in `internal/coordinator`; the Go types are generated from
`spec/proto` into `internal/pb/seismo/v1`.

```
agents ──▶ coordinator ──Attach (gRPC, mco_ key)──▶ backend instance A
                                                         ▲
worker on instance B ──NATS request seismo.coordinator.query.<app>──┘
```

## Attaching

1. Create a coordinator key for the app:
   `POST /api/v1/apps/{appID}/coordinator-keys` → `{"secret": "mco_…"}`
   (shown once, see [api.md](api.md#coordinator-keys)).
2. Give it to the coordinator, which dials the backend's gRPC address with
   `authorization: Bearer mco_…` and sends `Hello{protocol_version: 1}`.

The backend answers a bad or revoked key with `UNAUTHENTICATED`, a first
event other than `Hello` with `INVALID_ARGUMENT`, and another protocol
version with `FAILED_PRECONDITION`.

There is **one live stream per app**. A coordinator that attaches again,
on any instance, replaces the older stream, which ends with `ABORTED`. When
an instance shuts down, its streams end with `UNAVAILABLE` and the
coordinators attach again, to another instance behind the load balancer.

## Several backend instances

The coordinator's stream is held by one instance, but the worker that
needs logs runs on any instance. They meet on NATS (core NATS, not
JetStream):

| Subject | Who | What |
|---|---|---|
| `seismo.coordinator.query.<app id>` | the instance holding the app's stream subscribes; any worker sends requests | a `QueryLogs` request, answered with a `CoordinatorEvent` (`AggregateResult` or `AggregateError`) |
| `seismo.coordinator.attached.<app id>` | every instance | "a coordinator attached for this app": an instance with an older stream for it closes that stream |

No responder on the query subject means no coordinator is attached for the
app: the worker gets `coordinator.ErrNotAttached` immediately, not after a
timeout.

The answer comes back in one NATS message, so its size is capped by the
NATS max payload (1 MiB by default). The worker's budget
(`QueryLogs.limits.max_bytes_total`) is the lower of
`SEISMO_COORDINATOR_MAX_BYTES` and that payload less 64 KiB.

## What the worker does with it

For each report, before opening its issues, the worker asks for the logs in
`[occurred_at - window, occurred_at + window]` and waits at most
`SEISMO_COORDINATOR_TIMEOUT`. The issue gets a **Server logs** section:
per agent its status, per service its health (state, restarts, OOM, image)
and its last 40 lines, folded in `<details>`, about 30 KB at most in all.

Server logs never hold up an issue: with no coordinator attached, a timeout
or any error, the issue is opened without them. They are not asked again
when every repository already has its issue.

## Configuration

| Variable | Default | Meaning |
|---|---|---|
| `SEISMO_COORDINATOR_ENABLED` | `false` | Serve `Coordinator.Attach` in this process. Workers ask for logs either way. |
| `SEISMO_COORDINATOR_ADDR` | `:9443` | gRPC listen address |
| `SEISMO_COORDINATOR_TLS_CERT_FILE`, `SEISMO_COORDINATOR_TLS_KEY_FILE` | | Server certificate. Required outside development, unless `PLAINTEXT` is set. |
| `SEISMO_COORDINATOR_PLAINTEXT` | `false` | Serve plain gRPC because TLS ends in front of the backend (load balancer, ingress) |
| `SEISMO_COORDINATOR_WINDOW` | `2m` | Logs asked before and after the report |
| `SEISMO_COORDINATOR_TIMEOUT` | `8s` | How long the worker waits for the answer |
| `SEISMO_COORDINATOR_MAX_BYTES` | `262144` | Log budget of one query, whole aggregate |
| `SEISMO_COORDINATOR_MAX_LINES_PER_WORKLOAD` | `200` | Lines per service |

The gRPC server pings idle connections every 30 s so a dead coordinator
is noticed, and accepts messages up to 10 MiB.

## Trying it locally

The dev stack serves plain gRPC on `localhost:9443`. `cmd/fakecoordinator`
attaches with a key and answers every query with made-up logs from two
services:

```sh
FAKECOORDINATOR_BACKEND=127.0.0.1:9443 FAKECOORDINATOR_KEY=mco_… \
  go run ./cmd/fakecoordinator
```

Then send a report: its issue on the fake GitHub has the Server logs
section.

## Regenerating the Go types

After changing `spec/proto`:

```sh
make proto-generate   # needs protoc, protoc-gen-go and protoc-gen-go-grpc
```

## Not done yet

- **A revoked key doesn't end a live stream.** It is refused at the next
  attach; until then the coordinator keeps answering.
- **`trace_id` is never sent.** The report has no trace ID yet; queries are
  by time window only.
- **Logs are only in the issue body.** Full logs as a private attachment
  come with attachments.

## Tests

`go test ./internal/coordinator/` runs a real gRPC server in memory with a
fake coordinator and needs NATS (`SEISMO_TEST_NATS_URL`, skipped
otherwise): query relayed and answered, no coordinator attached, key and
`Hello` rejections, a newer attach replacing the older one on the same and
on another instance, a silent coordinator timing out, the budget fitting in
one NATS message. The worker's tests cover the issue's Server logs section
and that failures never hold up the issue.
