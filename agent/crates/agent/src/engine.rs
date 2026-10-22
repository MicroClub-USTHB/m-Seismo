use crate::{
    redaction::redact,
    source::{Fetched, Params, Source},
};
use futures_util::future::join_all;
use prost_types::Timestamp;
use seismo_proto::{
    self as pb, BoolDiagnostic, CoverageStatus, QueryStatus, SourceError, SourceErrorKind,
    StringDiagnostic, UintDiagnostic, Workload, WorkloadDiagnostics, WorkloadResult,
    WorkloadStatus,
};
use std::time::{Duration, SystemTime, UNIX_EPOCH};

// NOTE: used when the coordinator leaves a limit at 0, it never should
const DEFAULT_LINES: u32 = 1000;
const DEFAULT_BYTES: usize = 4 << 20;
const DEFAULT_TIMEOUT: Duration = Duration::from_secs(30);

pub struct Engine {
    services: Vec<String>,
    source: Source,
}

fn source_err(kind: SourceErrorKind, detail: &str) -> SourceError {
    SourceError {
        kind: kind as i32,
        detail: detail.into(),
    }
}

/// Time left until `deadline`, `None` if it already passed.
fn remaining(deadline: Option<&Timestamp>) -> Option<Duration> {
    let Some(d) = deadline else {
        return Some(DEFAULT_TIMEOUT);
    };
    let now = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap_or_default();
    let at = Duration::new(d.seconds.max(0) as u64, d.nanos.max(0) as u32);
    at.checked_sub(now).filter(|r| !r.is_zero())
}

/// Keeps the newest entries that fit in `budget`, returns how many it dropped.
// XXX: counts message bytes only, proto framing is small enough to ignore
fn fit(entries: &mut Vec<pb::LogEntry>, budget: &mut usize) -> u64 {
    let (mut keep, mut used) = (0, 0);
    for e in entries.iter().rev() {
        if used + e.message.len() > *budget {
            break;
        }
        used += e.message.len();
        keep += 1;
    }
    let dropped = entries.len() - keep;
    entries.drain(..dropped);
    *budget -= used;
    dropped as u64
}

fn unavailable() -> WorkloadDiagnostics {
    const WHY: &str = "not collected by this log source";
    let s = || StringDiagnostic {
        value: Some(pb::string_diagnostic::Value::UnavailableReason(WHY.into())),
    };
    let u = || UintDiagnostic {
        value: Some(pb::uint_diagnostic::Value::UnavailableReason(WHY.into())),
    };
    WorkloadDiagnostics {
        runtime_state: Some(s()),
        restart_count: Some(u()),
        oom_killed: Some(BoolDiagnostic {
            value: Some(pb::bool_diagnostic::Value::UnavailableReason(WHY.into())),
        }),
        image: Some(s()),
        configured_cpu_nanocpus: Some(u()),
        configured_memory_bytes: Some(u()),
    }
}

impl Engine {
    pub fn new(services: Vec<String>, source: Source) -> Self {
        Self { services, source }
    }

    pub fn kind(&self) -> pb::AgentSource {
        self.source.kind()
    }

    pub fn services(&self) -> &[String] {
        &self.services
    }

    pub async fn answer(&self, q: &pb::Query) -> Result<pb::QueryResult, pb::QueryError> {
        let fail = |kind, detail| pb::QueryError {
            query_id: q.query_id.clone(),
            error: Some(source_err(kind, detail)),
        };
        let (Some(win), Some(timeout)) = (&q.window, remaining(q.deadline.as_ref())) else {
            return Err(fail(
                SourceErrorKind::Unspecified,
                "missing window or deadline already passed",
            ));
        };
        let (Some(from), Some(to)) = (&win.from, &win.to) else {
            return Err(fail(
                SourceErrorKind::Unspecified,
                "window needs from and to",
            ));
        };
        if (from.seconds, from.nanos) > (to.seconds, to.nanos) {
            return Err(fail(
                SourceErrorKind::Unspecified,
                "window.from is after window.to",
            ));
        }

        let limits = q.limits.unwrap_or_default();
        let limit = if limits.max_lines_per_workload == 0 {
            DEFAULT_LINES
        } else {
            limits.max_lines_per_workload
        };
        let mut budget = match limits.max_bytes_total {
            0 => DEFAULT_BYTES,
            n => n as usize,
        };

        // one fetch per service, all at once
        let fetched = join_all(self.services.iter().map(|service| {
            self.source.fetch(Params {
                service,
                from,
                to,
                trace_id: q.trace_id.as_deref(),
                limit,
                timeout,
            })
        }))
        .await;

        let (mut workloads, mut dropped_total, mut first_err) = (Vec::new(), 0, None);
        for (service, res) in self.services.iter().zip(fetched) {
            workloads.push(workload(
                service,
                res,
                &mut budget,
                &mut dropped_total,
                &mut first_err,
            ));
        }

        let failed = workloads
            .iter()
            .filter(|w| w.status == WorkloadStatus::Unavailable as i32)
            .count();
        let truncated = workloads
            .iter()
            .any(|w| w.status == WorkloadStatus::Partial as i32);
        let all_failed = failed == workloads.len();
        let status = if all_failed {
            QueryStatus::Failed
        } else if failed > 0 || truncated {
            QueryStatus::Partial
        } else {
            QueryStatus::Complete
        };
        Ok(pb::QueryResult {
            query_id: q.query_id.clone(),
            status: status as i32,
            requested_window: q.window,
            oldest_available: None,
            source_error: first_err.filter(|_| all_failed),
            workloads,
            truncated,
            dropped_line_count: dropped_total,
        })
    }
}

fn workload(
    service: &str,
    res: Result<Fetched, SourceError>,
    budget: &mut usize,
    dropped_total: &mut u64,
    first_err: &mut Option<SourceError>,
) -> WorkloadResult {
    let mut w = WorkloadResult {
        workload: Some(Workload {
            id: service.into(),
            name: service.into(),
            scope_label: String::new(),
        }),
        diagnostics: Some(unavailable()),
        ..Default::default()
    };
    match res {
        Err(e) => {
            w.status = WorkloadStatus::Unavailable as i32;
            w.coverage = CoverageStatus::Unknown as i32;
            first_err.get_or_insert_with(|| e.clone());
            w.error = Some(e);
        }
        Ok(mut f) => {
            // the one place raw lines get scrubbed, before the byte budget so
            // it counts what we actually send
            for e in &mut f.entries {
                e.message = redact(&e.message);
            }
            w.dropped_line_count = fit(&mut f.entries, budget);
            *dropped_total += w.dropped_line_count;
            let cut = f.hit_limit || w.dropped_line_count > 0;
            let (status, coverage) = if cut {
                (WorkloadStatus::Partial, CoverageStatus::Partial)
            } else {
                (WorkloadStatus::Complete, CoverageStatus::Complete)
            };
            w.status = status as i32;
            w.coverage = coverage as i32;
            w.entries = f.entries;
            if let Some(wl) = &mut w.workload {
                wl.scope_label = f.scope_label;
            }
        }
    }
    w
}
