use seismo_coordinator::{agents, backend, config::Config, registry::Registry};
use std::sync::Arc;
use tokio::net::TcpListener;

fn die(what: &str, e: impl std::fmt::Display) -> ! {
    eprintln!("{what}: {e}");
    std::process::exit(2);
}

#[tokio::main]
async fn main() {
    let Some(path) = std::env::args()
        .nth(1)
        .or(std::env::var("SEISMO_COORDINATOR_CONFIG").ok())
    else {
        eprintln!("usage: seismo-coordinator <config.toml> (or set SEISMO_COORDINATOR_CONFIG)");
        std::process::exit(2);
    };
    let cfg = Config::load(&path).unwrap_or_else(|e| die("config", e));
    let registry = Arc::new(Registry::new(cfg.coordinator_id.clone(), cfg.query));
    let listener = TcpListener::bind(&cfg.listen)
        .await
        .unwrap_or_else(|e| die("listen", e));
    eprintln!(
        "coordinator {} listening on {}, backend {}",
        cfg.coordinator_id, cfg.listen, cfg.backend.url
    );

    tokio::select! {
        r = agents::serve(listener, registry.clone(), cfg.agents.token.clone()) => {
            die("agent server stopped", r.err().map_or("closed".into(), |e| e.to_string()))
        }
        never = backend::run(&cfg.backend, &cfg.coordinator_id, registry) => never,
    }
}
