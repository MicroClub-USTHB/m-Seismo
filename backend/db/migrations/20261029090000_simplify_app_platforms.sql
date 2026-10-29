-- migrate:up

-- A project groups one product's platform builds. Flutter and React
-- Native apps already ship as iOS and Android builds, so they aren't
-- platforms of their own: an app is web, iOS or Android.
alter table apps alter column platform type text;
drop type app_platform;
create type app_platform as enum ('web', 'ios', 'android');
alter table apps alter column platform type app_platform using platform::app_platform;

-- migrate:down

-- Lossy: apps on 'web' have no value in the old enum and this fails for
-- them. There was no production data when this was written.
alter table apps alter column platform type text;
drop type app_platform;
create type app_platform as enum ('ios', 'android', 'flutter', 'react_native');
alter table apps alter column platform type app_platform using platform::app_platform;
