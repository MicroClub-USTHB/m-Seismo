-- migrate:up
create table owners (
    id uuid primary key default gen_random_uuid(),
    email text not null,
    password_hash text not null,
    name text not null,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now()
);

create unique index owners_email_lower_key on owners (lower(email));

-- projects now belong to an owner; slugs are unique per owner. The default
-- repository lives in project_repositories.is_default.
alter table projects add column owner_id uuid not null references owners (id) on delete cascade;
alter table projects drop constraint projects_slug_key;
alter table projects add constraint projects_owner_id_slug_key unique (owner_id, slug);
alter table projects drop column default_repository;

-- an owner's link to a tracker account (for GitHub: one App installation)
create table provider_connections (
    id uuid primary key default gen_random_uuid(),
    owner_id uuid not null references owners (id) on delete cascade,
    tracker tracker not null,
    external_id text not null,
    account_login text not null,
    account_type text not null,
    suspended_at timestamptz,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    unique (tracker, external_id)
);

create index provider_connections_owner_id_idx on provider_connections (owner_id);

-- a project repository is reached through one of the owner's connections
alter table project_repositories
    add column provider_connection_id uuid not null references provider_connections (id) on delete cascade;
alter table project_repositories drop column tracker;
alter table project_repositories
    add constraint project_repositories_target_key unique (project_id, provider_connection_id, repository);

create index project_repositories_provider_connection_id_idx on project_repositories (provider_connection_id);

-- apps are the sources of issues and hold the SDK keys
create type app_platform as enum ('ios', 'android', 'flutter', 'react_native');

create table apps (
    id uuid primary key default gen_random_uuid(),
    owner_id uuid not null references owners (id) on delete cascade,
    name text not null,
    platform app_platform not null,
    bundle_id text not null,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now()
);

create index apps_owner_id_idx on apps (owner_id);

create table app_keys (
    id uuid primary key default gen_random_uuid(),
    app_id uuid not null references apps (id) on delete cascade,
    key_hash bytea not null unique,
    key_prefix text not null,
    revoked_at timestamptz,
    created_at timestamptz not null default now()
);

create index app_keys_app_id_idx on app_keys (app_id);

create table app_projects (
    app_id uuid not null references apps (id) on delete cascade,
    project_id uuid not null references projects (id) on delete cascade,
    created_at timestamptz not null default now(),
    primary key (app_id, project_id)
);

create index app_projects_project_id_idx on app_projects (project_id);

drop table project_keys;

-- issues opened on a tracker
create table issues (
    id uuid primary key default gen_random_uuid(),
    project_repository_id uuid not null references project_repositories (id) on delete cascade,
    app_id uuid references apps (id) on delete set null,
    external_id text not null,
    url text not null,
    title text not null,
    state text not null default 'open' check (state in ('open', 'closed')),
    fingerprint text,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now()
);

create index issues_project_repository_id_fingerprint_idx on issues (project_repository_id, fingerprint);

-- migrate:down
drop table issues;

create table project_keys (
    id uuid primary key default gen_random_uuid(),
    project_id uuid not null references projects (id) on delete cascade,
    key text not null unique,
    revoked_at timestamptz,
    created_at timestamptz not null default now()
);

create index project_keys_project_id_idx on project_keys (project_id);

drop table app_projects;
drop table app_keys;
drop table apps;
drop type app_platform;

alter table project_repositories drop constraint project_repositories_target_key;
alter table project_repositories add column tracker tracker;
update project_repositories pr
set tracker = pc.tracker
from provider_connections pc
where pc.id = pr.provider_connection_id;
alter table project_repositories alter column tracker set not null;
alter table project_repositories drop column provider_connection_id;

drop table provider_connections;

alter table projects add column default_repository text;
alter table projects drop constraint projects_owner_id_slug_key;
alter table projects add constraint projects_slug_key unique (slug);
alter table projects drop column owner_id;

drop table owners;
