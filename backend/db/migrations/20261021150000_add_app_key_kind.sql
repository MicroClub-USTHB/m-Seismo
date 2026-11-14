-- migrate:up
-- sdk keys (msk_) are sent by the SDKs to POST /reports; coordinator keys
-- (mco_) attach the app's log coordinator. One kind is never accepted
-- where the other is expected.
create type app_key_kind as enum ('sdk', 'coordinator');

alter table app_keys add column kind app_key_kind not null default 'sdk';
alter table app_keys alter column kind drop default;

-- migrate:down
alter table app_keys drop column kind;
drop type app_key_kind;
