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

Log lines are redacted in the agent before they are sent. Plaintext gRPC
for now, TLS is not done yet.

## Status

Agent done for Loki logs. No Tempo, Mimir or `context_lines` yet.
Coordinator is a stub.
