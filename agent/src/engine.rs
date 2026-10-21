//! Source-independent on-demand query pipeline.

use std::time::Duration;

use chrono::Utc;
use tokio::time::{Instant, timeout_at};

use crate::{
    config::AgentConfig,
    model::{
        CoverageStatus, LogEntry, MatchKind, Query, QueryResult, QueryStatus, RawLogLine,
        ReadLimits, SCHEMA_VERSION, SourceError, SourceErrorKind, Workload, WorkloadDiagnostics,
        WorkloadResult, WorkloadStatus,
    },
    redaction::{MAX_INPUT_BYTES, redact_bounded},
    source::LogSource,
};

const MAX_WORKLOADS_PER_QUERY: usize = 1_000;

pub async fn execute<S: LogSource>(source: &S, query: &Query, config: &AgentConfig) -> QueryResult {
    if let Err(error) = query.validate(Utc::now()) {
        return failed_result(
            query,
            SourceError {
                kind: SourceErrorKind::InvalidResponse,
                detail: format!("invalid query: {error:?}"),
            },
        );
    }
    if let Err(error) = config.validate() {
        return failed_result(
            query,
            SourceError {
                kind: SourceErrorKind::InvalidResponse,
                detail: format!("invalid agent configuration: {error:?}"),
            },
        );
    }

    let started = Instant::now();
    let configured_deadline = started + Duration::from_millis(config.query_deadline_ms);
    let request_remaining = (query.deadline - Utc::now())
        .to_std()
        .unwrap_or(Duration::ZERO);
    let deadline = configured_deadline.min(started + request_remaining);

    let workloads = match timeout_at(deadline, source.list_workloads(&config.scope)).await {
        Ok(Ok(workloads)) => workloads,
        Ok(Err(error)) => return failed_result(query, error),
        Err(_) => {
            return failed_result(
                query,
                timeout_error("deadline expired during workload discovery"),
            );
        }
    };

    let mut result = QueryResult {
        schema_version: SCHEMA_VERSION,
        query_id: query.query_id.clone(),
        status: QueryStatus::Complete,
        requested_window: query.window,
        oldest_available: None,
        source_error: None,
        workloads: Vec::new(),
        truncated: workloads.len() > MAX_WORKLOADS_PER_QUERY,
        dropped_line_count: 0,
    };

    let selected_workloads: Vec<_> = workloads
        .into_iter()
        .take(MAX_WORKLOADS_PER_QUERY)
        .collect();
    let mut remaining_bytes = query.limits.max_bytes_total;
    let mut saw_usable_result = false;

    for workload in selected_workloads {
        if Instant::now() >= deadline {
            result.truncated = true;
            result.workloads.push(unavailable_workload(
                workload,
                timeout_error("overall query deadline expired"),
            ));
            result.status = QueryStatus::Partial;
            continue;
        }

        let workload_deadline =
            (Instant::now() + Duration::from_millis(config.workload_timeout_ms)).min(deadline);
        let raw_logs = match timeout_at(
            workload_deadline,
            source.read_logs(
                &workload,
                query.window,
                ReadLimits::from_query(&query.limits, query.trace_id.is_some()),
            ),
        )
        .await
        {
            Ok(Ok(raw_logs)) => raw_logs,
            Ok(Err(error)) => {
                result.workloads.push(unavailable_workload(workload, error));
                result.status = QueryStatus::Partial;
                continue;
            }
            Err(_) => {
                result.workloads.push(unavailable_workload(
                    workload,
                    timeout_error("workload log query timed out"),
                ));
                result.status = QueryStatus::Partial;
                continue;
            }
        };

        saw_usable_result = true;
        result.truncated |= raw_logs.truncated;
        if raw_logs.truncated
            || raw_logs.error.is_some()
            || raw_logs.discarded_line_count > 0
            || raw_logs.coverage != CoverageStatus::Complete
        {
            result.status = QueryStatus::Partial;
        }
        if let Some(oldest) = raw_logs.oldest_available
            && result
                .oldest_available
                .is_none_or(|previous| oldest < previous)
        {
            result.oldest_available = Some(oldest);
        }

        let filtered: Vec<RawLogLine> = raw_logs
            .lines
            .into_iter()
            .filter(|line| {
                line.timestamp.is_none_or(|timestamp| {
                    timestamp >= query.window.from && timestamp <= query.window.to
                })
            })
            .collect();

        // Match against raw text, but redact the complete fetched sequence
        // before selecting matches or context so block state is not sliced.
        let trace_matches: Vec<usize> = query
            .trace_id
            .as_ref()
            .map(|trace_id| {
                filtered
                    .iter()
                    .enumerate()
                    .filter_map(|(index, line)| line.message.contains(trace_id).then_some(index))
                    .collect()
            })
            .unwrap_or_default();
        let redacted = redact_bounded(
            filtered.iter().map(|line| line.message.clone()),
            usize::MAX,
            MAX_INPUT_BYTES,
        );
        result.dropped_line_count = result
            .dropped_line_count
            .saturating_add(redacted.dropped_line_count)
            .saturating_add(raw_logs.discarded_line_count);
        result.truncated |= redacted.truncated;
        if redacted.truncated {
            result.status = QueryStatus::Partial;
        }

        let mut safe_by_index: Vec<Option<String>> = vec![None; filtered.len()];
        for (index, line) in redacted.kept_indices.into_iter().zip(redacted.lines) {
            if let Some(slot) = safe_by_index.get_mut(index) {
                *slot = Some(line);
            }
        }

        let selected = select_lines(
            &filtered,
            &trace_matches,
            query.trace_id.is_some(),
            query.limits.context_lines,
        );
        let mut newest_first: Vec<(usize, MatchKind)> = selected.into_iter().collect();
        newest_first.sort_by(|(left, _), (right, _)| {
            filtered[*right]
                .timestamp
                .cmp(&filtered[*left].timestamp)
                .then_with(|| right.cmp(left))
        });

        let mut entries = Vec::new();
        for (index, match_kind) in newest_first {
            let Some(message) = safe_by_index.get(index).and_then(Option::as_ref) else {
                continue;
            };
            if entries.len() >= query.limits.max_lines_per_workload {
                result.truncated = true;
                result.status = QueryStatus::Partial;
                break;
            }
            let entry = LogEntry {
                timestamp: filtered[index].timestamp,
                stream: filtered[index].stream,
                message: message.clone(),
                match_kind,
            };
            let encoded_len = serde_json::to_vec(&entry).map_or(usize::MAX, |bytes| bytes.len());
            if encoded_len > remaining_bytes {
                result.truncated = true;
                result.status = QueryStatus::Partial;
                break;
            }
            remaining_bytes -= encoded_len;
            entries.push(entry);
        }
        entries.sort_by_key(|entry| entry.timestamp);
        result.workloads.push(WorkloadResult {
            workload,
            status: if raw_logs.coverage == CoverageStatus::Complete
                && !raw_logs.truncated
                && raw_logs.error.is_none()
                && raw_logs.discarded_line_count == 0
            {
                WorkloadStatus::Complete
            } else {
                WorkloadStatus::Partial
            },
            coverage: if raw_logs.error.is_some() || raw_logs.truncated {
                CoverageStatus::Partial
            } else {
                raw_logs.coverage
            },
            entries,
            diagnostics: raw_logs.diagnostics,
            error: raw_logs.error,
            dropped_line_count: redacted
                .dropped_line_count
                .saturating_add(raw_logs.discarded_line_count),
        });
    }

    if result.truncated {
        result.status = QueryStatus::Partial;
    }
    if !saw_usable_result && !result.workloads.is_empty() {
        result.status = QueryStatus::Failed;
    }
    fit_response_size(&mut result, query.limits.max_bytes_total);
    result
}

fn select_lines(
    lines: &[RawLogLine],
    trace_matches: &[usize],
    trace_requested: bool,
    context_lines: usize,
) -> std::collections::BTreeMap<usize, MatchKind> {
    let mut selected = std::collections::BTreeMap::new();
    if !trace_requested {
        for index in 0..lines.len() {
            selected.insert(index, MatchKind::Window);
        }
        return selected;
    }

    for &matched in trace_matches {
        selected.insert(matched, MatchKind::Trace);
        let start = matched.saturating_sub(context_lines);
        let end = matched
            .saturating_add(context_lines)
            .min(lines.len().saturating_sub(1));
        for index in start..=end {
            selected.entry(index).or_insert(MatchKind::Context);
        }
    }
    selected
}

fn fit_response_size(result: &mut QueryResult, max_bytes: usize) {
    loop {
        let encoded_size = serde_json::to_vec(result).map_or(usize::MAX, |bytes| bytes.len());
        if encoded_size <= max_bytes {
            return;
        }

        let oldest = result
            .workloads
            .iter()
            .enumerate()
            .filter_map(|(workload_index, workload)| {
                workload
                    .entries
                    .iter()
                    .enumerate()
                    .min_by_key(|(_, entry)| entry.timestamp)
                    .map(|(entry_index, entry)| (workload_index, entry_index, entry.timestamp))
            })
            .min_by_key(|(_, _, timestamp)| *timestamp);
        if let Some((workload_index, entry_index, _)) = oldest {
            let workload = &mut result.workloads[workload_index];
            workload.entries.remove(entry_index);
            workload.status = WorkloadStatus::Partial;
            workload.coverage = CoverageStatus::Partial;
            result.truncated = true;
            result.status = QueryStatus::Partial;
            continue;
        }

        result.workloads.clear();
        result.status = QueryStatus::Failed;
        result.truncated = true;
        result.source_error = Some(SourceError {
            kind: SourceErrorKind::InvalidResponse,
            detail: "result metadata exceeds the configured response limit".to_owned(),
        });
        return;
    }
}

fn unavailable_workload(workload: Workload, error: SourceError) -> WorkloadResult {
    WorkloadResult {
        workload,
        status: WorkloadStatus::Unavailable,
        coverage: CoverageStatus::Unknown,
        entries: Vec::new(),
        diagnostics: WorkloadDiagnostics::default(),
        error: Some(error),
        dropped_line_count: 0,
    }
}

fn timeout_error(detail: &str) -> SourceError {
    SourceError {
        kind: SourceErrorKind::Timeout,
        detail: detail.to_owned(),
    }
}

fn failed_result(query: &Query, error: SourceError) -> QueryResult {
    QueryResult {
        schema_version: SCHEMA_VERSION,
        query_id: query.query_id.clone(),
        status: QueryStatus::Failed,
        requested_window: query.window,
        oldest_available: None,
        source_error: Some(error),
        workloads: Vec::new(),
        truncated: false,
        dropped_line_count: 0,
    }
}

#[cfg(test)]
mod tests {
    use std::{collections::BTreeMap, time::Duration};

    use chrono::{TimeZone, Utc};

    use super::*;
    use crate::{
        config::AgentConfig,
        model::{LogStream, QueryLimits, RawLogLine, SourceErrorKind, TimeWindow},
        source::fake::FakeSource,
    };

    fn at(seconds: i64) -> chrono::DateTime<Utc> {
        Utc.timestamp_opt(seconds, 0)
            .single()
            .expect("valid test time")
    }

    fn query(trace_id: Option<&str>) -> Query {
        let now = Utc::now();
        Query {
            query_id: "query-test".to_owned(),
            window: TimeWindow {
                from: now - chrono::Duration::minutes(1),
                to: now + chrono::Duration::seconds(1),
            },
            trace_id: trace_id.map(str::to_owned),
            limits: QueryLimits {
                max_lines_per_workload: 100,
                max_bytes_total: 64 * 1024,
                context_lines: 1,
            },
            deadline: now + chrono::Duration::seconds(2),
        }
    }

    fn config() -> AgentConfig {
        AgentConfig {
            query_deadline_ms: 1_000,
            workload_timeout_ms: 500,
            ..AgentConfig::default()
        }
    }

    #[tokio::test]
    async fn matches_trace_and_adds_bounded_context_after_redaction() {
        let workload = FakeSource::workload("api");
        let source = FakeSource {
            workloads: vec![workload.clone()],
            logs: BTreeMap::from([(
                workload.id.clone(),
                vec![
                    FakeSource::line(at(100), "before"),
                    FakeSource::line(at(101), "trace=abc password=secret"),
                    FakeSource::line(at(102), "after"),
                    FakeSource::line(at(103), "outside context"),
                ],
            )]),
            coverage: CoverageStatus::Complete,
            ..FakeSource::default()
        };
        let mut request = query(Some("abc"));
        request.window = TimeWindow {
            from: at(99),
            to: at(104),
        };
        let result = execute(&source, &request, &config()).await;
        assert_eq!(result.status, QueryStatus::Complete);
        assert_eq!(result.workloads[0].entries.len(), 3);
        assert!(
            result.workloads[0].entries[1]
                .message
                .contains(MASK_PLACEHOLDER)
        );
        assert!(!result.workloads[0].entries[1].message.contains("secret"));
        assert_eq!(result.workloads[0].entries[1].match_kind, MatchKind::Trace);
    }

    #[tokio::test]
    async fn distinguishes_empty_success_from_source_error() {
        let empty_workload = FakeSource::workload("empty");
        let successful = FakeSource {
            workloads: vec![empty_workload],
            coverage: CoverageStatus::Complete,
            ..FakeSource::default()
        };
        let empty_result = execute(&successful, &query(None), &config()).await;
        assert_eq!(empty_result.status, QueryStatus::Complete);
        assert!(empty_result.source_error.is_none());
        assert!(empty_result.workloads[0].entries.is_empty());

        let failed = FakeSource {
            list_error: Some(SourceError {
                kind: SourceErrorKind::AccessDenied,
                detail: "denied".to_owned(),
            }),
            ..FakeSource::default()
        };
        let failed_result = execute(&failed, &query(None), &config()).await;
        assert_eq!(failed_result.status, QueryStatus::Failed);
        assert_eq!(
            failed_result.source_error.map(|error| error.kind),
            Some(SourceErrorKind::AccessDenied)
        );
    }

    #[tokio::test]
    async fn marks_per_workload_timeout_partial_when_another_workload_succeeds() {
        let slow = FakeSource::workload("slow");
        let fast = FakeSource::workload("fast");
        let source = FakeSource {
            workloads: vec![slow.clone(), fast.clone()],
            delays: BTreeMap::from([(slow.id.clone(), Duration::from_millis(30))]),
            coverage: CoverageStatus::Complete,
            ..FakeSource::default()
        };
        let mut limits = config();
        limits.workload_timeout_ms = 5;
        let result = execute(&source, &query(None), &limits).await;
        assert_eq!(result.status, QueryStatus::Partial);
        assert_eq!(result.workloads[0].status, WorkloadStatus::Unavailable);
        assert_eq!(
            result.workloads[0].error.as_ref().unwrap().kind,
            SourceErrorKind::Timeout
        );
        assert_eq!(result.workloads[1].status, WorkloadStatus::Complete);
    }

    #[tokio::test]
    async fn drops_a_key_body_even_when_the_match_window_starts_inside_it() {
        let workload = FakeSource::workload("api");
        let source = FakeSource {
            workloads: vec![workload.clone()],
            logs: BTreeMap::from([(
                workload.id.clone(),
                vec![RawLogLine {
                    timestamp: Some(Utc::now()),
                    stream: LogStream::Stdout,
                    message: "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefgh".to_owned(),
                }],
            )]),
            coverage: CoverageStatus::Complete,
            ..FakeSource::default()
        };
        let result = execute(&source, &query(None), &config()).await;
        assert_eq!(result.dropped_line_count, 1);
        assert!(result.workloads[0].entries.is_empty());
    }

    #[tokio::test]
    async fn keeps_recent_lines_when_the_line_limit_is_reached() {
        let workload = FakeSource::workload("api");
        let source = FakeSource {
            workloads: vec![workload.clone()],
            logs: BTreeMap::from([(
                workload.id.clone(),
                (100..105)
                    .map(|second| FakeSource::line(at(second), &format!("line-{second}")))
                    .collect(),
            )]),
            coverage: CoverageStatus::Complete,
            ..FakeSource::default()
        };
        let mut request = query(None);
        request.window = TimeWindow {
            from: at(99),
            to: at(106),
        };
        request.limits.max_lines_per_workload = 2;
        let result = execute(&source, &request, &config()).await;
        let entries = &result.workloads[0].entries;
        assert_eq!(entries.len(), 2);
        assert_eq!(entries[0].message, "line-103");
        assert_eq!(entries[1].message, "line-104");
        assert!(result.truncated);
    }

    #[tokio::test]
    async fn keeps_newest_lines_when_the_read_cap_truncates_a_workload() {
        let workload = FakeSource::workload("api");
        let source = FakeSource {
            workloads: vec![workload.clone()],
            logs: BTreeMap::from([(
                workload.id.clone(),
                (100..110)
                    .map(|second| FakeSource::line(at(second), &format!("line-{second}")))
                    .collect(),
            )]),
            coverage: CoverageStatus::Complete,
            ..FakeSource::default()
        };
        let mut request = query(None);
        request.window = TimeWindow {
            from: at(99),
            to: at(111),
        };
        request.limits.max_lines_per_workload = 3;
        let result = execute(&source, &request, &config()).await;
        let entries = &result.workloads[0].entries;
        assert_eq!(
            entries.last().map(|entry| entry.message.as_str()),
            Some("line-109")
        );
        assert!(entries.iter().all(|entry| entry.message != "line-100"));
        assert_eq!(result.status, QueryStatus::Partial);
        assert!(result.truncated);
    }

    #[tokio::test]
    async fn serialized_result_stays_within_the_total_byte_limit() {
        let workload = FakeSource::workload("api");
        let source = FakeSource {
            workloads: vec![workload.clone()],
            logs: BTreeMap::from([(
                workload.id.clone(),
                (100..110)
                    .map(|second| FakeSource::line(at(second), &"payload ".repeat(31)))
                    .collect(),
            )]),
            coverage: CoverageStatus::Complete,
            ..FakeSource::default()
        };
        let mut request = query(None);
        request.window = TimeWindow {
            from: at(99),
            to: at(111),
        };
        request.limits.max_bytes_total = 1_024;
        let result = execute(&source, &request, &config()).await;
        let encoded = serde_json::to_vec(&result).expect("result serializes");
        assert!(encoded.len() <= request.limits.max_bytes_total);
        assert!(result.truncated);
    }

    const MASK_PLACEHOLDER: &str = "[REDACTED]";
}
