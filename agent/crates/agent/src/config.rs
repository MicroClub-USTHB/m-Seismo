use serde::Deserialize;

pub const DEFAULT_TOKEN_ENV: &str = "SEISMO_AGENT_TOKEN";

#[derive(Debug, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct Config {
    /// Falls back to the hostname, must stay stable across restarts.
    #[serde(default)]
    pub agent_id: String,
    /// Service names this agent is responsible for, each is fetched on its own.
    pub services: Vec<String>,
    pub coordinator: Coordinator,
    pub source: Source,
}

#[derive(Debug, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct Coordinator {
    pub url: String,
    #[serde(default = "default_token_env")]
    pub token_env: String,
    #[serde(skip)]
    pub token: String,
}

/// Where this agent reads its logs from. Pick one with `type`.
#[derive(Debug, Clone, Deserialize)]
#[serde(tag = "type", rename_all = "snake_case")]
pub enum Source {
    Loki(Loki),
}

#[derive(Debug, Clone, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct Loki {
    pub url: String,
    /// Label holding the service name, `services` are matched against it.
    #[serde(default = "default_service_label")]
    pub service_label: String,
    /// Label copied into `Workload.scope_label` (namespace, host...).
    pub scope_label: Option<String>,
    /// Sent as `X-Scope-OrgID`.
    pub tenant: Option<String>,
    /// Structured metadata field carrying the trace id. Empty string means
    /// no such field, fall back to a plain line filter.
    #[serde(default = "default_trace_field")]
    pub trace_field: String,
    #[serde(default)]
    pub auth: Auth,
}

/// Secrets never live in the file, only the env var names do.
#[derive(Debug, Clone, Default, Deserialize)]
#[serde(tag = "type", rename_all = "snake_case", deny_unknown_fields)]
pub enum Auth {
    #[default]
    None,
    Basic {
        username: String,
        password_env: String,
        #[serde(skip)]
        password: String,
    },
    Bearer {
        token_env: String,
        #[serde(skip)]
        token: String,
    },
}

fn default_token_env() -> String {
    DEFAULT_TOKEN_ENV.into()
}

fn default_service_label() -> String {
    "service_name".into()
}

fn default_trace_field() -> String {
    "trace_id".into()
}

impl Config {
    pub fn load(path: &str) -> Result<Self, String> {
        let text = std::fs::read_to_string(path).map_err(|e| format!("read {path}: {e}"))?;
        Self::parse(&text, |k| std::env::var(k).ok())
    }

    /// `env` is injected so tests don't have to poke the process environment.
    pub fn parse(text: &str, env: impl Fn(&str) -> Option<String>) -> Result<Self, String> {
        let mut cfg: Config = toml::from_str(text).map_err(|e| e.to_string())?;
        let secret = |name: &str| env(name).ok_or(format!("env var {name} is not set"));

        if cfg.services.is_empty() || cfg.services.iter().any(|s| s.is_empty()) {
            return Err("services must be a list of non-empty names".into());
        }
        if cfg.agent_id.is_empty() {
            cfg.agent_id = hostname::get()
                .ok()
                .and_then(|h| h.into_string().ok())
                .ok_or("agent_id is unset and the hostname is unreadable")?;
        }
        cfg.coordinator.token = secret(&cfg.coordinator.token_env)?;
        match &mut cfg.source {
            Source::Loki(l) => match &mut l.auth {
                Auth::None => {}
                Auth::Basic {
                    password_env,
                    password,
                    ..
                } => *password = secret(password_env)?,
                Auth::Bearer { token_env, token } => *token = secret(token_env)?,
            },
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
            "LOKI_PW" => Some("hunter2".into()),
            _ => None,
        }
    }

    const MIN: &str = r#"
        agent_id = "db"
        services = ["postgres"]
        [coordinator]
        url = "http://c:50051"
        [source]
        type = "loki"
        url = "http://loki:3100"
    "#;

    #[test]
    fn minimal_gets_defaults() {
        let c = Config::parse(MIN, env).unwrap();
        assert_eq!(c.agent_id, "db");
        assert_eq!(c.coordinator.token, "tok");
        let Source::Loki(l) = c.source;
        assert_eq!(l.service_label, "service_name");
        assert_eq!(l.trace_field, "trace_id");
        assert!(matches!(l.auth, Auth::None));
    }

    #[test]
    fn empty_agent_id_falls_back_to_hostname() {
        let c = Config::parse(&MIN.replace("agent_id = \"db\"", ""), env).unwrap();
        assert!(!c.agent_id.is_empty());
    }

    #[test]
    fn basic_auth_reads_password_from_env() {
        let text = format!(
            "{MIN}\n[source.auth]\ntype = \"basic\"\nusername = \"u\"\npassword_env = \"LOKI_PW\""
        );
        let Source::Loki(l) = Config::parse(&text, env).unwrap().source;
        match l.auth {
            Auth::Basic { password, .. } => assert_eq!(password, "hunter2"),
            other => panic!("{other:?}"),
        }
    }

    #[test]
    fn rejects_bad_input() {
        let missing_secret =
            format!("{MIN}\n[source.auth]\ntype = \"bearer\"\ntoken_env = \"NOPE\"");
        for (text, why) in [
            (MIN.replace("[\"postgres\"]", "[]"), "no services"),
            (MIN.replace("[\"postgres\"]", "[\"\"]"), "empty service"),
            (
                MIN.replace("type = \"loki\"", "type = \"loki\"\nbogus = 1"),
                "unknown field",
            ),
            (missing_secret, "unset secret env"),
        ] {
            assert!(Config::parse(&text, env).is_err(), "{why}");
        }
        assert!(Config::parse(MIN, |_| None).is_err(), "unset token env");
    }
}
