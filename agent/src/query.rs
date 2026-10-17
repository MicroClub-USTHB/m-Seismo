use chrono::{DateTime, Utc};
use serde::Serialize;

const MAX_TRACE_ID_BYTES: usize = 256;
const MAX_FILTERS: usize = 100;
const MAX_FILTER_BYTES: usize = 256;

#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize)]
#[serde(rename_all = "snake_case")]
pub enum Source {
    Docker,
    Kubernetes,
}

impl Source {
    pub fn parse(value: &str) -> Result<Self, String> {
        match value {
            "docker" => Ok(Self::Docker),
            "kubernetes" => Ok(Self::Kubernetes),
            _ => Err(format!(
                "unsupported source '{value}'; expected 'docker' or 'kubernetes'"
            )),
        }
    }
}

#[derive(Clone, Debug, PartialEq, Eq, Serialize)]
pub struct QueryRequest {
    pub source: Source,
    pub from: DateTime<Utc>,
    pub to: DateTime<Utc>,
    pub trace_id: Option<String>,
    pub workload_filters: Vec<String>,
    pub namespaces: Vec<String>,
}

impl QueryRequest {
    pub fn validate(&self) -> Result<(), String> {
        if self.from >= self.to {
            return Err("--from must be earlier than --to".to_owned());
        }

        if let Some(trace_id) = &self.trace_id {
            validate_filter("trace ID", trace_id, MAX_TRACE_ID_BYTES)?;
        }

        validate_filters("workload", &self.workload_filters)?;
        validate_filters("namespace", &self.namespaces)?;

        if self.source == Source::Docker && !self.namespaces.is_empty() {
            return Err("--namespace can only be used with --source kubernetes".to_owned());
        }

        Ok(())
    }
}

fn validate_filters(label: &str, values: &[String]) -> Result<(), String> {
    if values.len() > MAX_FILTERS {
        return Err(format!("at most {MAX_FILTERS} {label} filters are allowed"));
    }

    for value in values {
        validate_filter(label, value, MAX_FILTER_BYTES)?;
    }

    Ok(())
}

fn validate_filter(label: &str, value: &str, max_bytes: usize) -> Result<(), String> {
    if value.trim().is_empty() {
        return Err(format!("{label} must not be empty"));
    }
    if value.len() > max_bytes {
        return Err(format!("{label} must be at most {max_bytes} bytes"));
    }
    if value.chars().any(char::is_control) {
        return Err(format!("{label} must not contain control characters"));
    }

    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    fn request(source: Source, from: &str, to: &str) -> QueryRequest {
        QueryRequest {
            source,
            from: DateTime::parse_from_rfc3339(from)
                .unwrap()
                .with_timezone(&Utc),
            to: DateTime::parse_from_rfc3339(to)
                .unwrap()
                .with_timezone(&Utc),
            trace_id: None,
            workload_filters: Vec::new(),
            namespaces: Vec::new(),
        }
    }

    #[test]
    fn accepts_valid_docker_request() {
        assert!(
            request(
                Source::Docker,
                "2026-10-16T10:00:00Z",
                "2026-10-16T10:01:00Z"
            )
            .validate()
            .is_ok()
        );
    }

    #[test]
    fn rejects_empty_or_reversed_time_range() {
        assert!(
            request(
                Source::Docker,
                "2026-10-16T10:00:00Z",
                "2026-10-16T10:00:00Z"
            )
            .validate()
            .is_err()
        );
        assert!(
            request(
                Source::Docker,
                "2026-10-16T10:01:00Z",
                "2026-10-16T10:00:00Z"
            )
            .validate()
            .is_err()
        );
    }

    #[test]
    fn rejects_namespace_filter_for_docker() {
        let mut request = request(
            Source::Docker,
            "2026-10-16T10:00:00Z",
            "2026-10-16T10:01:00Z",
        );
        request.namespaces.push("payments".to_owned());
        assert!(request.validate().unwrap_err().contains("only be used"));
    }

    #[test]
    fn rejects_control_characters_in_trace_id() {
        let mut request = request(
            Source::Kubernetes,
            "2026-10-16T10:00:00Z",
            "2026-10-16T10:01:00Z",
        );
        request.trace_id = Some("trace\nid".to_owned());
        assert!(
            request
                .validate()
                .unwrap_err()
                .contains("control characters")
        );
    }
}
