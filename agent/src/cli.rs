use std::{collections::BTreeMap, iter::Peekable};

use chrono::{DateTime, Duration as ChronoDuration, FixedOffset, Utc};
use serde::Serialize;

use crate::{
    config::{AgentConfig, WorkloadScope, WorkloadSelectors},
    engine,
    model::{Query, QueryResult, TimeWindow},
    query::{QueryRequest, Source},
    source::docker::DockerSource,
};

#[derive(Serialize)]
struct QueryPreview<'a> {
    status: &'static str,
    message: &'static str,
    request: &'a QueryRequest,
}

pub fn run(args: impl Iterator<Item = String>) -> Result<(), String> {
    let mut args = args.peekable();
    let Some(command) = args.next() else {
        print_help();
        return Err("a command is required".to_owned());
    };

    match command.as_str() {
        "help" | "--help" | "-h" => {
            print_help();
            Ok(())
        }
        "query" => run_query(args),
        _ => {
            print_help();
            Err(format!("unknown command '{command}'"))
        }
    }
}

fn run_query(mut args: Peekable<impl Iterator<Item = String>>) -> Result<(), String> {
    let mut source = None;
    let mut from = None;
    let mut to = None;
    let mut trace_id = None;
    let mut workload_filters = Vec::new();
    let mut namespaces = Vec::new();
    let mut labels = BTreeMap::new();
    let mut projects = Vec::new();
    let mut docker_endpoint = None;
    let mut dry_run = false;

    while let Some(option) = args.next() {
        match option.as_str() {
            "--source" => source = Some(Source::parse(&required_value(&mut args, &option)?)?),
            "--from" => {
                from = Some(parse_timestamp(
                    &required_value(&mut args, &option)?,
                    &option,
                )?)
            }
            "--to" => {
                to = Some(parse_timestamp(
                    &required_value(&mut args, &option)?,
                    &option,
                )?)
            }
            "--trace-id" => trace_id = Some(required_value(&mut args, &option)?),
            "--workload" => workload_filters.push(required_value(&mut args, &option)?),
            "--namespace" => namespaces.push(required_value(&mut args, &option)?),
            "--label" => {
                let value = required_value(&mut args, &option)?;
                let (key, label_value) = value
                    .split_once('=')
                    .ok_or_else(|| "--label expects KEY=VALUE".to_owned())?;
                if labels
                    .insert(key.to_owned(), label_value.to_owned())
                    .is_some()
                {
                    return Err(format!("duplicate label selector '{key}'"));
                }
            }
            "--project" => projects.push(required_value(&mut args, &option)?),
            "--docker-endpoint" => docker_endpoint = Some(required_value(&mut args, &option)?),
            "--dry-run" => dry_run = true,
            "--help" | "-h" => {
                print_query_help();
                return Ok(());
            }
            _ => return Err(format!("unknown query option '{option}'")),
        }
    }

    let request = QueryRequest {
        source: source.ok_or_else(|| "missing required option --source".to_owned())?,
        from: from.ok_or_else(|| "missing required option --from".to_owned())?,
        to: to.ok_or_else(|| "missing required option --to".to_owned())?,
        trace_id,
        workload_filters,
        namespaces,
    };
    request.validate()?;
    if request.to > Utc::now() {
        return Err("--to must not be in the future".to_owned());
    }

    let config = AgentConfig {
        endpoint: docker_endpoint,
        scope: if request.workload_filters.is_empty() && labels.is_empty() && projects.is_empty() {
            WorkloadScope::All
        } else {
            WorkloadScope::Selectors(WorkloadSelectors {
                names: request.workload_filters.clone(),
                labels,
                projects,
            })
        },
        ..AgentConfig::default()
    };
    config
        .validate()
        .map_err(|error| format!("invalid configuration: {error:?}"))?;

    if dry_run {
        let preview = QueryPreview {
            status: "validated",
            message: "dry run only; no logs were queried",
            request: &request,
        };
        let output = serde_json::to_string_pretty(&preview)
            .map_err(|error| format!("could not serialize query preview: {error}"))?;
        println!("{output}");
        return Ok(());
    }

    if request.source != Source::Docker {
        return Err("Kubernetes log retrieval is planned for a later milestone; Docker is currently supported".to_owned());
    }

    let source = DockerSource::connect(&config).map_err(|error| {
        format!(
            "could not configure Docker source ({:?}): {}",
            error.kind, error.detail
        )
    })?;
    let now = Utc::now();
    let deadline = now
        .checked_add_signed(ChronoDuration::milliseconds(
            config.query_deadline_ms as i64,
        ))
        .ok_or_else(|| "configured query deadline is out of range".to_owned())?;
    let query = Query {
        query_id: format!("local-{}", now.timestamp_nanos_opt().unwrap_or_default()),
        window: TimeWindow {
            from: request.from,
            to: request.to,
        },
        trace_id: request.trace_id,
        limits: config.limits,
        deadline,
    };
    let runtime = tokio::runtime::Builder::new_multi_thread()
        .enable_all()
        .build()
        .map_err(|error| format!("could not start async runtime: {error}"))?;
    let result: QueryResult = runtime.block_on(engine::execute(&source, &query, &config));
    let output = serde_json::to_string_pretty(&result)
        .map_err(|error| format!("could not serialize query result: {error}"))?;
    println!("{output}");
    Ok(())
}

fn required_value(
    args: &mut Peekable<impl Iterator<Item = String>>,
    option: &str,
) -> Result<String, String> {
    match args.next() {
        Some(value) if !value.starts_with("--") => Ok(value),
        _ => Err(format!("{option} requires a value")),
    }
}

fn parse_timestamp(value: &str, option: &str) -> Result<DateTime<Utc>, String> {
    let timestamp = DateTime::<FixedOffset>::parse_from_rfc3339(value).map_err(|_| {
        format!("{option} must be an RFC 3339 timestamp, for example 2026-10-16T10:00:00Z")
    })?;
    Ok(timestamp.with_timezone(&Utc))
}

fn print_help() {
    println!(
        "m-seismo-agent: local on-demand server-log query agent\n\nCommands:\n  query   Query logs from a supported source\n  help    Show this help"
    );
    print_query_help();
}

fn print_query_help() {
    println!(
        "\nUsage:\n  m-seismo-agent query --source <docker|kubernetes> --from <RFC3339> --to <RFC3339> [options]\n\nOptions:\n  --trace-id <ID>       Match this trace ID and nearby context\n  --workload <NAME>     Limit Docker queries; repeat to include multiple workloads\n  --label KEY=VALUE     Require a Docker label; repeat for multiple labels\n  --project <NAME>      Limit to a Docker Compose project\n  --docker-endpoint <PATH|unix://PATH>  Docker Engine socket (default: local socket)\n  --namespace <NAME>    Limit Kubernetes queries (not yet implemented)\n  --dry-run             Validate and print the request without reading logs\n  -h, --help            Show query help"
    );
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parses_rfc3339_offsets_and_normalizes_to_utc() {
        let timestamp = parse_timestamp("2026-10-16T12:00:00+02:00", "--from").unwrap();
        assert_eq!(timestamp.to_rfc3339(), "2026-10-16T10:00:00+00:00");
    }

    #[test]
    fn rejects_non_rfc3339_timestamps() {
        assert!(parse_timestamp("yesterday", "--from").is_err());
    }
}
