//! Where log lines come from. Every source answers the same question: the
//! lines of one service inside a time window, newest kept when it has to cut.
pub mod file;
pub mod journal;
pub mod loki;

use crate::config;
use prost_types::Timestamp;
use seismo_proto::{self as pb, AgentSource, SourceError, SourceErrorKind};
use std::time::Duration;

pub struct Params<'a> {
    pub service: &'a str,
    pub from: &'a Timestamp,
    pub to: &'a Timestamp,
    pub trace_id: Option<&'a str>,
    pub limit: u32,
    pub timeout: Duration,
}

#[derive(Debug)]
pub struct Fetched {
    /// Redacted, oldest first.
    pub entries: Vec<pb::LogEntry>,
    pub scope_label: String,
    /// The source stopped at `limit` lines, so older ones were probably cut.
    pub hit_limit: bool,
}

// NOTE: enum dispatch instead of a trait object, a closed set of sources
// doesn't need async-trait or boxing
pub enum Source {
    Loki(loki::Loki),
    File(file::File),
    Journal(journal::Journal),
}

impl Source {
    pub fn build(cfg: &config::Source) -> Result<Self, String> {
        match cfg {
            config::Source::Loki(c) => Ok(Self::Loki(loki::Loki::new(c.clone())?)),
            config::Source::File(c) => Ok(Self::File(file::File::new(c))),
            config::Source::Journal(c) => Ok(Self::Journal(journal::Journal::new(c))),
        }
    }

    /// What we tell the coordinator in `Register.source`.
    pub fn kind(&self) -> AgentSource {
        match self {
            Self::Loki(_) => AgentSource::Loki,
            Self::File(_) => AgentSource::File,
            Self::Journal(_) => AgentSource::Journal,
        }
    }

    pub async fn fetch(&self, p: Params<'_>) -> Result<Fetched, SourceError> {
        match self {
            Self::Loki(s) => s.fetch(p).await,
            Self::File(s) => s.fetch(p).await,
            Self::Journal(s) => s.fetch(p).await,
        }
    }
}

pub(crate) fn err(kind: SourceErrorKind, detail: impl Into<String>) -> SourceError {
    SourceError {
        kind: kind as i32,
        detail: detail.into(),
    }
}

pub(crate) fn ts_to_ns(t: &Timestamp) -> i128 {
    t.seconds as i128 * 1_000_000_000 + t.nanos as i128
}

pub(crate) fn ns_to_ts(ns: i128) -> Timestamp {
    Timestamp {
        seconds: ns.div_euclid(1_000_000_000) as i64,
        nanos: ns.rem_euclid(1_000_000_000) as i32,
    }
}
