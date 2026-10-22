use seismo_agent::{config::Config, engine::Engine, loki::Loki, session};
use std::sync::Arc;

#[tokio::main]
async fn main() {
    let Some(path) = std::env::args()
        .nth(1)
        .or(std::env::var("SEISMO_AGENT_CONFIG").ok())
    else {
        eprintln!("usage: seismo-agent <config.toml> (or set SEISMO_AGENT_CONFIG)");
        std::process::exit(2);
    };
    let cfg = Config::load(&path).unwrap_or_else(|e| {
        eprintln!("config: {e}");
        std::process::exit(2);
    });
    let loki = Loki::new(cfg.loki.clone()).unwrap_or_else(|e| {
        eprintln!("loki client: {e}");
        std::process::exit(2);
    });
    let engine = Arc::new(Engine::new(cfg.services.clone(), loki));
    eprintln!("agent {} serving {:?}", cfg.agent_id, cfg.services);
    session::run(&cfg, engine).await
}
