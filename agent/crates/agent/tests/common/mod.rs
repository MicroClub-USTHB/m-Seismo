// NOTE: each test crate uses a different subset of this
#![allow(dead_code)]

//! Tiny canned-response HTTP server, enough to stand in for Loki.
use std::{
    sync::{Arc, Mutex},
    time::Duration,
};
use tokio::{
    io::{AsyncReadExt, AsyncWriteExt},
    net::TcpListener,
};

pub struct Fake {
    pub url: String,
    /// Raw request heads (request line + headers), in arrival order.
    pub requests: Arc<Mutex<Vec<String>>>,
}

/// `handler` gets the request target (path + query) and returns (status, body).
pub async fn serve(
    delay: Duration,
    handler: impl Fn(&str) -> (u16, String) + Send + Sync + 'static,
) -> Fake {
    let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
    let url = format!("http://{}", listener.local_addr().unwrap());
    let requests = Arc::new(Mutex::new(Vec::new()));
    let (log, handler) = (requests.clone(), Arc::new(handler));
    tokio::spawn(async move {
        loop {
            let (mut sock, _) = listener.accept().await.unwrap();
            let (log, handler) = (log.clone(), handler.clone());
            tokio::spawn(async move {
                let mut buf = Vec::new();
                let mut chunk = [0u8; 1024];
                while !buf.windows(4).any(|w| w == b"\r\n\r\n") {
                    match sock.read(&mut chunk).await {
                        Ok(0) | Err(_) => return,
                        Ok(n) => buf.extend_from_slice(&chunk[..n]),
                    }
                }
                let head = String::from_utf8_lossy(&buf).into_owned();
                let target = head.split_whitespace().nth(1).unwrap_or("").to_owned();
                log.lock().unwrap().push(head);
                tokio::time::sleep(delay).await;
                let (status, body) = handler(&target);
                let resp = format!(
                    "HTTP/1.1 {status} X\r\ncontent-type: application/json\r\ncontent-length: {}\r\nconnection: close\r\n\r\n{body}",
                    body.len()
                );
                let _ = sock.write_all(resp.as_bytes()).await;
            });
        }
    });
    Fake { url, requests }
}

/// Loki `streams` response from (labels, [(ts_ns, line)]) pairs.
pub fn streams(s: &[(serde_json::Value, &[(&str, &str)])]) -> String {
    let result: Vec<_> = s
        .iter()
        .map(|(labels, vals)| {
            serde_json::json!({
                "stream": labels,
                "values": vals.iter().map(|(t, l)| serde_json::json!([t, l])).collect::<Vec<_>>(),
            })
        })
        .collect();
    serde_json::json!({"status": "success", "data": {"resultType": "streams", "result": result}})
        .to_string()
}
