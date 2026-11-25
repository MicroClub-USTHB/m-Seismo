-- migrate:up
-- Crash data sources are selected independently for each app.
ALTER TYPE app_platform ADD VALUE IF NOT EXISTS 'web';
CREATE TABLE app_crash_providers (
    app_id uuid PRIMARY KEY REFERENCES apps(id) ON DELETE CASCADE,
    provider text NOT NULL CHECK (provider IN ('sentry', 'crashlytics')),
    settings jsonb NOT NULL,
    credentials bytea,
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK (provider <> 'sentry' OR credentials IS NOT NULL)
);

-- migrate:down
DROP TABLE app_crash_providers;
-- PostgreSQL cannot remove an enum value in place. Keep 'web' on rollback
-- so existing web apps remain readable; this is an additive enum change.
