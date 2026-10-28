use serde::Deserialize;

pub const DEFAULT_TOKEN_ENV: &str = "SEISMO_AGENT_TOKEN";
pub const DEFAULT_KEY_ENV: &str = "SEISMO_COORDINATOR_KEY";

// NOTE: these are the bounds from the protocol doc (section 3.3 and 6)
const MAX_LINES: u32 = 10_000;
const MAX_CONTEXT: u32 = 100;
const MIN_BUDGET: u32 = 1 << 10;
const MAX_BUDGET: u32 = 8 << 20;

#[derive(Debug, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct Config {
    /// Sent in `Hello`. Falls back to the hostname.
    #[serde(default)]
    pub coordinator_id: String,
    /// Where agents dial in.
    #[serde(default = "default_listen")]
    pub listen: String,
    pub agents: Agents,
    pub backend: Backend,
    #[serde(default)]
    pub query: Query,
    /// Prometheus endpoint, off when the section is absent.
    pub metrics: Option<Metrics>,
}

#[derive(Debug, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct Agents {
    /// Env var holding the registration token every agent presents.
    #[serde(default = "default_token_env")]
    pub token_env: String,
    #[serde(skip)]
    pub token: String,
    /// Serve TLS when set, plaintext otherwise.
    pub tls: Option<Tls>,
}

#[derive(Debug, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct Tls {
    /// PEM, full chain.
    pub cert_file: String,
    pub key_file: String,
}

#[derive(Debug, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct Backend {
    /// Where we dial the backend's `Coordinator.Attach`.
    pub url: String,
    /// Env var holding the `mco_...` key of the app.
    #[serde(default = "default_key_env")]
    pub key_env: String,
    #[serde(skip)]
    pub key: String,
}

/// What we ask of the agents. `QueryLogs` doesn't carry any of it.
#[derive(Debug, Clone, Copy, Deserialize)]
#[serde(deny_unknown_fields, default)]
pub struct Query {
    pub max_lines_per_workload: u32,
    pub context_lines: u32,
    /// Log bytes for the whole aggregate, split evenly between agents.
    pub total_budget_bytes: u32,
    /// Kept back from the backend's deadline to aggregate and reply.
    pub margin_ms: u64,
}

impl Default for Query {
    fn default() -> Self {
        Self {
            max_lines_per_workload: 1000,
            context_lines: 5,
            total_budget_bytes: MAX_BUDGET,
            margin_ms: 500,
        }
    }
}

/// Where Prometheus scrapes us. Every scrape asks the agents for counts over
/// the last `window_secs`.
#[derive(Debug, Clone, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct Metrics {
    pub listen: String,
    #[serde(default = "default_window_secs")]
    pub window_secs: u64,
    /// How long a scrape waits for slow agents.
    #[serde(default = "default_scrape_timeout_ms")]
    pub timeout_ms: u64,
}

fn default_window_secs() -> u64 {
    60
}

fn default_scrape_timeout_ms() -> u64 {
    5000
}

fn default_listen() -> String {
    "[::]:50051".into()
}

fn default_token_env() -> String {
    DEFAULT_TOKEN_ENV.into()
}

fn default_key_env() -> String {
    DEFAULT_KEY_ENV.into()
}

impl Config {
    pub fn load(path: &str) -> Result<Self, String> {
        let text = std::fs::read_to_string(path).map_err(|e| format!("read {path}: {e}"))?;
        Self::parse(&text, |k| std::env::var(k).ok())
    }

    /// `env` is injected so tests don't have to poke the process environment.
    pub fn parse(text: &str, env: impl Fn(&str) -> Option<String>) -> Result<Self, String> {
        let mut cfg: Config = toml::from_str(text).map_err(|e| e.to_string())?;
        let secret = |name: &str| {
            env(name)
                .filter(|v| !v.is_empty())
                .ok_or(format!("env var {name} is not set"))
        };

        if cfg.coordinator_id.is_empty() {
            cfg.coordinator_id = hostname::get()
                .ok()
                .and_then(|h| h.into_string().ok())
                .ok_or("coordinator_id is unset and the hostname is unreadable")?;
        }
        cfg.listen
            .parse::<std::net::SocketAddr>()
            .map_err(|e| format!("listen: {e}"))?;
        cfg.agents.token = secret(&cfg.agents.token_env)?;
        cfg.backend.key = secret(&cfg.backend.key_env)?;
        // msk_ SDK keys are never accepted on this hop, catch the mix-up early
        if !cfg.backend.key.starts_with("mco_") {
            return Err("backend key must be an mco_ key".into());
        }

        if let Some(m) = &cfg.metrics {
            m.listen
                .parse::<std::net::SocketAddr>()
                .map_err(|e| format!("metrics.listen: {e}"))?;
            if m.window_secs == 0 {
                return Err("metrics.window_secs must be > 0".into());
            }
        }
        let q = &cfg.query;
        let ok = (1..=MAX_LINES).contains(&q.max_lines_per_workload)
            && q.context_lines <= MAX_CONTEXT
            && (MIN_BUDGET..=MAX_BUDGET).contains(&q.total_budget_bytes);
        if !ok {
            return Err(format!(
                "query limits out of range: lines 1..={MAX_LINES}, context 0..={MAX_CONTEXT}, \
                 budget {MIN_BUDGET}..={MAX_BUDGET}"
            ));
        }
        Ok(cfg)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn env(k: &str) -> Option<String> {
        match k {
            DEFAULT_TOKEN_ENV => Some("tok".into()),
            DEFAULT_KEY_ENV => Some("mco_abc".into()),
            _ => None,
        }
    }

    const MIN: &str = r#"
        coordinator_id = "prod"
        [agents]
        [backend]
        url = "http://backend:9090"
    "#;

    #[test]
    fn minimal_gets_defaults() {
        let c = Config::parse(MIN, env).unwrap();
        assert_eq!(c.coordinator_id, "prod");
        assert_eq!(c.listen, "[::]:50051");
        assert_eq!(c.agents.token, "tok");
        assert_eq!(c.backend.key, "mco_abc");
        assert_eq!(c.query.max_lines_per_workload, 1000);
        assert_eq!(c.query.total_budget_bytes, 8 << 20);
    }

    #[test]
    fn empty_id_falls_back_to_hostname() {
        let c = Config::parse(&MIN.replace("coordinator_id = \"prod\"", ""), env).unwrap();
        assert!(!c.coordinator_id.is_empty());
    }

    #[test]
    fn query_section_overrides_single_fields() {
        let c = Config::parse(
            &format!("{MIN}\n[query]\ncontext_lines = 0\nmargin_ms = 100"),
            env,
        )
        .unwrap();
        assert_eq!((c.query.context_lines, c.query.margin_ms), (0, 100));
        assert_eq!(
            c.query.max_lines_per_workload, 1000,
            "untouched fields keep defaults"
        );
    }

    #[test]
    fn rejects_bad_input() {
        for (text, why) in [
            (
                MIN.replace("[agents]", "[agents]\nbogus = 1"),
                "unknown field",
            ),
            (
                format!("{MIN}\n[query]\nmax_lines_per_workload = 0"),
                "no lines",
            ),
            (
                format!("{MIN}\n[query]\nmax_lines_per_workload = 10001"),
                "too many lines",
            ),
            (
                format!("{MIN}\n[query]\ncontext_lines = 101"),
                "too much context",
            ),
            (
                format!("{MIN}\n[query]\ntotal_budget_bytes = 100"),
                "budget below 1KiB",
            ),
            (
                format!("{MIN}\n[query]\ntotal_budget_bytes = 9000000"),
                "budget above 8MiB",
            ),
            (format!("listen = \"nope\"\n{MIN}"), "bad listen address"),
        ] {
            assert!(Config::parse(&text, env).is_err(), "{why}");
        }
        assert!(Config::parse(MIN, |_| None).is_err(), "unset secrets");
        let sdk_key = |k: &str| {
            (k == DEFAULT_KEY_ENV)
                .then(|| "msk_sdk".to_owned())
                .or_else(|| env(k))
        };
        assert!(
            Config::parse(MIN, sdk_key).is_err(),
            "msk_ key on the coordinator hop"
        );
    }
}
