use seismo_agent::{config::Config, engine::Engine, session, source::Source};
use std::sync::Arc;

fn die(what: &str, e: String) -> ! {
    eprintln!("{what}: {e}");
    std::process::exit(2);
}

#[tokio::main]
async fn main() {
    let Some(path) = std::env::args()
        .nth(1)
        .or(std::env::var("SEISMO_AGENT_CONFIG").ok())
    else {
        eprintln!("usage: seismo-agent <config.toml> (or set SEISMO_AGENT_CONFIG)");
        std::process::exit(2);
    };
    let cfg = Config::load(&path).unwrap_or_else(|e| die("config", e));
    let source = Source::build(&cfg.source).unwrap_or_else(|e| die("log source", e));
    let engine = Arc::new(Engine::new(cfg.services.clone(), source));
    eprintln!("agent {} serving {:?}", cfg.agent_id, cfg.services);
    session::run(&cfg, engine).await
}
