-- migrate:up
-- set once the pipeline has opened an issue in every target repository
alter table reports add column processed_at timestamptz;

-- One row per report and repository it was delivered to. The queue can
-- deliver a report more than once, so the worker checks here before
-- opening an issue, and a report never opens two issues in one repository.
create table report_deliveries (
    report_id uuid not null references reports (id) on delete cascade,
    project_repository_id uuid not null references project_repositories (id) on delete cascade,
    -- null when the issue was opened but could not be recorded
    issue_id uuid references issues (id) on delete set null,
    url text not null,
    created_at timestamptz not null default now(),
    primary key (report_id, project_repository_id)
);

-- migrate:down
drop table report_deliveries;
alter table reports drop column processed_at;
