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
- `crates/coordinator`: not implemented yet.

The build needs `protoc`.

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
for now, TLS is not done yet.

## Log sources

Pick one per agent with `[source] type = ...`.

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

## Status

Agent done, sources: `loki`, `file`, `journal`, `docker`. No Tempo, Mimir or
`context_lines` yet. Coordinator is a stub.
