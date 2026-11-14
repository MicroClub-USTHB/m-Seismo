//! An agent reaches a TLS coordinator only when it trusts the cert.
use seismo_agent::{config as agent_cfg, engine::Engine, session, source::Source};
use seismo_coordinator::{agents, config, registry::Registry};
use std::{collections::HashMap, sync::Arc, time::Duration};
use tokio::net::TcpListener;
use tonic::transport::{Identity, ServerTlsConfig};

fn agent(url: &str, ca_pem: Option<Vec<u8>>) -> tokio::task::JoinHandle<()> {
    let source = agent_cfg::Source::File(agent_cfg::File {
        files: HashMap::from([("db".to_owned(), "/dev/null".into())]),
        rotated: 0,
    });
    let cfg = agent_cfg::Config {
        agent_id: "a".into(),
        services: vec!["db".into()],
        coordinator: agent_cfg::Coordinator {
            url: url.into(),
            token_env: String::new(),
            token: "tok".into(),
            ca_file: None,
            ca_pem,
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

#[tokio::test]
async fn agent_registers_over_tls_only_with_the_right_ca() {
    let cert = rcgen::generate_simple_self_signed(vec!["localhost".into()]).unwrap();
    let tls = ServerTlsConfig::new().identity(Identity::from_pem(
        cert.cert.pem(),
        cert.signing_key.serialize_pem(),
    ));
    let registry = Arc::new(Registry::new("coord".into(), config::Query::default()));
    let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
    let url = format!(
        "https://localhost:{}",
        listener.local_addr().unwrap().port()
    );
    tokio::spawn(agents::serve(
        listener,
        registry.clone(),
        "tok".into(),
        Some(tls),
    ));

    // no CA: the self-signed cert must be rejected
    let untrusting = agent(&url, None);
    tokio::time::sleep(Duration::from_millis(500)).await;
    assert_eq!(registry.len(), 0);
    untrusting.abort();

    let _trusting = agent(&url, Some(cert.cert.pem().into_bytes()));
    for _ in 0..100 {
        if registry.len() == 1 {
            return;
        }
        tokio::time::sleep(Duration::from_millis(50)).await;
    }
    panic!("agent never registered over tls");
}
