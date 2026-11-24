-- migrate:up
CREATE TABLE app_observability_providers (
    app_id uuid NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    provider text NOT NULL CHECK (provider IN ('grafana', 'loki', 'cloudwatch', 'datadog')),
    settings jsonb NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (app_id, provider)
);

-- migrate:down
DROP TABLE IF EXISTS app_observability_providers;
