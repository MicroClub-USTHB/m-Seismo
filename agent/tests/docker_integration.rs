use std::process::Command;
use std::time::{Duration as StdDuration, SystemTime, UNIX_EPOCH};

use chrono::{Duration, Utc};

struct TestContainer {
    name: String,
}

impl Drop for TestContainer {
    fn drop(&mut self) {
        let _ = Command::new("docker")
            .args(["rm", "--force", &self.name])
            .output();
    }
}

#[test]
#[ignore = "requires a reachable Docker Engine and permission to query it"]
fn cli_queries_configured_docker_engine_and_returns_json() {
    let suffix = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .expect("system clock is before Unix epoch")
        .as_nanos();
    let container_name = format!("m-seismo-agent-test-{suffix}");
    let marker = format!("m-seismo integration marker {suffix}");
    let container = TestContainer {
        name: container_name.clone(),
    };

    let create = Command::new("docker")
        .args([
            "run",
            "--detach",
            "--name",
            &container_name,
            "alpine:latest",
            "sh",
            "-c",
            &format!("printf '%s\\n' '{marker}'; sleep 30"),
        ])
        .output()
        .expect("spawn Docker container");
    assert!(
        create.status.success(),
        "could not create test container: {}",
        String::from_utf8_lossy(&create.stderr)
    );

    std::thread::sleep(StdDuration::from_millis(250));
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
            "--workload",
            &container_name,
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
    assert_eq!(result["workloads"].as_array().unwrap().len(), 1);
    assert_eq!(result["workloads"][0]["workload"]["name"], container_name);
    assert!(
        result["workloads"][0]["entries"]
            .as_array()
            .unwrap()
            .iter()
            .any(|entry| entry["message"].as_str() == Some(marker.as_str())),
        "test container log marker was not returned: {result}"
    );

    drop(container);
}
