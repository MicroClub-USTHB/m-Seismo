-- migrate:up

-- A surrogate id lets an attachment be addressed on its own
-- (GET /api/v1/attachments/{id}), instead of only through its report.
alter table report_attachments add column id uuid not null default gen_random_uuid();
alter table report_attachments add constraint report_attachments_id_key unique (id);

-- migrate:down

alter table report_attachments drop constraint report_attachments_id_key;
alter table report_attachments drop column id;
