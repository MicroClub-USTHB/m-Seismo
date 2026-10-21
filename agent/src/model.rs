//! Source-neutral request and response types shared by log adapters.

use std::collections::BTreeMap;

use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};

pub const SCHEMA_VERSION: u32 = 1;

#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub struct Query {
    pub query_id: String,
    pub window: TimeWindow,
    pub trace_id: Option<String>,
    pub limits: QueryLimits,
    pub deadline: DateTime<Utc>,
}

impl Query {
    pub fn validate(&self, now: DateTime<Utc>) -> Result<(), ValidationError> {
        self.window.validate()?;
        if !valid_identifier(&self.query_id, 128)
            || self
                .trace_id
                .as_ref()
                .is_some_and(|value| !valid_identifier(value, 256))
        {
            return Err(ValidationError::InvalidIdentifier);
        }
        self.limits.validate()?;
        if self.deadline <= now {
            return Err(ValidationError::DeadlineExpired);
        }
        if self.deadline < self.window.to {
            return Err(ValidationError::DeadlineBeforeWindowEnd);
        }
        Ok(())
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub struct TimeWindow {
    pub from: DateTime<Utc>,
    pub to: DateTime<Utc>,
}

impl TimeWindow {
    pub fn validate(&self) -> Result<(), ValidationError> {
        if self.from >= self.to {
            return Err(ValidationError::InvalidTimeWindow);
        }
        Ok(())
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub struct QueryLimits {
    pub max_lines_per_workload: usize,
    pub max_bytes_total: usize,
    pub context_lines: usize,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct ReadLimits {
    pub max_lines: usize,
    pub max_bytes: usize,
}

impl ReadLimits {
    pub const MAX_LINES: usize = 100_000;
    pub const MAX_BYTES: usize = 8 * 1024 * 1024;

    pub const fn per_workload_default() -> Self {
        Self {
            max_lines: Self::MAX_LINES,
            max_bytes: Self::MAX_BYTES,
        }
    }

    pub fn from_query(limits: &QueryLimits, trace_requested: bool) -> Self {
        // A trace match can sit anywhere in the window, so read the full cap.
        if trace_requested {
            return Self::per_workload_default();
        }
        // NOTE: 2x margin for lines the window filter and redaction drop later.
        Self {
            max_lines: limits
                .max_lines_per_workload
                .saturating_mul(2)
                .min(Self::MAX_LINES),
            max_bytes: limits
                .max_bytes_total
                .saturating_mul(2)
                .min(Self::MAX_BYTES),
        }
    }
}

impl QueryLimits {
    pub const MAX_LINES_PER_WORKLOAD: usize = 10_000;
    pub const MAX_BYTES_TOTAL: usize = 8 * 1024 * 1024;
    pub const MIN_BYTES_TOTAL: usize = 1_024;
    pub const MAX_CONTEXT_LINES: usize = 100;

    pub fn validate(&self) -> Result<(), ValidationError> {
        if self.max_lines_per_workload == 0
            || self.max_lines_per_workload > Self::MAX_LINES_PER_WORKLOAD
            || self.max_bytes_total < Self::MIN_BYTES_TOTAL
            || self.max_bytes_total > Self::MAX_BYTES_TOTAL
            || self.context_lines > Self::MAX_CONTEXT_LINES
        {
            return Err(ValidationError::InvalidLimits);
        }
        Ok(())
    }
}

#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub struct Workload {
    pub id: String,
    pub name: String,
    /// A source-neutral grouping label, such as a host or namespace.
    pub scope_label: String,
    #[serde(skip_serializing, default)]
    pub labels: BTreeMap<String, String>,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum LogStream {
    Stdout,
    Stderr,
    Unknown,
}

#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub struct RawLogLine {
    pub timestamp: Option<DateTime<Utc>>,
    pub stream: LogStream,
    pub message: String,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct RawLogs {
    pub lines: Vec<RawLogLine>,
    pub diagnostics: WorkloadDiagnostics,
    pub coverage: CoverageStatus,
    pub oldest_available: Option<DateTime<Utc>>,
    pub error: Option<SourceError>,
    pub discarded_line_count: u64,
    pub truncated: bool,
}

#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub struct WorkloadDiagnostics {
    pub runtime_state: DiagnosticValue<String>,
    pub restart_count: DiagnosticValue<u64>,
    pub oom_killed: DiagnosticValue<bool>,
    pub image: DiagnosticValue<String>,
    /// Configured limits, not observed CPU/memory utilization.
    pub configured_cpu_nanocpus: DiagnosticValue<u64>,
    pub configured_memory_bytes: DiagnosticValue<u64>,
}

impl Default for WorkloadDiagnostics {
    fn default() -> Self {
        Self {
            runtime_state: DiagnosticValue::unavailable("source_did_not_provide_value"),
            restart_count: DiagnosticValue::unavailable("source_did_not_provide_value"),
            oom_killed: DiagnosticValue::unavailable("source_did_not_provide_value"),
            image: DiagnosticValue::unavailable("source_did_not_provide_value"),
            configured_cpu_nanocpus: DiagnosticValue::unavailable("source_did_not_provide_value"),
            configured_memory_bytes: DiagnosticValue::unavailable("source_did_not_provide_value"),
        }
    }
}

#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case", tag = "availability", content = "value")]
pub enum DiagnosticValue<T> {
    Available(T),
    Unavailable { reason: String },
}

impl<T> DiagnosticValue<T> {
    pub fn unavailable(reason: impl Into<String>) -> Self {
        Self::Unavailable {
            reason: reason.into(),
        }
    }
}

#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub struct LogEntry {
    pub timestamp: Option<DateTime<Utc>>,
    pub stream: LogStream,
    pub message: String,
    pub match_kind: MatchKind,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum MatchKind {
    Trace,
    Context,
    Window,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum QueryStatus {
    Complete,
    Partial,
    Failed,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum WorkloadStatus {
    Complete,
    Partial,
    Unavailable,
}

#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum CoverageStatus {
    Complete,
    Partial,
    #[default]
    Unknown,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum SourceErrorKind {
    AccessDenied,
    SourceUnreachable,
    DriverUnsupported,
    HistoryUnavailable,
    WorkloadNotFound,
    Timeout,
    InvalidResponse,
}

#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub struct SourceError {
    pub kind: SourceErrorKind,
    /// Safe, bounded diagnostic text. Must never contain raw log content.
    pub detail: String,
}

#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub struct WorkloadResult {
    pub workload: Workload,
    pub status: WorkloadStatus,
    pub coverage: CoverageStatus,
    pub entries: Vec<LogEntry>,
    pub diagnostics: WorkloadDiagnostics,
    pub error: Option<SourceError>,
    pub dropped_line_count: u64,
}

#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub struct QueryResult {
    pub schema_version: u32,
    pub query_id: String,
    pub status: QueryStatus,
    pub requested_window: TimeWindow,
    pub oldest_available: Option<DateTime<Utc>>,
    pub source_error: Option<SourceError>,
    pub workloads: Vec<WorkloadResult>,
    pub truncated: bool,
    pub dropped_line_count: u64,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum ValidationError {
    InvalidTimeWindow,
    InvalidLimits,
    InvalidIdentifier,
    DeadlineExpired,
    DeadlineBeforeWindowEnd,
}

fn valid_identifier(value: &str, max_bytes: usize) -> bool {
    !value.trim().is_empty() && value.len() <= max_bytes && !value.chars().any(char::is_control)
}

#[cfg(test)]
mod tests {
    use chrono::TimeZone;

    use super::*;

    fn at(seconds: i64) -> DateTime<Utc> {
        Utc.timestamp_opt(seconds, 0)
            .single()
            .expect("valid test time")
    }

    fn query() -> Query {
        Query {
            query_id: "query-1".to_owned(),
            window: TimeWindow {
                from: at(10),
                to: at(20),
            },
            trace_id: None,
            limits: QueryLimits {
                max_lines_per_workload: 100,
                max_bytes_total: 64 * 1024,
                context_lines: 5,
            },
            deadline: at(30),
        }
    }

    #[test]
    fn validates_a_well_formed_query() {
        assert_eq!(query().validate(at(0)), Ok(()));
    }

    #[test]
    fn rejects_empty_or_reversed_time_windows() {
        let mut value = query();
        value.window.to = value.window.from;
        assert_eq!(
            value.validate(at(0)),
            Err(ValidationError::InvalidTimeWindow)
        );
        value.window.from = at(21);
        value.window.to = at(20);
        assert_eq!(
            value.validate(at(0)),
            Err(ValidationError::InvalidTimeWindow)
        );
    }

    #[test]
    fn rejects_zero_or_excessive_limits() {
        let mut value = query();
        value.limits.max_lines_per_workload = 0;
        assert_eq!(value.validate(at(0)), Err(ValidationError::InvalidLimits));
        value.limits.max_lines_per_workload = QueryLimits::MAX_LINES_PER_WORKLOAD + 1;
        assert_eq!(value.validate(at(0)), Err(ValidationError::InvalidLimits));
    }

    #[test]
    fn derives_read_limits_from_the_query_with_a_margin() {
        let limits = QueryLimits {
            max_lines_per_workload: 1_000,
            max_bytes_total: 64 * 1024,
            context_lines: 5,
        };
        assert_eq!(
            ReadLimits::from_query(&limits, false),
            ReadLimits {
                max_lines: 2_000,
                max_bytes: 128 * 1024,
            }
        );
        assert_eq!(
            ReadLimits::from_query(&limits, true),
            ReadLimits::per_workload_default()
        );
        let max = QueryLimits {
            max_lines_per_workload: QueryLimits::MAX_LINES_PER_WORKLOAD,
            max_bytes_total: QueryLimits::MAX_BYTES_TOTAL,
            context_lines: 0,
        };
        assert_eq!(
            ReadLimits::from_query(&max, false),
            ReadLimits {
                max_lines: 20_000,
                max_bytes: ReadLimits::MAX_BYTES,
            }
        );
    }

    #[test]
    fn rejects_expired_deadlines_and_invalid_ids() {
        let mut value = query();
        assert_eq!(
            value.validate(at(30)),
            Err(ValidationError::DeadlineExpired)
        );
        value.query_id = "bad\nid".to_owned();
        assert_eq!(
            value.validate(at(0)),
            Err(ValidationError::InvalidIdentifier)
        );
    }
}
