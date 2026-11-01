-- migrate:up

-- A per-repository webhook (GitLab, Bitbucket) so the tracker tells
-- m-Seismo when one of its issues changes state. Created the first time an
-- app's connection opens an issue in the repository; GitHub needs none,
-- its App already has one webhook for every repository it's installed on.
alter table project_repositories add column webhook_external_id text;
alter table project_repositories add column webhook_secret bytea;

-- migrate:down

alter table project_repositories drop column webhook_external_id;
alter table project_repositories drop column webhook_secret;
