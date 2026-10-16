-- migrate:up
create type tracker as enum ('github', 'gitlab', 'bitbucket');

create table project_repositories (
    id uuid primary key default gen_random_uuid(),
    project_id uuid not null references projects (id) on delete cascade,
    tracker tracker not null,
    repository text not null,
    is_default boolean not null default false,
    created_at timestamptz not null default now()
);

create index project_repositories_project_id_idx on project_repositories (project_id);

-- one default repository per project
create unique index project_repositories_one_default_per_project
    on project_repositories (project_id)
    where is_default;

-- migrate:down
drop table project_repositories;
drop type tracker;
