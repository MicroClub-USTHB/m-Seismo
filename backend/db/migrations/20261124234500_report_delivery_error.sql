-- migrate:up

-- Failure reason recorded when the worker gives up on delivering a report
-- (permanent error or max_attempts reached), cleared on retry or delivery.
alter table reports add column delivery_error text;

-- Speeds up keyset-paginated listing of an app's reports newest-first.
create index reports_app_id_occurred_at_id_idx on reports (app_id, occurred_at desc, id desc);

-- migrate:down

drop index reports_app_id_occurred_at_id_idx;
alter table reports drop column delivery_error;
