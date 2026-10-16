-- +goose Up
CREATE TABLE projects (
    project_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    uuid NOT NULL REFERENCES users (user_id) ON DELETE CASCADE,
    name       text NOT NULL CHECK (length(name) BETWEEN 1 AND 255),
    slug       text NOT NULL CHECK (slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$' AND length(slug) <= 100),
    settings   jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(settings) = 'object'),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT projects_user_slug_key UNIQUE (user_id, slug)
);

CREATE INDEX projects_user_created_idx ON projects (user_id, created_at, project_id);

CREATE TRIGGER projects_set_updated_at BEFORE UPDATE ON projects
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE apps (
    app_id      uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id  uuid NOT NULL REFERENCES projects (project_id) ON DELETE CASCADE,
    name        text NOT NULL CHECK (length(name) BETWEEN 1 AND 255),
    platform    text NOT NULL CHECK (platform IN ('ios', 'android', 'flutter', 'react_native')),
    bundle_id   text NOT NULL CHECK (length(bundle_id) BETWEEN 1 AND 255),
    environment text NOT NULL DEFAULT 'production' CHECK (environment ~ '^[a-z0-9][a-z0-9_-]{0,49}$'),
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT apps_project_bundle_env_key UNIQUE (project_id, bundle_id, environment)
);

CREATE INDEX apps_project_created_idx ON apps (project_id, created_at, app_id);

CREATE TRIGGER apps_set_updated_at BEFORE UPDATE ON apps
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- +goose Down
DROP TABLE apps;
DROP TABLE projects;
