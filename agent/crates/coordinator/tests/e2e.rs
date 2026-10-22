//! Real agents and a real coordinator, only the backend is a stand-in.
mod common;

use common::{Backend, logs_within};
use seismo_agent::{config as agent_cfg, engine::Engine, session, source::Source};
use seismo_coordinator::{agents, backend, config, registry::Registry};
use seismo_proto::{AgentSource, AgentStatus, coordinator_event::Event};
use std::{
    collections::HashMap,
    path::Path,
    sync::Arc,
    time::{Duration, Instant},
};
use tokio::{net::TcpListener, task::JoinHandle};

const TOKEN: &str = "e2e-token";

struct Stack {
    backend: Backend,
    registry: Arc<Registry>,
    agents_url: String,
}

async fn stack() -> Stack {
    let mut backend = Backend::start().await;
    let registry = Arc::new(Registry::new(
        "coord".into(),
        config::Query {
            margin_ms: 100,
            ..Default::default()
        },
    ));
    let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
    let agents_url = format!("http://{}", listener.local_addr().unwrap());
    tokio::spawn(agents::serve(
        listener,
        registry.clone(),
        TOKEN.into(),
        None,
    ));

    let cfg = config::Backend {
        url: backend.url.clone(),
        key_env: String::new(),
        key: "mco_e2e".into(),
    };
    let r = registry.clone();
    tokio::spawn(async move { backend::attach(&cfg, "coord", r).await });
    assert!(matches!(backend.next().await, Event::Hello(_)));
    Stack {
        backend,
        registry,
        agents_url,
    }
}

impl Stack {
    fn spawn_agent(
        &self,
        id: &str,
        services: &[&str],
        source: agent_cfg::Source,
    ) -> JoinHandle<()> {
        let cfg = agent_cfg::Config {
            agent_id: id.into(),
            services: services.iter().map(|s| s.to_string()).collect(),
            coordinator: agent_cfg::Coordinator {
                url: self.agents_url.clone(),
                token_env: String::new(),
                token: TOKEN.into(),
                ca_file: None,
                ca_pem: None,
            },
            source,
        };
        let engine = Arc::new(Engine::new(
            cfg.services.clone(),
            Source::build(&cfg.source).unwrap(),
        ));
        tokio::spawn(async move {
            let _ = session::session(&cfg, engine).await;
        })
    }

    async fn wait_for_agents(&self, n: usize) {
        for _ in 0..200 {
            if self.registry.len() == n {
                return;
            }
            tokio::time::sleep(Duration::from_millis(25)).await;
        }
        panic!("expected {n} agents, have {}", self.registry.len());
    }
}

/// A log file with one line at 101s, inside the window of `logs_within`.
fn log_file(dir: &Path, service: &str) -> agent_cfg::Source {
    let path = dir.join(format!("{service}.log"));
    std::fs::write(
        &path,
        format!("1970-01-01T00:01:41Z {service} says hi password=hunter2\n"),
    )
    .unwrap();
    agent_cfg::Source::File(agent_cfg::File {
        files: HashMap::from([(service.to_owned(), path)]),
        rotated: 0,
    })
}

/// A journal that never answers: `sh -c "sleep 30"` ignores the journalctl args.
fn stuck() -> agent_cfg::Source {
    agent_cfg::Source::Journal(agent_cfg::Journal {
        units: HashMap::new(),
        command: vec!["sh".into(), "-c".into(), "sleep 30".into()],
    })
}

#[tokio::test]
async fn two_agents_one_aggregate_with_redacted_logs() {
    let dir = tempfile::tempdir().unwrap();
    let mut s = stack().await;
    s.spawn_agent("web", &["web"], log_file(dir.path(), "web"));
    s.spawn_agent("db", &["db"], log_file(dir.path(), "db"));
    s.wait_for_agents(2).await;

    s.backend.ask(logs_within("q1", 10)).await;
    let Event::Result(r) = s.backend.next().await else {
        panic!("expected an aggregate result");
    };

    let ids: Vec<_> = r.agents.iter().map(|a| a.agent_id.as_str()).collect();
    assert_eq!(ids, ["db", "web"]);
    for (section, service) in r.agents.iter().zip(["db", "web"]) {
        assert_eq!(section.status, AgentStatus::Ok as i32, "{}", section.detail);
        assert_eq!(section.source, AgentSource::File as i32);
        let workload = &section.result.as_ref().unwrap().workloads[0];
        assert_eq!(workload.workload.as_ref().unwrap().name, service);
        assert_eq!(
            workload.entries[0].message,
            format!("1970-01-01T00:01:41Z {service} says hi password=[REDACTED]")
        );
    }
    assert!(r.warnings.is_empty());
}

#[tokio::test]
async fn a_stuck_agent_times_out_and_the_others_still_answer() {
    let dir = tempfile::tempdir().unwrap();
    let mut s = stack().await;
    s.spawn_agent("web", &["web"], log_file(dir.path(), "web"));
    s.spawn_agent("stuck", &["stuck"], stuck());
    s.wait_for_agents(2).await;

    let started = Instant::now();
    s.backend.ask(logs_within("q1", 2)).await;
    let Event::Result(r) = s.backend.next().await else {
        panic!("expected an aggregate result");
    };
    assert!(
        started.elapsed() < Duration::from_secs(2),
        "answered after the backend deadline"
    );

    let status = |id: &str| r.agents.iter().find(|a| a.agent_id == id).unwrap().status;
    assert_eq!(status("web"), AgentStatus::Ok as i32);
    assert_eq!(status("stuck"), AgentStatus::Timeout as i32);

    // the stuck one was cancelled, nobody is wedged: ask again, same shape
    s.backend.ask(logs_within("q2", 2)).await;
    let Event::Result(r) = s.backend.next().await else {
        panic!("expected a second aggregate result");
    };
    assert_eq!(r.agents.len(), 2);
}

#[tokio::test]
async fn an_agent_killed_mid_query_shows_up_as_disconnected() {
    let dir = tempfile::tempdir().unwrap();
    let mut s = stack().await;
    s.spawn_agent("web", &["web"], log_file(dir.path(), "web"));
    let doomed = s.spawn_agent("doomed", &["doomed"], stuck());
    s.wait_for_agents(2).await;

    let started = Instant::now();
    s.backend.ask(logs_within("q1", 20)).await;
    tokio::time::sleep(Duration::from_millis(400)).await;
    doomed.abort();

    let Event::Result(r) = s.backend.next().await else {
        panic!("expected an aggregate result");
    };
    assert!(
        started.elapsed() < Duration::from_secs(5),
        "should not wait for the deadline"
    );
    let status = |id: &str| r.agents.iter().find(|a| a.agent_id == id).unwrap().status;
    assert_eq!(status("web"), AgentStatus::Ok as i32);
    assert_eq!(status("doomed"), AgentStatus::Disconnected as i32);
    s.wait_for_agents(1).await;
}
