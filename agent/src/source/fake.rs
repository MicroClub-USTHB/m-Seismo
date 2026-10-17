//! Deterministic source used by query-engine unit tests.

use std::{collections::BTreeMap, future::Future, time::Duration};

use chrono::{DateTime, Utc};

use crate::{
    config::WorkloadScope,
    model::{
        CoverageStatus, RawLogLine, RawLogs, ReadLimits, SourceError, TimeWindow, Workload,
        WorkloadDiagnostics,
    },
    source::LogSource,
};

#[derive(Clone, Debug, Default)]
pub struct FakeSource {
    pub workloads: Vec<Workload>,
    pub logs: BTreeMap<String, Vec<RawLogLine>>,
    pub list_error: Option<SourceError>,
    pub workload_errors: BTreeMap<String, SourceError>,
    pub delays: BTreeMap<String, Duration>,
    pub coverage: CoverageStatus,
}

impl FakeSource {
    pub fn workload(name: &str) -> Workload {
        Workload {
            id: name.to_owned(),
            name: name.to_owned(),
            scope_label: "fake".to_owned(),
            labels: BTreeMap::new(),
        }
    }

    pub fn line(timestamp: DateTime<Utc>, message: &str) -> RawLogLine {
        RawLogLine {
            timestamp: Some(timestamp),
            stream: crate::model::LogStream::Stdout,
            message: message.to_owned(),
        }
    }
}

impl LogSource for FakeSource {
    #[allow(clippy::manual_async_fn)]
    fn list_workloads(
        &self,
        _scope: &WorkloadScope,
    ) -> impl Future<Output = Result<Vec<Workload>, SourceError>> + Send {
        async move {
            if let Some(error) = &self.list_error {
                return Err(error.clone());
            }
            Ok(self.workloads.clone())
        }
    }

    #[allow(clippy::manual_async_fn)]
    fn read_logs(
        &self,
        workload: &Workload,
        window: TimeWindow,
        limits: ReadLimits,
    ) -> impl Future<Output = Result<RawLogs, SourceError>> + Send {
        async move {
            if let Some(delay) = self.delays.get(&workload.id) {
                tokio::time::sleep(*delay).await;
            }
            if let Some(error) = self.workload_errors.get(&workload.id) {
                return Err(error.clone());
            }

            let available = self.logs.get(&workload.id).cloned().unwrap_or_default();
            let mut lines = Vec::new();
            let mut bytes = 0usize;
            let mut truncated = false;
            for line in available.into_iter().filter(|line| {
                line.timestamp
                    .is_none_or(|timestamp| timestamp >= window.from && timestamp <= window.to)
            }) {
                if lines.len() >= limits.max_lines
                    || bytes.saturating_add(line.message.len()) > limits.max_bytes
                {
                    truncated = true;
                    break;
                }
                bytes += line.message.len();
                lines.push(line);
            }
            let oldest_available = lines.iter().filter_map(|line| line.timestamp).min();
            Ok(RawLogs {
                lines,
                diagnostics: WorkloadDiagnostics::default(),
                coverage: self.coverage,
                oldest_available,
                error: None,
                discarded_line_count: 0,
                truncated,
            })
        }
    }
}
