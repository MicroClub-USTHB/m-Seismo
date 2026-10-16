use std::iter::Peekable;

use chrono::{DateTime, FixedOffset, Utc};
use serde::Serialize;

use crate::query::{QueryRequest, Source};

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

    if !dry_run {
        return Err(
            "log sources are not connected yet; use --dry-run to preview the validated query"
                .to_owned(),
        );
    }

    let preview = QueryPreview {
        status: "validated",
        message: "dry run only; no logs were queried",
        request: &request,
    };
    let output = serde_json::to_string_pretty(&preview)
        .map_err(|error| format!("could not serialize query preview: {error}"))?;
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
        "m-seismo-agent: local server-log query agent\n\nCommands:\n  query   Validate and preview a log query (source retrieval is added in later milestones)\n  help    Show this help"
    );
    print_query_help();
}

fn print_query_help() {
    println!(
        "\nUsage:\n  m-seismo-agent query --source <docker|kubernetes> --from <RFC3339> --to <RFC3339> --dry-run [options]\n\nOptions:\n  --trace-id <ID>       Match this trace ID when the query is executed\n  --workload <NAME>     Limit to a workload; repeat to include multiple workloads\n  --namespace <NAME>    Limit Kubernetes queries; repeat to include multiple namespaces\n  --dry-run             Validate and print the normalized request without reading logs\n  -h, --help            Show query help"
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
