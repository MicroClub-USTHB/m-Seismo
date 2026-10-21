-- migrate:up
-- Reports accepted by the ingestion API. payload is the report as
-- validated, with unknown fields already dropped.
create table reports (
    id uuid primary key default gen_random_uuid(),
    app_id uuid not null references apps (id) on delete cascade,
    -- the SDK's report ID; the same report sent twice is stored once
    report_id uuid not null,
    installation_id uuid not null,
    session_id text not null,
    trigger text not null,
    occurred_at timestamptz not null,
    received_at timestamptz not null default now(),
    -- set once the pipeline has been told about the report
    queued_at timestamptz,
    payload jsonb not null,
    unique (app_id, report_id)
);

create index reports_app_id_installation_id_idx on reports (app_id, installation_id);
create index reports_not_queued_idx on reports (received_at) where queued_at is null;

-- Files sent with a report (for now only the screenshot).
create table report_attachments (
    report_id uuid not null references reports (id) on delete cascade,
    name text not null,
    content_type text not null,
    data bytea not null,
    created_at timestamptz not null default now(),
    primary key (report_id, name)
);

-- migrate:down
drop table report_attachments;
drop table reports;
