-- migrate:up
-- Credentials of connections that need them (GitLab and Bitbucket OAuth
-- tokens), encrypted by the backend with AES-256-GCM. GitHub connections
-- have no row: they use installation tokens minted from the App key.
create table connection_secrets (
    provider_connection_id uuid primary key references provider_connections (id) on delete cascade,
    ciphertext bytea not null,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now()
);

-- migrate:down
drop table connection_secrets;
