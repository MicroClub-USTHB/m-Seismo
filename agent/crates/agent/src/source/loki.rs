use super::{Fetched, Params, err, ns_to_ts, ts_to_ns};
use crate::config;
use seismo_proto::{self as pb, LogStream, MatchKind, SourceError, SourceErrorKind};
use serde::Deserialize;
use std::collections::HashMap;

pub struct Loki {
    http: reqwest::Client,
    cfg: config::Loki,
}

#[derive(Deserialize)]
struct Response {
    status: String,
    data: Data,
}

#[derive(Deserialize)]
struct Data {
    result: Vec<Stream>,
}

#[derive(Deserialize)]
struct Stream {
    stream: HashMap<String, String>,
    // NOTE: [ts_ns, line] but newer lokis append a structured metadata object,
    // so keep it loose and only look at the first two
    values: Vec<Vec<serde_json::Value>>,
}

/// LogQL string literal escaping.
fn quote(s: &str) -> String {
    format!("\"{}\"", s.replace('\\', "\\\\").replace('"', "\\\""))
}

impl Loki {
    pub fn new(cfg: config::Loki) -> Result<Self, String> {
        let http = reqwest::Client::builder()
            .build()
            .map_err(|e| e.to_string())?;
        Ok(Self { http, cfg })
    }

    fn logql(&self, service: &str, trace_id: Option<&str>) -> String {
        let mut q = format!("{{{}={}}}", self.cfg.service_label, quote(service));
        match (trace_id, self.cfg.trace_field.as_str()) {
            (None, _) => {}
            (Some(id), "") => q.push_str(&format!(" |= {}", quote(id))),
            (Some(id), field) => q.push_str(&format!(" | {field}={}", quote(id))),
        }
        q
    }

    pub async fn fetch(&self, p: Params<'_>) -> Result<Fetched, SourceError> {
        let url = format!(
            "{}/loki/api/v1/query_range",
            self.cfg.url.trim_end_matches('/')
        );
        let mut req = self.http.get(url).timeout(p.timeout).query(&[
            ("query", self.logql(p.service, p.trace_id)),
            ("start", ts_to_ns(p.from).to_string()),
            ("end", ts_to_ns(p.to).to_string()),
            ("limit", p.limit.to_string()),
            // newest first so the limit cuts the old end, we flip it after
            ("direction", "backward".into()),
        ]);
        if let Some(t) = &self.cfg.tenant {
            req = req.header("X-Scope-OrgID", t);
        }
        req = match &self.cfg.auth {
            config::Auth::None => req,
            config::Auth::Basic {
                username, password, ..
            } => req.basic_auth(username, Some(password)),
            config::Auth::Bearer { token, .. } => req.bearer_auth(token),
        };

        let resp = req.send().await.map_err(transport_err)?;
        let status = resp.status();
        if !status.is_success() {
            let body = resp.text().await.unwrap_or_default();
            let detail = format!(
                "loki answered {status}: {}",
                body.chars().take(200).collect::<String>()
            );
            let kind = match status.as_u16() {
                401 | 403 => SourceErrorKind::AccessDenied,
                408 | 504 => SourceErrorKind::Timeout,
                429 | 500..=599 => SourceErrorKind::SourceUnreachable,
                _ => SourceErrorKind::InvalidResponse,
            };
            return Err(err(kind, detail));
        }
        let body: Response = resp.json().await.map_err(transport_err)?;
        if body.status != "success" {
            return Err(err(
                SourceErrorKind::InvalidResponse,
                "loki status is not success",
            ));
        }
        self.to_fetched(body.data.result, p.trace_id.is_some(), p.limit)
    }

    fn to_fetched(
        &self,
        streams: Vec<Stream>,
        traced: bool,
        limit: u32,
    ) -> Result<Fetched, SourceError> {
        let bad = || err(SourceErrorKind::InvalidResponse, "malformed loki value");
        let match_kind = if traced {
            MatchKind::Trace
        } else {
            MatchKind::Window
        };
        let mut scope_label = String::new();
        let mut rows = Vec::new();
        for s in streams {
            if scope_label.is_empty()
                && let Some(v) = self.cfg.scope_label.as_ref().and_then(|k| s.stream.get(k))
            {
                scope_label = v.clone();
            }
            let stream = match s.stream.get("stream").map(String::as_str) {
                Some("stdout") => LogStream::Stdout,
                Some("stderr") => LogStream::Stderr,
                _ => LogStream::Unknown,
            };
            for v in s.values {
                let ns: i128 = v
                    .first()
                    .and_then(|x| x.as_str())
                    .and_then(|x| x.parse().ok())
                    .ok_or_else(bad)?;
                let line = v.get(1).and_then(|x| x.as_str()).ok_or_else(bad)?;
                rows.push((ns, stream, line.to_owned()));
            }
        }
        let hit_limit = rows.len() >= limit as usize;
        // stable, so same-timestamp lines keep the order loki gave us
        rows.sort_by_key(|r| r.0);
        let entries = rows
            .into_iter()
            .map(|(ns, stream, message)| pb::LogEntry {
                timestamp: Some(ns_to_ts(ns)),
                stream: stream as i32,
                message,
                match_kind: match_kind as i32,
            })
            .collect();
        Ok(Fetched {
            entries,
            scope_label,
            hit_limit,
        })
    }
}

fn transport_err(e: reqwest::Error) -> SourceError {
    let kind = if e.is_timeout() {
        SourceErrorKind::Timeout
    } else if e.is_decode() {
        SourceErrorKind::InvalidResponse
    } else {
        SourceErrorKind::SourceUnreachable
    };
    err(kind, e.without_url().to_string())
}
