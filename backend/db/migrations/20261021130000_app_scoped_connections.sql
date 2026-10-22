-- migrate:up
-- Tracker connections move from the owner to the app: each app brings its
-- own credentials, and projects only say where their code lives.
create extension if not exists btree_gist;

alter table provider_connections drop constraint provider_connections_tracker_external_id_key;
alter table provider_connections add column app_id uuid;

-- Every app could use every connection of its owner until now, so each
-- owner-level connection is copied to each of the owner's apps.
create temporary table connection_copies (
    old_id uuid not null,
    app_id uuid not null,
    new_id uuid not null default gen_random_uuid()
) on commit drop;

insert into connection_copies (old_id, app_id)
select pc.id, a.id
from provider_connections pc
join apps a on a.owner_id = pc.owner_id;

insert into provider_connections (id, owner_id, app_id, tracker, external_id, account_login, account_type, suspended_at, created_at, updated_at)
select cc.new_id, pc.owner_id, cc.app_id, pc.tracker, pc.external_id, pc.account_login, pc.account_type, pc.suspended_at, pc.created_at, pc.updated_at
from connection_copies cc
join provider_connections pc on pc.id = cc.old_id;

-- issues remember which connection opened them: comments and status sync
-- need those credentials, and the repository no longer carries them
alter table issues
    add column provider_connection_id uuid references provider_connections (id) on delete set null;

update issues i
set provider_connection_id = cc.new_id
from project_repositories pr, connection_copies cc
where pr.id = i.project_repository_id
  and cc.old_id = pr.provider_connection_id
  and cc.app_id = i.app_id;

create index issues_provider_connection_id_idx on issues (provider_connection_id);

-- project repositories name a tracker instead of a connection
alter table project_repositories add column tracker tracker;

update project_repositories pr
set tracker = pc.tracker
from provider_connections pc
where pc.id = pr.provider_connection_id;

alter table project_repositories alter column tracker set not null;
alter table project_repositories drop constraint project_repositories_target_key;
drop index project_repositories_provider_connection_id_idx;
alter table project_repositories drop column provider_connection_id;

-- the same repository linked through two connections becomes one row,
-- keeping the default (or else the oldest)
delete from project_repositories
where id in (
    select id
    from (
        select id, row_number() over (
            partition by project_id, tracker, repository
            order by is_default desc, created_at, id
        ) as n
        from project_repositories
    ) ranked
    where n > 1
);

alter table project_repositories
    add constraint project_repositories_target_key unique (project_id, tracker, repository);

-- owner-level connections are gone; every connection now belongs to an app
delete from provider_connections where app_id is null;
alter table provider_connections alter column app_id set not null;

alter table apps add constraint apps_id_owner_id_key unique (id, owner_id);

alter table provider_connections
    -- owner_id is kept for owner-scoped queries; this keeps it equal to the app's owner
    add constraint provider_connections_app_owner_fkey
        foreign key (app_id, owner_id) references apps (id, owner_id) on delete cascade,
    add constraint provider_connections_app_target_key unique (app_id, tracker, external_id),
    -- apps of one owner may share a tracker account; two owners never can
    add constraint provider_connections_one_owner_per_account
        exclude using gist (tracker with =, external_id with =, owner_id with <>);

create index provider_connections_app_id_idx on provider_connections (app_id);

-- migrate:down
-- Lossy: connections collapse to one per tracker account (the oldest copy
-- wins), and project repositories that no remaining connection's account
-- owns are deleted.
drop index provider_connections_app_id_idx;
alter table provider_connections
    drop constraint provider_connections_one_owner_per_account,
    drop constraint provider_connections_app_target_key,
    drop constraint provider_connections_app_owner_fkey;
alter table apps drop constraint apps_id_owner_id_key;

drop index issues_provider_connection_id_idx;
alter table issues drop column provider_connection_id;

delete from provider_connections
where id in (
    select id
    from (
        select id, row_number() over (
            partition by tracker, external_id
            order by created_at, id
        ) as n
        from provider_connections
    ) ranked
    where n > 1
);

alter table provider_connections drop column app_id;
alter table provider_connections
    add constraint provider_connections_tracker_external_id_key unique (tracker, external_id);

alter table project_repositories
    add column provider_connection_id uuid references provider_connections (id) on delete cascade;

update project_repositories pr
set provider_connection_id = pc.id
from projects p, provider_connections pc
where p.id = pr.project_id
  and pc.owner_id = p.owner_id
  and pc.tracker = pr.tracker
  and lower(pc.account_login) = lower(split_part(pr.repository, '/', 1));

delete from project_repositories where provider_connection_id is null;

alter table project_repositories alter column provider_connection_id set not null;
alter table project_repositories drop constraint project_repositories_target_key;
alter table project_repositories drop column tracker;
alter table project_repositories
    add constraint project_repositories_target_key unique (project_id, provider_connection_id, repository);

create index project_repositories_provider_connection_id_idx on project_repositories (provider_connection_id);

drop extension if exists btree_gist;
