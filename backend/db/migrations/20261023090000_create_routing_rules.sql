-- migrate:up
-- A project's routing rules, tried in position order: the first rule whose
-- conditions all match a report sends its issue to the rule's repository,
-- with the rule's labels, assignees and milestone. A report that matches
-- no rule goes to the project's default repository.
create table routing_rules (
    id uuid primary key default gen_random_uuid(),
    project_id uuid not null references projects (id) on delete cascade,
    position integer not null default 0 check (position >= 0),
    -- conditions; null matches any report. A rule for an app goes with the
    -- app, so it never turns into a rule for every app.
    app_id uuid references apps (id) on delete cascade,
    severity text check (severity in ('low', 'medium', 'high', 'critical')),
    category text check (category in ('bug', 'crash', 'ui', 'other')),
    -- where a matching report goes, one of the project's repositories. A
    -- repository can't be unlinked while rules send to it.
    project_repository_id uuid not null references project_repositories (id),
    labels text[] not null default '{}',
    assignees text[] not null default '{}',
    milestone text,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now()
);

create index routing_rules_project_id_position_idx on routing_rules (project_id, position);
create index routing_rules_app_id_idx on routing_rules (app_id);
create index routing_rules_project_repository_id_idx on routing_rules (project_repository_id);

-- migrate:down
drop table routing_rules;
