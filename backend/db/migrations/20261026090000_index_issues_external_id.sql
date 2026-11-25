-- migrate:up
-- Tracker webhooks name an issue by its tracker reference, compared
-- without case.
create index issues_lower_external_id_idx on issues (lower(external_id));

-- migrate:down
drop index issues_lower_external_id_idx;
