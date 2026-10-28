-- migrate:up
-- Each app brings its own OAuth application for GitLab and Bitbucket
-- (client_id, client_secret), instead of one shared OAuth application
-- configured for the whole deployment. The client secret is encrypted by
-- the backend with AES-256-GCM, the same as connection_secrets.
create table app_tracker_credentials (
    id uuid primary key default gen_random_uuid(),
    app_id uuid not null references apps (id) on delete cascade,
    tracker tracker not null,
    client_id text not null,
    client_secret_ciphertext bytea not null,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    unique (app_id, tracker),
    check (tracker in ('gitlab', 'bitbucket'))
);

-- migrate:down
drop table app_tracker_credentials;
