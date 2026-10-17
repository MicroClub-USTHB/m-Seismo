use std::process::Command;

use chrono::{Duration, Utc};

#[test]
#[ignore = "requires a reachable Docker Engine and permission to query it"]
fn cli_queries_configured_docker_engine_and_returns_json() {
    let now = Utc::now();
    let from = (now - Duration::minutes(5)).to_rfc3339();
    let to = now.to_rfc3339();
    let output = Command::new(env!("CARGO_BIN_EXE_m-seismo-agent"))
        .args([
            "query",
            "--source",
            "docker",
            "--from",
            from.as_str(),
            "--to",
            to.as_str(),
        ])
        .output()
        .expect("spawn agent CLI");

    assert!(
        output.status.success(),
        "agent CLI failed: {}",
        String::from_utf8_lossy(&output.stderr)
    );
    let result: serde_json::Value =
        serde_json::from_slice(&output.stdout).expect("CLI should print a JSON query result");
    assert_eq!(result["schema_version"], 1);
    assert!(result["query_id"].as_str().is_some());
    assert!(result["workloads"].is_array());
    assert_ne!(result["status"], "failed", "query result: {result}");
}
