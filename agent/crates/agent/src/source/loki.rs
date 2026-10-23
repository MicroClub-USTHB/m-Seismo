use super::{Fetched, Params, Raw, err, finish, ts_to_ns};
use crate::config;
use seismo_proto::{LogStream, SourceError, SourceErrorKind};
use serde::Deserialize;
use std::{
    collections::HashMap,
    time::{Duration, Instant},
};

// NOTE: loki refuses more than max_entries_limit_per_query lines per request,
// 5000 by default, so that is as much surrounding text as we can look at
const CONTEXT_SCAN_LINES: u32 = 5000;

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
        // one budget for all the requests this fetch makes
        let deadline = Instant::now() + p.timeout;
        let left = || deadline.saturating_duration_since(Instant::now());
        let (from, to) = (ts_to_ns(p.from), ts_to_ns(p.to));
        let traced = p.trace_id.is_some();

        let found = self
            .query(
                &self.logql(p.service, p.trace_id),
                (from, to),
                p.limit,
                "backward",
                left(),
            )
            .await?;
        let hit_limit = found.rows.len() >= p.limit as usize;
        let (mut scope_label, mut rows) = (found.scope, found.rows);

        if traced && p.context > 0 && !rows.is_empty() {
            // loki can't give us "N lines around this one", so grab everything
            // up to the newest match (newest first, the cap cuts the old end)
            // plus a few lines after it, and mark which ones are the matches
            let newest = rows.last().map_or(from, |r| r.ts);
            let plain = self.logql(p.service, None);
            let (back, fwd) = tokio::join!(
                self.query(
                    &plain,
                    (from, newest + 1),
                    CONTEXT_SCAN_LINES,
                    "backward",
                    left()
                ),
                async {
                    if newest + 1 >= to {
                        return Ok(Page::default());
                    }
                    self.query(&plain, (newest + 1, to), p.context, "forward", left())
                        .await
                },
            );
            let (back, fwd) = (back?, fwd?);
            if scope_label.is_empty() {
                scope_label = back.scope;
            }
            rows = mark_matches(back.rows.into_iter().chain(fwd.rows).collect(), &rows);
        }

        let entries = finish(rows, traced, p.context as usize, p.limit as usize);
        Ok(Fetched {
            entries,
            scope_label,
            hit_limit,
        })
    }

    /// One `query_range` call. Rows come back oldest first, all `matched`.
    async fn query(
        &self,
        logql: &str,
        (start, end): (i128, i128),
        limit: u32,
        direction: &str,
        timeout: Duration,
    ) -> Result<Page, SourceError> {
        let url = format!(
            "{}/loki/api/v1/query_range",
            self.cfg.url.trim_end_matches('/')
        );
        let mut req = self.http.get(url).timeout(timeout).query(&[
            ("query", logql.to_owned()),
            ("start", start.to_string()),
            ("end", end.to_string()),
            ("limit", limit.to_string()),
            ("direction", direction.to_owned()),
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
        self.to_page(body.data.result)
    }

    fn to_page(&self, streams: Vec<Stream>) -> Result<Page, SourceError> {
        let bad = || err(SourceErrorKind::InvalidResponse, "malformed loki value");
        let mut page = Page::default();
        for s in streams {
            if page.scope.is_empty()
                && let Some(v) = self.cfg.scope_label.as_ref().and_then(|k| s.stream.get(k))
            {
                page.scope = v.clone();
            }
            let stream = match s.stream.get("stream").map(String::as_str) {
                Some("stdout") => LogStream::Stdout,
                Some("stderr") => LogStream::Stderr,
                _ => LogStream::Unknown,
            };
            for v in s.values {
                let ts: i128 = v
                    .first()
                    .and_then(|x| x.as_str())
                    .and_then(|x| x.parse().ok())
                    .ok_or_else(bad)?;
                let text = v.get(1).and_then(|x| x.as_str()).ok_or_else(bad)?;
                page.rows.push(Raw {
                    ts,
                    stream,
                    text: text.to_owned(),
                    matched: true,
                });
            }
        }
        // stable, so same-timestamp lines keep the order loki gave us
        page.rows.sort_by_key(|r| r.ts);
        Ok(page)
    }
}

#[derive(Default)]
struct Page {
    rows: Vec<Raw>,
    scope: String,
}

/// Flags the rows of `plain` that are one of `matches` (same time and text).
/// Matches the plain scan didn't reach are put back, so none get lost.
fn mark_matches(mut plain: Vec<Raw>, matches: &[Raw]) -> Vec<Raw> {
    let mut left: HashMap<(i128, String), usize> = HashMap::new();
    for m in matches {
        *left.entry((m.ts, m.text.clone())).or_default() += 1;
    }
    for r in &mut plain {
        r.matched = match left.get_mut(&(r.ts, r.text.clone())) {
            Some(n) if *n > 0 => {
                *n -= 1;
                true
            }
            _ => false,
        };
    }
    for m in matches {
        if let Some(n) = left.get_mut(&(m.ts, m.text.clone()))
            && *n > 0
        {
            *n -= 1;
            plain.push(m.clone());
        }
    }
    plain.sort_by_key(|r| r.ts);
    plain
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
