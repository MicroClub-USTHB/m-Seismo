//! Runtime-neutral configuration shared by source adapters.

use std::collections::BTreeMap;

use serde::{Deserialize, Serialize};

use crate::model::QueryLimits;

pub const DEFAULT_QUERY_DEADLINE_MS: u64 = 2_000;
pub const DEFAULT_WORKLOAD_TIMEOUT_MS: u64 = 1_000;
pub const MAX_QUERY_DEADLINE_MS: u64 = 30_000;

#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub struct AgentConfig {
    /// Optional runtime endpoint. When omitted, the adapter chooses its local
    /// platform default; this keeps host-specific paths out of shared code.
    pub endpoint: Option<String>,
    pub scope: WorkloadScope,
    pub limits: QueryLimits,
    pub query_deadline_ms: u64,
    pub workload_timeout_ms: u64,
}

#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case", tag = "mode")]
pub enum WorkloadScope {
    All,
    Selectors(WorkloadSelectors),
}

#[derive(Clone, Debug, Default, PartialEq, Eq, Serialize, Deserialize)]
pub struct WorkloadSelectors {
    pub names: Vec<String>,
    pub labels: BTreeMap<String, String>,
    pub projects: Vec<String>,
}

impl Default for AgentConfig {
    fn default() -> Self {
        Self {
            endpoint: None,
            scope: WorkloadScope::All,
            limits: QueryLimits {
                max_lines_per_workload: 1_000,
                max_bytes_total: 1024 * 1024,
                context_lines: 5,
            },
            query_deadline_ms: DEFAULT_QUERY_DEADLINE_MS,
            workload_timeout_ms: DEFAULT_WORKLOAD_TIMEOUT_MS,
        }
    }
}

impl AgentConfig {
    pub fn validate(&self) -> Result<(), ConfigError> {
        self.limits
            .validate()
            .map_err(|_| ConfigError::InvalidLimits)?;
        if self.query_deadline_ms == 0 || self.query_deadline_ms > MAX_QUERY_DEADLINE_MS {
            return Err(ConfigError::InvalidDeadline);
        }
        if self.workload_timeout_ms == 0 || self.workload_timeout_ms > self.query_deadline_ms {
            return Err(ConfigError::InvalidWorkloadTimeout);
        }
        if let Some(endpoint) = &self.endpoint
            && (endpoint.trim().is_empty()
                || endpoint.len() > 2_048
                || endpoint.chars().any(char::is_control))
        {
            return Err(ConfigError::InvalidEndpoint);
        }
        if let WorkloadScope::Selectors(selectors) = &self.scope {
            for selector in selectors
                .names
                .iter()
                .chain(selectors.projects.iter())
                .chain(
                    selectors
                        .labels
                        .iter()
                        .flat_map(|(key, value)| [key, value]),
                )
            {
                if selector.trim().is_empty()
                    || selector.len() > 256
                    || selector.chars().any(char::is_control)
                {
                    return Err(ConfigError::InvalidSelector);
                }
            }
            if selectors.names.is_empty()
                && selectors.projects.is_empty()
                && selectors.labels.is_empty()
            {
                return Err(ConfigError::EmptyScope);
            }
        }
        Ok(())
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum ConfigError {
    InvalidEndpoint,
    InvalidDeadline,
    InvalidWorkloadTimeout,
    InvalidLimits,
    EmptyScope,
    InvalidSelector,
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn defaults_to_all_workloads_on_the_configured_endpoint() {
        let config = AgentConfig::default();
        assert_eq!(config.scope, WorkloadScope::All);
        assert_eq!(config.endpoint, None);
        assert_eq!(config.validate(), Ok(()));
    }

    #[test]
    fn accepts_name_label_and_project_scope_selectors() {
        let config = AgentConfig {
            scope: WorkloadScope::Selectors(WorkloadSelectors {
                names: vec!["api".to_owned()],
                labels: BTreeMap::from([("team".to_owned(), "payments".to_owned())]),
                projects: vec!["demo".to_owned()],
            }),
            ..AgentConfig::default()
        };
        assert_eq!(config.validate(), Ok(()));
    }

    #[test]
    fn rejects_empty_scope_and_invalid_deadline() {
        let mut config = AgentConfig {
            scope: WorkloadScope::Selectors(WorkloadSelectors::default()),
            ..AgentConfig::default()
        };
        assert_eq!(config.validate(), Err(ConfigError::EmptyScope));
        config.scope = WorkloadScope::All;
        config.query_deadline_ms = 0;
        assert_eq!(config.validate(), Err(ConfigError::InvalidDeadline));
    }

    #[test]
    fn rejects_workload_timeout_longer_than_query_deadline() {
        let config = AgentConfig {
            workload_timeout_ms: DEFAULT_QUERY_DEADLINE_MS + 1,
            ..AgentConfig::default()
        };
        assert_eq!(config.validate(), Err(ConfigError::InvalidWorkloadTimeout));
    }

    #[test]
    fn rejects_blank_endpoint_and_control_characters_in_selectors() {
        let mut config = AgentConfig {
            endpoint: Some("  ".to_owned()),
            ..AgentConfig::default()
        };
        assert_eq!(config.validate(), Err(ConfigError::InvalidEndpoint));
        config.endpoint = None;
        config.scope = WorkloadScope::Selectors(WorkloadSelectors {
            names: vec!["api\nworker".to_owned()],
            ..WorkloadSelectors::default()
        });
        assert_eq!(config.validate(), Err(ConfigError::InvalidSelector));
    }
}
