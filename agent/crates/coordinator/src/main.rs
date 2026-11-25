use anyhow::{Context, Result, anyhow, bail};
use seismo_coordinator::{
    agents, backend, config::Config, logstats, prometheus, registry::Registry,
};
use std::sync::Arc;
use tokio::net::TcpListener;
use tonic::transport::{Identity, ServerTlsConfig};
use tracing::info;
use tracing_subscriber::{EnvFilter, prelude::*};

#[tokio::main]
async fn main() -> Result<()> {
    tracing_subscriber::registry()
        .with(EnvFilter::try_from_default_env().unwrap_or_else(|_| "info".into()))
        .with(tracing_subscriber::fmt::layer().with_writer(std::io::stderr))
        .with(logstats::LogStats)
        .init();

    let Some(path) = std::env::args()
        .nth(1)
        .or(std::env::var("SEISMO_COORDINATOR_CONFIG").ok())
    else {
        bail!("usage: seismo-coordinator <config.toml> (or set SEISMO_COORDINATOR_CONFIG)");
    };
    let cfg = Config::load(&path)
        .map_err(|e| anyhow!(e))
        .context("config")?;
    let registry = Arc::new(Registry::new(cfg.coordinator_id.clone(), cfg.query));
    let listener = TcpListener::bind(&cfg.listen).await.context("listen")?;
    if let Some(m) = cfg.metrics.clone() {
        let l = TcpListener::bind(&m.listen)
            .await
            .context("metrics listen")?;
        info!("prometheus metrics on {}", m.listen);
        tokio::spawn(prometheus::serve(l, registry.clone(), m));
    }
    let tls = cfg
        .agents
        .tls
        .as_ref()
        .map(|t| -> Result<_> {
            let read = |p: &str| std::fs::read(p).with_context(|| format!("tls: read {p}"));
            Ok(ServerTlsConfig::new()
                .identity(Identity::from_pem(read(&t.cert_file)?, read(&t.key_file)?)))
        })
        .transpose()?;
    info!(
        "coordinator {} listening on {}, backend {}",
        cfg.coordinator_id, cfg.listen, cfg.backend.url
    );

    tokio::select! {
        r = agents::serve(listener, registry.clone(), cfg.agents.token.clone(), tls) => {
            r.context("agent server stopped")?;
            bail!("agent server stopped: closed")
        }
        never = backend::run(&cfg.backend, &cfg.coordinator_id, registry) => never,
    }
}
