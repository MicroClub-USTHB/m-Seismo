-- migrate:up
create extension if not exists pgcrypto;

create table projects (
    id uuid primary key default gen_random_uuid(),
    name text not null,
    slug text not null unique,
    default_repository text,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now()
);

create table project_keys (
    id uuid primary key default gen_random_uuid(),
    project_id uuid not null references projects (id) on delete cascade,
    key text not null unique,
    revoked_at timestamptz,
    created_at timestamptz not null default now()
);

create index project_keys_project_id_idx on project_keys (project_id);

-- migrate:down
drop table project_keys;
drop table projects;
