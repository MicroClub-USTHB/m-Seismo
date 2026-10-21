//! Docker Engine API adapter. Docker-specific API types stay in this module.

mod stream;

use std::{
    collections::{BTreeMap, HashMap, HashSet},
    error::Error as StdError,
    io,
};

use bollard::{
    API_DEFAULT_VERSION, Docker,
    errors::Error as DockerError,
    query_parameters::{ListContainersOptionsBuilder, LogsOptionsBuilder},
};
use chrono::{DateTime, Utc};
use futures_util::StreamExt;

use crate::{
    config::{AgentConfig, WorkloadScope, WorkloadSelectors},
    model::{
        CoverageStatus, DiagnosticValue, RawLogs, ReadLimits, SourceError, SourceErrorKind,
        TimeWindow, Workload, WorkloadDiagnostics,
    },
    source::LogSource,
};

use self::stream::LineDecoder;

const DOCKER_REQUEST_TIMEOUT_SECS: u64 = 30;
const COMPOSE_PROJECT_LABEL: &str = "com.docker.compose.project";

#[derive(Clone)]
pub struct DockerSource {
    client: Docker,
}

impl DockerSource {
    pub fn connect(config: &AgentConfig) -> Result<Self, SourceError> {
        let client = match config.endpoint.as_deref() {
            None => Docker::connect_with_socket_defaults(),
            Some(endpoint) => {
                let path = endpoint.strip_prefix("unix://").unwrap_or(endpoint);
                if path.is_empty() || path.contains("://") {
                    return Err(SourceError {
                        kind: SourceErrorKind::InvalidResponse,
                        detail: "Docker endpoint must be a Unix socket path or unix:// URL".into(),
                    });
                }
                Docker::connect_with_socket(path, DOCKER_REQUEST_TIMEOUT_SECS, API_DEFAULT_VERSION)
            }
        }
        .map_err(map_docker_error)?;
        Ok(Self { client })
    }

    pub async fn probe(&self) -> Result<(), SourceError> {
        self.client
            .ping()
            .await
            .map(|_| ())
            .map_err(map_docker_error)
    }

    async fn inspect(
        &self,
        workload: &Workload,
    ) -> Result<bollard::models::ContainerInspectResponse, SourceError> {
        self.client
            .inspect_container(&workload.id, None)
            .await
            .map_err(map_docker_error)
    }

    async fn list_containers(
        &self,
        filters: Option<&HashMap<String, Vec<String>>>,
    ) -> Result<Vec<bollard::models::ContainerSummary>, SourceError> {
        let mut options = ListContainersOptionsBuilder::default().all(true);
        if let Some(filters) = filters {
            options = options.filters(filters);
        }
        self.client
            .list_containers(Some(options.build()))
            .await
            .map_err(map_docker_error)
    }
}

impl LogSource for DockerSource {
    async fn list_workloads(&self, scope: &WorkloadScope) -> Result<Vec<Workload>, SourceError> {
        let filter_groups = docker_filter_groups(scope);
        let mut containers = Vec::new();
        let mut seen_ids = HashSet::new();
        for filters in filter_groups {
            for container in self.list_containers(filters.as_ref()).await? {
                if container
                    .id
                    .as_ref()
                    .is_some_and(|id| seen_ids.insert(id.clone()))
                {
                    containers.push(container);
                }
            }
        }

        let mut workloads = Vec::with_capacity(containers.len());
        for container in containers {
            let Some(workload) = map_workload(container) else {
                return Err(SourceError {
                    kind: SourceErrorKind::InvalidResponse,
                    detail: "Docker returned a container without an identifier".into(),
                });
            };
            if matches_scope(&workload, scope) {
                workloads.push(workload);
            }
        }

        Ok(workloads)
    }

    async fn read_logs(
        &self,
        workload: &Workload,
        window: TimeWindow,
        limits: ReadLimits,
    ) -> Result<RawLogs, SourceError> {
        let inspect = self.inspect(workload).await?;
        let diagnostics = map_diagnostics(&inspect);
        let driver = inspect
            .host_config
            .as_ref()
            .and_then(|host_config| host_config.log_config.as_ref())
            .and_then(|log_config| log_config.typ.as_deref());
        match driver {
            Some("json-file" | "journald") => {}
            Some(_) => {
                return Ok(source_failure(
                    diagnostics,
                    SourceErrorKind::DriverUnsupported,
                    "the container logging driver is not readable through the Docker logs API",
                ));
            }
            None => {
                return Ok(source_failure(
                    diagnostics,
                    SourceErrorKind::DriverUnsupported,
                    "Docker did not report a readable container logging driver",
                ));
            }
        }

        let since = docker_epoch_seconds(window.from, false)?;
        // Docker accepts second-resolution bounds; round the upper bound up
        // and let the shared engine apply exact timestamp filtering.
        let until = docker_epoch_seconds(window.to, true)?;
        let options = LogsOptionsBuilder::default()
            .follow(false)
            .stdout(true)
            .stderr(true)
            .timestamps(true)
            .since(since)
            .until(until)
            .tail("all")
            .build();

        let mut stream = self.client.logs(&workload.id, Some(options));
        let mut decoder = LineDecoder::new(limits);
        let mut stream_error = None;
        while let Some(item) = stream.next().await {
            match item {
                Ok(output) => {
                    let (tag, bytes) = match output {
                        bollard::container::LogOutput::StdOut { message } => {
                            (crate::model::LogStream::Stdout, message)
                        }
                        bollard::container::LogOutput::StdErr { message } => {
                            (crate::model::LogStream::Stderr, message)
                        }
                        bollard::container::LogOutput::Console { message }
                        | bollard::container::LogOutput::StdIn { message } => {
                            (crate::model::LogStream::Unknown, message)
                        }
                    };
                    if !decoder.push(tag, &bytes) {
                        break;
                    }
                }
                Err(error) => {
                    stream_error = Some(map_docker_error(error));
                    break;
                }
            }
        }
        let decoded = decoder.finish();
        let oldest_available = decoded.lines.iter().filter_map(|line| line.timestamp).min();
        let incomplete =
            decoded.truncated || decoded.discarded_line_count > 0 || stream_error.is_some();
        let history_gap = oldest_available.is_some_and(|oldest| oldest > window.from);
        Ok(RawLogs {
            lines: decoded.lines,
            diagnostics,
            coverage: if incomplete || history_gap {
                CoverageStatus::Partial
            } else {
                // The Docker logs API returns the retained range but cannot
                // attest that a logging driver has retained every requested
                // line. Keep this explicit rather than claiming completeness.
                CoverageStatus::Unknown
            },
            oldest_available,
            error: stream_error,
            discarded_line_count: decoded.discarded_line_count,
            truncated: decoded.truncated,
        })
    }
}

fn docker_filter_groups(scope: &WorkloadScope) -> Vec<Option<HashMap<String, Vec<String>>>> {
    let WorkloadScope::Selectors(selectors) = scope else {
        return vec![None];
    };

    let mut groups = Vec::new();
    if !selectors.names.is_empty() {
        groups.push(Some(HashMap::from([(
            "name".to_owned(),
            selectors.names.clone(),
        )])));
    }
    if !selectors.projects.is_empty() {
        groups.push(Some(HashMap::from([(
            "label".to_owned(),
            selectors
                .projects
                .iter()
                .map(|project| format!("{COMPOSE_PROJECT_LABEL}={project}"))
                .collect(),
        )])));
    }
    if !selectors.labels.is_empty() {
        groups.push(Some(HashMap::from([(
            "label".to_owned(),
            selectors
                .labels
                .iter()
                .map(|(key, value)| format!("{key}={value}"))
                .collect(),
        )])));
    }
    groups
}

fn map_diagnostics(inspect: &bollard::models::ContainerInspectResponse) -> WorkloadDiagnostics {
    let unavailable = || "not_reported_by_docker".to_owned();
    WorkloadDiagnostics {
        runtime_state: inspect
            .state
            .as_ref()
            .and_then(|state| state.status.as_ref())
            .map(|state| DiagnosticValue::Available(format!("{state:?}").to_ascii_lowercase()))
            .unwrap_or_else(|| DiagnosticValue::unavailable(unavailable())),
        restart_count: inspect
            .restart_count
            .and_then(|count| u64::try_from(count).ok())
            .map(DiagnosticValue::Available)
            .unwrap_or_else(|| DiagnosticValue::unavailable(unavailable())),
        oom_killed: inspect
            .state
            .as_ref()
            .and_then(|state| state.oom_killed)
            .map(DiagnosticValue::Available)
            .unwrap_or_else(|| DiagnosticValue::unavailable(unavailable())),
        image: inspect
            .image
            .clone()
            .map(DiagnosticValue::Available)
            .unwrap_or_else(|| DiagnosticValue::unavailable(unavailable())),
        configured_cpu_nanocpus: inspect
            .host_config
            .as_ref()
            .and_then(|host_config| host_config.nano_cpus)
            .and_then(|limit| u64::try_from(limit).ok())
            .map(DiagnosticValue::Available)
            .unwrap_or_else(|| DiagnosticValue::unavailable(unavailable())),
        configured_memory_bytes: inspect
            .host_config
            .as_ref()
            .and_then(|host_config| host_config.memory)
            .and_then(|limit| u64::try_from(limit).ok())
            .map(DiagnosticValue::Available)
            .unwrap_or_else(|| DiagnosticValue::unavailable(unavailable())),
    }
}

fn source_failure(
    diagnostics: WorkloadDiagnostics,
    kind: SourceErrorKind,
    detail: &str,
) -> RawLogs {
    RawLogs {
        lines: Vec::new(),
        diagnostics,
        coverage: CoverageStatus::Unknown,
        oldest_available: None,
        error: Some(SourceError {
            kind,
            detail: detail.to_owned(),
        }),
        discarded_line_count: 0,
        truncated: false,
    }
}

fn map_workload(summary: bollard::models::ContainerSummary) -> Option<Workload> {
    let id = summary.id?;
    let name = summary
        .names
        .as_ref()
        .and_then(|names| names.first())
        .map(|name| name.trim_start_matches('/').to_owned())
        .filter(|name| !name.is_empty())
        .unwrap_or_else(|| id.clone());
    let labels: BTreeMap<String, String> = summary.labels.unwrap_or_default().into_iter().collect();
    Some(Workload {
        id,
        name,
        scope_label: "docker-host".to_owned(),
        labels,
    })
}

fn matches_scope(workload: &Workload, scope: &WorkloadScope) -> bool {
    let WorkloadScope::Selectors(WorkloadSelectors {
        names,
        labels,
        projects,
    }) = scope
    else {
        return true;
    };
    let has_selectors = !names.is_empty() || !labels.is_empty() || !projects.is_empty();
    if !has_selectors {
        return false;
    }
    names
        .iter()
        .any(|name| name == &workload.name || name == &workload.id)
        || projects.iter().any(|project| {
            workload
                .labels
                .get(COMPOSE_PROJECT_LABEL)
                .is_some_and(|actual| actual == project)
        })
        || (!labels.is_empty()
            && labels
                .iter()
                .all(|(key, value)| workload.labels.get(key) == Some(value)))
}

fn docker_epoch_seconds(timestamp: DateTime<Utc>, round_up: bool) -> Result<i32, SourceError> {
    let seconds = timestamp.timestamp();
    let seconds = if round_up && timestamp.timestamp_subsec_nanos() > 0 {
        seconds.saturating_add(1)
    } else {
        seconds
    };
    i32::try_from(seconds).map_err(|_| SourceError {
        kind: SourceErrorKind::InvalidResponse,
        detail: "Docker time-window timestamps must fit its supported integer range".into(),
    })
}

fn map_docker_error(error: DockerError) -> SourceError {
    let kind = match &error {
        DockerError::DockerResponseServerError {
            status_code: 401 | 403,
            ..
        } => SourceErrorKind::AccessDenied,
        DockerError::DockerResponseServerError {
            status_code: 404, ..
        } => SourceErrorKind::WorkloadNotFound,
        DockerError::DockerResponseServerError { message, .. }
            if message.to_ascii_lowercase().contains("permission denied") =>
        {
            SourceErrorKind::AccessDenied
        }
        DockerError::RequestTimeoutError => SourceErrorKind::Timeout,
        DockerError::SocketNotFoundError(_) => SourceErrorKind::SourceUnreachable,
        DockerError::IOError { err } if err.kind() == io::ErrorKind::PermissionDenied => {
            SourceErrorKind::AccessDenied
        }
        DockerError::HyperLegacyError { err } if is_permission_denied(err) => {
            SourceErrorKind::AccessDenied
        }
        _ => SourceErrorKind::SourceUnreachable,
    };
    SourceError {
        kind,
        // Do not forward client/server error text into output; it may contain
        // endpoint details or implementation-specific information.
        detail: match kind {
            SourceErrorKind::AccessDenied => "access to the Docker Engine was denied".into(),
            SourceErrorKind::WorkloadNotFound => "the container was not found".into(),
            SourceErrorKind::Timeout => "the Docker Engine request timed out".into(),
            _ => "the Docker Engine could not be reached or returned an invalid response".into(),
        },
    }
}

fn is_permission_denied(error: &(dyn StdError + 'static)) -> bool {
    let mut current = Some(error);
    while let Some(cause) = current {
        if cause
            .downcast_ref::<io::Error>()
            .is_some_and(|error| error.kind() == io::ErrorKind::PermissionDenied)
            || cause
                .to_string()
                .to_ascii_lowercase()
                .contains("permission denied")
        {
            return true;
        }
        current = cause.source();
    }
    false
}

#[cfg(test)]
mod error_tests {
    use super::*;

    #[test]
    fn classifies_socket_permission_denial_as_access_denied() {
        let error = DockerError::IOError {
            err: io::Error::from(io::ErrorKind::PermissionDenied),
        };
        assert_eq!(map_docker_error(error).kind, SourceErrorKind::AccessDenied);
    }

    #[test]
    fn classifies_permission_denied_engine_response_as_access_denied() {
        let error = DockerError::DockerResponseServerError {
            status_code: 500,
            message: "permission denied while opening socket".to_owned(),
        };
        assert_eq!(map_docker_error(error).kind, SourceErrorKind::AccessDenied);
    }
}

#[cfg(test)]
mod diagnostics_tests {
    use super::*;

    #[test]
    fn maps_inspect_values_and_keeps_configured_resources_labeled_as_limits() {
        let inspect = bollard::models::ContainerInspectResponse {
            image: Some("sha256:image-id".to_owned()),
            restart_count: Some(3),
            state: Some(bollard::models::ContainerState {
                status: Some(bollard::models::ContainerStateStatusEnum::RUNNING),
                oom_killed: Some(false),
                ..Default::default()
            }),
            host_config: Some(bollard::models::HostConfig {
                nano_cpus: Some(2_000_000_000),
                memory: Some(1024),
                ..Default::default()
            }),
            ..Default::default()
        };
        let diagnostics = map_diagnostics(&inspect);
        assert_eq!(
            diagnostics.runtime_state,
            DiagnosticValue::Available("running".to_owned())
        );
        assert_eq!(diagnostics.restart_count, DiagnosticValue::Available(3));
        assert_eq!(diagnostics.oom_killed, DiagnosticValue::Available(false));
        assert_eq!(
            diagnostics.configured_cpu_nanocpus,
            DiagnosticValue::Available(2_000_000_000)
        );
        assert_eq!(
            diagnostics.configured_memory_bytes,
            DiagnosticValue::Available(1024)
        );
        assert_eq!(
            diagnostics.image,
            DiagnosticValue::Available("sha256:image-id".to_owned())
        );
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn matches_all_scope_and_name_label_or_compose_project_filters() {
        let workload = Workload {
            id: "abc123".into(),
            name: "payments-api".into(),
            scope_label: "docker-host".into(),
            labels: BTreeMap::from([
                ("team".into(), "payments".into()),
                (COMPOSE_PROJECT_LABEL.into(), "demo".into()),
            ]),
        };
        assert!(matches_scope(&workload, &WorkloadScope::All));
        assert!(matches_scope(
            &workload,
            &WorkloadScope::Selectors(WorkloadSelectors {
                names: vec!["payments-api".into()],
                ..WorkloadSelectors::default()
            })
        ));
        assert!(matches_scope(
            &workload,
            &WorkloadScope::Selectors(WorkloadSelectors {
                labels: BTreeMap::from([("team".into(), "payments".into())]),
                ..WorkloadSelectors::default()
            })
        ));
        assert!(matches_scope(
            &workload,
            &WorkloadScope::Selectors(WorkloadSelectors {
                projects: vec!["demo".into()],
                ..WorkloadSelectors::default()
            })
        ));
    }

    #[test]
    fn rejects_empty_selector_scope() {
        let workload = Workload {
            id: "abc123".into(),
            name: "payments-api".into(),
            scope_label: "docker-host".into(),
            labels: BTreeMap::new(),
        };
        assert!(!matches_scope(
            &workload,
            &WorkloadScope::Selectors(WorkloadSelectors::default())
        ));
    }

    #[test]
    fn rounds_docker_upper_bound_up_to_preserve_fractional_second_logs() {
        let timestamp = DateTime::parse_from_rfc3339("2026-10-16T12:00:00.500Z")
            .unwrap()
            .with_timezone(&Utc);
        let floor = i32::try_from(timestamp.timestamp()).unwrap();
        assert_eq!(docker_epoch_seconds(timestamp, false), Ok(floor));
        assert_eq!(docker_epoch_seconds(timestamp, true), Ok(floor + 1));
    }

    #[test]
    fn builds_server_side_filter_groups_without_falling_back_to_all_containers() {
        assert_eq!(docker_filter_groups(&WorkloadScope::All), vec![None]);

        let groups = docker_filter_groups(&WorkloadScope::Selectors(WorkloadSelectors {
            names: vec!["api".into()],
            labels: BTreeMap::from([("team".into(), "payments".into())]),
            projects: vec!["shop".into()],
        }));

        assert_eq!(groups.len(), 3);
        assert_eq!(
            groups[0],
            Some(HashMap::from([("name".into(), vec!["api".into()])]))
        );
        assert_eq!(
            groups[1],
            Some(HashMap::from([(
                "label".into(),
                vec!["com.docker.compose.project=shop".into()]
            )]))
        );
        assert_eq!(
            groups[2],
            Some(HashMap::from([(
                "label".into(),
                vec!["team=payments".into()]
            )]))
        );
    }
}
