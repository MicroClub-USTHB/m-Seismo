//! Fan a backend `QueryLogs` out to every connected agent and aggregate.
use crate::registry::{AgentHandle, Registry, Reply};
use futures_util::future::join_all;
use prost_types::Timestamp;
use seismo_proto::{
    self as pb, AgentStatus, SourceError, SourceErrorKind, WarningCode,
    coordinator_message::Message,
};
use std::{
    collections::BTreeMap,
    sync::atomic::Ordering,
    time::{Duration, SystemTime, UNIX_EPOCH},
};
use tokio::{sync::oneshot, time::Instant};

pub const SCHEMA_VERSION: u32 = 1;

const MIN_SHARE: usize = 1 << 10;
const MAX_SHARE: usize = 8 << 20;

enum Outcome {
    Reply(Reply),
    Disconnected,
    Timeout,
}

impl Registry {
    /// One `AggregateResult` for the whole deployment. Agents that fail to
    /// answer in time don't fail the query, they get their own status.
    pub async fn query_logs(
        &self,
        q: pb::QueryLogs,
    ) -> Result<pb::AggregateResult, pb::AggregateError> {
        let fail = |detail: &str| pb::AggregateError {
            query_id: q.query_id.clone(),
            error: Some(SourceError {
                kind: SourceErrorKind::Unspecified as i32,
                detail: detail.into(),
            }),
        };
        let Some(window) = q.window else {
            return Err(fail("missing window"));
        };
        let left = remaining(q.deadline.as_ref())
            .ok_or_else(|| fail("deadline missing or already passed"))?;
        // keep some time back to aggregate and answer
        let wait = left
            .checked_sub(Duration::from_millis(self.query.margin_ms))
            .filter(|d| !d.is_zero())
            .ok_or_else(|| fail("deadline is closer than the aggregation margin"))?;

        let agents = self.snapshot();
        let share = (self.query.total_budget_bytes as usize / agents.len().max(1))
            .clamp(MIN_SHARE, MAX_SHARE);
        let ask = Ask {
            window,
            trace_id: q.trace_id.clone(),
            limits: pb::QueryLimits {
                max_lines_per_workload: self.query.max_lines_per_workload,
                max_bytes_total: share as u32,
                context_lines: self.query.context_lines,
            },
            deadline: after(wait),
            until: Instant::now() + wait,
        };
        let sections = join_all(agents.iter().map(|a| self.ask(a, &ask))).await;

        Ok(pb::AggregateResult {
            schema_version: SCHEMA_VERSION,
            query_id: q.query_id,
            requested_window: Some(window),
            agents: sections,
            warnings: duplicate_services(&agents),
        })
    }

    async fn ask(&self, a: &AgentHandle, ask: &Ask) -> pb::AgentSection {
        let query_id = format!(
            "{}-{}",
            self.coordinator_id,
            self.next_query.fetch_add(1, Ordering::Relaxed)
        );
        let (tx, rx) = oneshot::channel();
        a.pending.lock().unwrap().insert(query_id.clone(), tx);
        let query = pb::Query {
            query_id: query_id.clone(),
            window: Some(ask.window),
            trace_id: ask.trace_id.clone(),
            limits: Some(ask.limits),
            deadline: Some(ask.deadline),
        };

        // sending can block too if the agent stopped reading, so it is under
        // the same deadline as the wait
        let outcome = tokio::time::timeout_at(ask.until, async {
            if a.tx.send(Ok(wire(Message::Query(query)))).await.is_err() {
                return Outcome::Disconnected;
            }
            match rx.await {
                Ok(reply) => Outcome::Reply(reply),
                // deregister() dropped our sender
                Err(_) => Outcome::Disconnected,
            }
        })
        .await
        .unwrap_or(Outcome::Timeout);

        a.pending.lock().unwrap().remove(&query_id);
        if matches!(outcome, Outcome::Timeout) {
            // best effort, the agent may be gone or slow
            let cancel = pb::Cancel { query_id };
            let _ = a.tx.try_send(Ok(wire(Message::Cancel(cancel))));
        }
        section(a, outcome)
    }
}

struct Ask {
    window: pb::TimeWindow,
    trace_id: Option<String>,
    limits: pb::QueryLimits,
    /// Sent to the agents, absolute.
    deadline: Timestamp,
    /// When we stop waiting for them.
    until: Instant,
}

fn wire(m: Message) -> pb::CoordinatorMessage {
    pb::CoordinatorMessage { message: Some(m) }
}

fn section(a: &AgentHandle, outcome: Outcome) -> pb::AgentSection {
    let (status, result, detail) = match outcome {
        Outcome::Reply(Reply::Result(r)) => {
            let status = if r.truncated {
                AgentStatus::Truncated
            } else {
                AgentStatus::Ok
            };
            (status, Some(r), String::new())
        }
        Outcome::Reply(Reply::Error(e)) => {
            let detail = e.error.map_or("no detail".into(), |e| {
                let kind = SourceErrorKind::try_from(e.kind).map_or("UNKNOWN", |k| k.as_str_name());
                format!("{kind}: {}", e.detail)
            });
            (AgentStatus::Error, None, detail)
        }
        Outcome::Disconnected => (
            AgentStatus::Disconnected,
            None,
            "stream closed before the agent answered".into(),
        ),
        Outcome::Timeout => (
            AgentStatus::Timeout,
            None,
            "no answer before the deadline".into(),
        ),
    };
    pb::AgentSection {
        agent_id: a.id.clone(),
        source: a.source,
        status: status as i32,
        result,
        detail,
    }
}

/// One warning per service that more than one agent claims.
pub(crate) fn duplicate_services(agents: &[AgentHandle]) -> Vec<pb::Warning> {
    let mut by_service: BTreeMap<&str, Vec<&str>> = BTreeMap::new();
    for a in agents {
        for s in &a.services {
            let ids = by_service.entry(s).or_default();
            // an agent listing a service twice is not a duplicate
            if !ids.contains(&a.id.as_str()) {
                ids.push(&a.id);
            }
        }
    }
    by_service
        .into_iter()
        .filter(|(_, ids)| ids.len() > 1)
        .map(|(service, ids)| pb::Warning {
            code: WarningCode::DuplicateService as i32,
            detail: format!("service {service} is claimed by {} agents", ids.len()),
            service: Some(service.to_owned()),
            // agents come sorted by id, so these are too
            agent_ids: ids.into_iter().map(str::to_owned).collect(),
        })
        .collect()
}

/// Time left until `deadline`, `None` when it is missing or already past.
fn remaining(deadline: Option<&Timestamp>) -> Option<Duration> {
    let d = deadline?;
    let now = SystemTime::now().duration_since(UNIX_EPOCH).ok()?;
    Duration::new(d.seconds.max(0) as u64, d.nanos.max(0) as u32)
        .checked_sub(now)
        .filter(|r| !r.is_zero())
}

fn after(d: Duration) -> Timestamp {
    let t = SystemTime::now()
        .checked_add(d)
        .and_then(|t| t.duration_since(UNIX_EPOCH).ok())
        .unwrap_or_default();
    Timestamp {
        seconds: t.as_secs() as i64,
        nanos: t.subsec_nanos() as i32,
    }
}
