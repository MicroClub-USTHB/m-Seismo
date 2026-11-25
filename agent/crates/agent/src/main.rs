use anyhow::{Context, Result, anyhow, bail};
use seismo_agent::{config::Config, engine::Engine, session, source::Source};
use std::sync::Arc;
use tracing::info;
use tracing_subscriber::EnvFilter;

#[tokio::main]
async fn main() -> Result<()> {
    tracing_subscriber::fmt()
        .with_env_filter(EnvFilter::try_from_default_env().unwrap_or_else(|_| "info".into()))
        .with_writer(std::io::stderr)
        .init();

    let Some(path) = std::env::args()
        .nth(1)
        .or(std::env::var("SEISMO_AGENT_CONFIG").ok())
    else {
        bail!("usage: seismo-agent <config.toml> (or set SEISMO_AGENT_CONFIG)");
    };
    let cfg = Config::load(&path)
        .map_err(|e| anyhow!(e))
        .context("config")?;
    let source = Source::build(&cfg.source)
        .map_err(|e| anyhow!(e))
        .context("log source")?;
    let engine = Arc::new(Engine::new(cfg.services.clone(), source));
    info!("agent {} serving {:?}", cfg.agent_id, cfg.services);
    session::run(&cfg, engine).await
}
