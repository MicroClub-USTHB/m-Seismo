-- migrate:up

-- NULL means the app uses the operator's default retention period
-- (retention.default_days); a value overrides it for this app.
alter table apps add column retention_days integer;
alter table apps add constraint apps_retention_days_check check (retention_days is null or retention_days > 0);

-- migrate:down

alter table apps drop constraint apps_retention_days_check;
alter table apps drop column retention_days;
