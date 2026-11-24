-- migrate:up
-- The reports grouped under an issue are counted from their deliveries.
create index report_deliveries_issue_id_idx on report_deliveries (issue_id);

-- migrate:down
drop index report_deliveries_issue_id_idx;
