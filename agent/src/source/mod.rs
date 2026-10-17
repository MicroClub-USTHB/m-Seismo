//! Runtime adapters implement this interface and return source-neutral data.

use std::future::Future;

use crate::{
    config::WorkloadScope,
    model::{RawLogs, ReadLimits, SourceError, TimeWindow, Workload},
};

#[cfg(test)]
pub mod fake;
pub mod docker;

pub trait LogSource: Send + Sync {
    fn list_workloads(
        &self,
        scope: &WorkloadScope,
    ) -> impl Future<Output = Result<Vec<Workload>, SourceError>> + Send;

    fn read_logs(
        &self,
        workload: &Workload,
        window: TimeWindow,
        limits: ReadLimits,
    ) -> impl Future<Output = Result<RawLogs, SourceError>> + Send;
}
