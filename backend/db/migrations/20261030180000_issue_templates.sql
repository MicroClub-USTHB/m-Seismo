-- migrate:up

-- A project's own issue layouts (FR-50): title_template and body_template
-- are Go text/template source, rendered with the report's title, steps,
-- crash, environment and logs already formatted as markdown.
create table issue_templates (
    id uuid primary key default gen_random_uuid(),
    project_id uuid not null references projects (id) on delete cascade,
    name text not null,
    title_template text not null,
    body_template text not null,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now()
);

create index issue_templates_project_id_idx on issue_templates (project_id);

-- A routing rule can pick one of its project's templates; without one the
-- default layout is used. Deleting the template falls back to it too,
-- rather than breaking the rule.
alter table routing_rules add column issue_template_id uuid references issue_templates (id) on delete set null;

-- migrate:down

alter table routing_rules drop column issue_template_id;
drop table issue_templates;
