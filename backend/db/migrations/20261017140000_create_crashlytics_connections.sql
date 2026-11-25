-- migrate:up
-- One Crashlytics connection per project, created by the OAuth flow
-- (internal/crash/crashlytics). The refresh token is stored encrypted.
create table crashlytics_connections (
    project_id uuid primary key references projects (id) on delete cascade,
    google_email text not null,
    refresh_token bytea not null,
    apps text[] not null default '{}',
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now()
);

-- migrate:down
drop table crashlytics_connections;
