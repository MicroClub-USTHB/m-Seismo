# Agent

Rust agent that runs beside services, collecting and redacting logs and diagnostics on demand with outbound connections only.

## Commands

Run these commands from the repository root:

- `make agent-fmt`: format Rust source files with `cargo fmt`.
- `make agent-fmt-check`: verify that Rust source files are formatted.
- `make agent-lint`: run `cargo clippy` on all targets and treat warnings as errors.
- `make agent-build`: build the Rust agent using the committed lockfile.
- `make agent-test`: run Rust tests using the committed lockfile.
- `make agent-check`: run the agent formatting check and linting, build, and tests.

## Layout

- `crates/proto`: generated `seismo.v1` types from `spec/proto`.
- `crates/agent`: the agent. Reads logs from one pluggable source (`[source] type = ...`) and answers the coordinator.
- `crates/coordinator`: the coordinator. Agents dial in, it dials the backend and fans queries out.

The build needs `protoc`.

## Running the coordinator

One per deployment. Agents register with it, it attaches to the backend and
turns each `QueryLogs` into a query to every connected agent, then answers
with one aggregate: a section per agent with its status (`OK`, `TRUNCATED`,
`TIMEOUT`, `DISCONNECTED`, `ERROR`) and a warning for services claimed by
more than one agent.

```sh
SEISMO_AGENT_TOKEN=... SEISMO_COORDINATOR_KEY=mco_... \
  cargo run -p seismo-coordinator -- coordinator.toml
```

```toml
# coordinator_id = "prod"           # sent in Hello, defaults to the hostname
# listen = "[::]:50051"             # where agents dial in

[agents]
# token_env = "SEISMO_AGENT_TOKEN"  # env var with the registration token
# [agents.tls]                     # serve TLS, plaintext when absent
# cert_file = "/etc/seismo/tls.crt"  # PEM, full chain
# key_file = "/etc/seismo/tls.key"

[backend]
url = "http://backend:9090"
# key_env = "SEISMO_COORDINATOR_KEY" # env var with the mco_ key

# what the coordinator asks of every agent, QueryLogs doesn't carry it
[query]
# max_lines_per_workload = 1000
# context_lines = 5
# total_budget_bytes = 8388608      # split evenly between connected agents
# margin_ms = 500                   # kept back from the backend deadline
```

The agent's `[coordinator] token` must match `agents.token_env`. Both sides
reconnect with backoff.

## Running the agent

```sh
SEISMO_AGENT_TOKEN=... cargo run -p seismo-agent -- agent.toml
```

```toml
agent_id = "db"                     # optional, defaults to the hostname
services = ["postgres"]             # fetched one by one from the source

[coordinator]
url = "http://coordinator:50051"
# token_env = "SEISMO_AGENT_TOKEN"  # env var holding the registration token
# ca_file = "/etc/seismo/ca.pem"    # extra CA to trust, for a private cert (use an https:// url)

[source]
type = "loki"
url = "http://loki:3100"
# service_label = "service_name"    # label matched against `services`
# scope_label = "namespace"         # copied into Workload.scope_label
# tenant = "acme"                   # X-Scope-OrgID
# trace_field = "trace_id"          # structured metadata, "" = plain line filter

# [source.auth]
# type = "bearer"                   # or "basic" (username, password_env)
# token_env = "LOKI_TOKEN"
```

Sources return raw lines, the engine redacts every line once before it leaves the agent. Plaintext gRPC
on the agent hop, TLS is opt-in (see `[agents.tls]`).

## Log sources

Pick one per agent with `[source] type = ...`.

With a `trace_id` a source also returns `context_lines` lines on each side of
every match, marked as context. Context only comes from inside the window, and
`limit` counts matches, not context. `loki` has no "lines around this one", so
it makes two extra queries per service (everything up to the newest match, and
a few lines after it), capped at 5000 lines by Loki's own limit.

### `loki`

See the example above. One `query_range` per service.

### `file`

Plain log files, one per service. Reads from the end of the file, so a small
window on a big log stays cheap, and follows rotated copies (`app.log.1`,
`app.log.2`, uncompressed only) when the window reaches back that far.

```toml
[source]
type = "file"
rotated = 3                         # how many app.log.N to look into

[source.files]
postgres = "/var/log/postgresql/postgresql.log"
```

A line needs a timestamp the agent can read: RFC3339, `YYYY-MM-DD HH:MM:SS[,fff]`
(taken as UTC), or JSON with `time`, `timestamp`, `ts`, `@timestamp` or `t`
(string or epoch number). Lines without one (stack traces) stick to the line
above. The `trace_id` filter is a plain substring match. A fetch reads at most
64 MiB, past that it reports the result as truncated.

### `journal`

The systemd journal, through `journalctl -o json`. Each service maps to a unit
(default `<service>.service`). It stops reading as soon as it has enough
lines, so a big journal is fine.

```toml
[source]
type = "journal"
# command = ["sudo", "journalctl"]  # default ["journalctl"]

[source.units]
postgres = "postgresql@16-main.service"
```

The agent's user needs to be in `systemd-journal` (or use `command`) to see
other units' logs. The `trace_id` filter is a plain substring match.

### `docker`

Container logs, through `docker logs --timestamps`. Each service maps to a
container name or id (default: the service name). stdout and stderr are
interleaved by time and keep their stream. Needs a logging driver that
supports reading (`json-file`, `local`, `journald`).

```toml
[source]
type = "docker"
# command = ["sudo", "docker"]      # default ["docker"]

[source.containers]
postgres = "myproj-postgres-1"
```

The agent's user needs access to the Docker socket. The `trace_id` filter is
a plain substring match.

### `kubernetes`

Pod logs, through `kubectl logs --timestamps`. Each service maps to a
`namespace/pod/container`. Every service needs an entry, there is no default.

```toml
[source]
type = "kubernetes"
# command = ["sudo", "kubectl"]     # default ["kubectl"]

[source.pods]
postgres = "shop/postgres-0/postgres"
```

Uses whatever `kubectl` finds in the environment (`KUBECONFIG`, or the pod's
service account in-cluster). The agent's identity needs `get` on pods and
`get` on `pods/log` in each namespace. The `trace_id` filter is a plain
substring match.

## Container status

A `StatusQuery` asks for each service's container state, with no logs:
runtime state, restart count, OOM kill, image and configured CPU and memory
limits. `docker` reads `docker inspect`, `kubernetes` reads `kubectl get pod`.
The logs-only sources answer every service `UNAVAILABLE` with
`DRIVER_UNSUPPORTED`. The coordinator fans it out like a log query, and the
backend asks for it with `QueryStatus`.

## Redaction

Every log line is scrubbed before it leaves the agent (`crates/agent/src/redaction.rs`):
bearer/basic tokens, JWTs, `password=`/`token:`/`api_key`-style fields,
credentials in a `scheme://user:pass@host` URL, AWS access key IDs, email
addresses, phone numbers (`+1415...`, or a 3-3-4 group like
`415-555-0132`), and card numbers (13-19 digits, grouped or not) that pass
the Luhn check — a same-length id or trace number that isn't a real card
number is left alone. IP addresses are deliberately not scrubbed: too many
false positives in debugging logs.

## Resource budget

Idle (registered with a coordinator, zero log/metrics activity) the agent
measured 8.3 MB RSS and effectively 0% CPU over a minute, on an Apple M1
Pro (`cargo build --release`, single `file` source, one service). Both are
far under the 50 MB / 2% CPU budget (FR: agent redaction and resource
budget).

Reproduce with:

```sh
cargo build --release -p seismo-agent
cat > /tmp/idle-agent.toml <<'EOF'
agent_id = "resource-budget-probe"
services = ["idle"]

[coordinator]
url = "http://127.0.0.1:1"   # unreachable on purpose: steady reconnect-with-backoff, not a live session
token_env = "SEISMO_AGENT_TOKEN"

[source]
type = "file"

[source.files]
idle = "/tmp/idle-service.log"
EOF
touch /tmp/idle-service.log
SEISMO_AGENT_TOKEN=dummy SEISMO_AGENT_CONFIG=/tmp/idle-agent.toml \
  RUST_LOG=warn ./target/release/seismo-agent &
sleep 60 && ps -p $! -o rss,%cpu,etime,time
```

## Status

Agent: sources `loki`, `file`, `journal`, `docker`, `kubernetes`, and container
status for `docker` and `kubernetes`. Coordinator: registry, fan-out and
backend attach. TLS on the agent hop is opt-in, the backend hop is plaintext.
No Tempo or Mimir.
