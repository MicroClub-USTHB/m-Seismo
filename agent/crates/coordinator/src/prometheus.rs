//! `GET /metrics` for Prometheus. Each scrape fans a metrics query out to the
//! agents, so the numbers are gauges over the last `window_secs`, not
//! counters: error rate is `error_lines / lines`.
use crate::{config, registry::Registry};
use prost_types::Timestamp;
use seismo_proto::{self as pb, AgentStatus};
use std::{
    fmt::Write as _,
    sync::Arc,
    time::{Duration, SystemTime, UNIX_EPOCH},
};
use tokio::{
    io::{AsyncBufReadExt, AsyncWriteExt, BufReader},
    net::TcpListener,
};

pub async fn serve(listener: TcpListener, registry: Arc<Registry>, cfg: config::Metrics) {
    loop {
        let Ok((mut sock, _)) = listener.accept().await else {
            continue;
        };
        let (registry, cfg) = (registry.clone(), cfg.clone());
        tokio::spawn(async move {
            // NOTE: only the request line matters, headers and body are never read
            let mut line = String::new();
            if BufReader::new(&mut sock)
                .read_line(&mut line)
                .await
                .is_err()
            {
                return;
            }
            let (status, body) = if line.starts_with("GET /metrics ") {
                ("200 OK", scrape(&registry, &cfg).await)
            } else {
                ("404 Not Found", String::new())
            };
            let head = format!(
                "HTTP/1.1 {status}\r\ncontent-type: text/plain; version=0.0.4\r\n\
                 content-length: {}\r\nconnection: close\r\n\r\n",
                body.len()
            );
            let _ = sock.write_all(head.as_bytes()).await;
            let _ = sock.write_all(body.as_bytes()).await;
        });
    }
}

async fn scrape(registry: &Registry, cfg: &config::Metrics) -> String {
    let now = SystemTime::now();
    let ts = |t: SystemTime| {
        let d = t.duration_since(UNIX_EPOCH).unwrap_or_default();
        Timestamp {
            seconds: d.as_secs() as i64,
            nanos: d.subsec_nanos() as i32,
        }
    };
    let timeout = Duration::from_millis(cfg.timeout_ms);
    let q = pb::MetricsQuery {
        query_id: String::new(),
        window: Some(pb::TimeWindow {
            from: Some(ts(now - Duration::from_secs(cfg.window_secs))),
            to: Some(ts(now)),
        }),
        // the margin is spent aggregating, so it comes on top of the timeout
        deadline: Some(ts(now
            + timeout
            + Duration::from_millis(registry.query.margin_ms))),
    };
    let agg = registry.query_metrics(q).await.unwrap_or_default();
    render(&agg, registry.len())
}

fn render(agg: &pb::AggregateMetrics, connected: usize) -> String {
    let mut out = String::new();
    let _ = writeln!(out, "# TYPE seismo_agents_connected gauge");
    let _ = writeln!(out, "seismo_agents_connected {connected}");
    let _ = writeln!(out, "# TYPE seismo_agent_up gauge");
    for a in &agg.agents {
        let up = matches!(
            AgentStatus::try_from(a.status),
            Ok(AgentStatus::Ok | AgentStatus::Truncated)
        ) as u8;
        let _ = writeln!(
            out,
            "seismo_agent_up{{agent=\"{}\"}} {up}",
            esc(&a.agent_id)
        );
    }
    for (name, help) in [
        ("seismo_log_lines_window", "Log lines in the scrape window."),
        (
            "seismo_error_lines_window",
            "Log lines that look like errors in the scrape window.",
        ),
    ] {
        let _ = writeln!(out, "# HELP {name} {help}\n# TYPE {name} gauge");
        for a in &agg.agents {
            for s in a.services.iter().filter(|s| s.error.is_none()) {
                let n = if name == "seismo_log_lines_window" {
                    s.lines
                } else {
                    s.error_lines
                };
                let _ = writeln!(
                    out,
                    "{name}{{agent=\"{}\",service=\"{}\"}} {n}",
                    esc(&a.agent_id),
                    esc(&s.service)
                );
            }
        }
    }
    out
}

/// Label values escape backslash, quote and newline.
fn esc(s: &str) -> String {
    s.replace('\\', "\\\\")
        .replace('"', "\\\"")
        .replace('\n', "\\n")
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn renders_gauges_and_escapes_labels() {
        let agg = pb::AggregateMetrics {
            agents: vec![pb::AgentMetrics {
                agent_id: "a\"1".into(),
                status: AgentStatus::Ok as i32,
                services: vec![pb::ServiceMetrics {
                    service: "api".into(),
                    lines: 10,
                    error_lines: 2,
                    ..Default::default()
                }],
                ..Default::default()
            }],
            ..Default::default()
        };
        let out = render(&agg, 1);
        assert!(out.contains("seismo_agents_connected 1"));
        assert!(out.contains("seismo_agent_up{agent=\"a\\\"1\"} 1"));
        assert!(out.contains("seismo_log_lines_window{agent=\"a\\\"1\",service=\"api\"} 10"));
        assert!(out.contains("seismo_error_lines_window{agent=\"a\\\"1\",service=\"api\"} 2"));
    }
}
