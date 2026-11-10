-- +goose Up
-- Scheduling: services, availability, buffers on bookings, holds, joining a space. See PRD M4, ADR-7.
-- +goose StatementBegin
create table offerings (
  id          uuid primary key default gen_random_uuid(),
  tenant_id   uuid not null references tenants(id) on delete cascade,
  type        text not null check (type in ('course','live_class','event','service','membership','product_physical','product_digital')),
  title       text not null check (length(title) between 1 and 120),
  description text not null default '',
  price_paise bigint not null default 0 check (price_paise >= 0),
  currency    text not null default 'INR',
  status      text not null default 'published' check (status in ('draft','published','archived')),
  created_at  timestamptz not null default now(),
  unique (tenant_id, id)
);
create index on offerings (tenant_id, type);

create table services (
  offering_id         uuid primary key,
  tenant_id           uuid not null,
  duration_min        int not null check (duration_min between 5 and 480),
  buffer_min          int not null default 0 check (buffer_min between 0 and 240),
  location            text not null default 'video' check (location in ('video','in_person','phone')),
  min_notice_min      int not null default 60 check (min_notice_min between 0 and 43200),
  horizon_days        int not null default 60 check (horizon_days between 1 and 365),
  cancel_cutoff_hours int not null default 24 check (cancel_cutoff_hours between 0 and 720),
  foreign key (tenant_id, offering_id) references offerings (tenant_id, id) on delete cascade
);

-- Minutes from local midnight in the provider's timezone.
create table availability_rules (
  id          uuid primary key default gen_random_uuid(),
  tenant_id   uuid not null,
  provider_id uuid not null,
  weekday     smallint not null check (weekday between 0 and 6), -- 0 = Sunday, as Go's time.Weekday
  start_min   smallint not null check (start_min between 0 and 1439),
  end_min     smallint not null check (end_min between 1 and 1440 and end_min > start_min),
  foreign key (tenant_id, provider_id) references providers (tenant_id, id) on delete cascade
);
create index on availability_rules (tenant_id, provider_id);

-- An override replaces that date's weekly hours. A row with null times closes the day.
create table availability_overrides (
  id          uuid primary key default gen_random_uuid(),
  tenant_id   uuid not null,
  provider_id uuid not null,
  date        date not null,
  start_min   smallint check (start_min between 0 and 1439),
  end_min     smallint check (end_min between 1 and 1440),
  check ((start_min is null) = (end_min is null) and (start_min is null or end_min > start_min)),
  foreign key (tenant_id, provider_id) references providers (tenant_id, id) on delete cascade
);
create index on availability_overrides (tenant_id, provider_id, date);

-- Bookings block [starts_at, blocked_until): the appointment plus its buffer.
alter table bookings add column service_id uuid, add column blocked_until timestamptz;
update bookings set blocked_until = ends_at;
alter table bookings
  alter column blocked_until set not null,
  add constraint bookings_blocked_until_check check (blocked_until >= ends_at),
  add foreign key (tenant_id, service_id) references offerings (tenant_id, id),
  drop constraint bookings_provider_id_tstzrange_excl,
  add constraint bookings_no_overlap exclude using gist
    (provider_id with =, tstzrange(starts_at, blocked_until) with &&) where (status <> 'cancelled');

alter table offerings              enable row level security; alter table offerings              force row level security;
alter table services               enable row level security; alter table services               force row level security;
alter table availability_rules     enable row level security; alter table availability_rules     force row level security;
alter table availability_overrides enable row level security; alter table availability_overrides force row level security;

-- Members see published offerings; staff see and manage all.
create policy member_read on offerings for select
  using (tenant_id = app.tenant_id() and (app.is_staff() or (app.member_role() is not null and status = 'published')));
create policy staff_write on offerings for all
  using (tenant_id = app.tenant_id() and app.is_staff()) with check (tenant_id = app.tenant_id() and app.is_staff());
create policy member_read on services for select using (tenant_id = app.tenant_id() and app.member_role() is not null);
create policy staff_write on services for all
  using (tenant_id = app.tenant_id() and app.is_staff()) with check (tenant_id = app.tenant_id() and app.is_staff());

-- Availability: everyone in the space can read it (to book); staff or the provider themself can change it.
create policy member_read on availability_rules for select using (tenant_id = app.tenant_id() and app.member_role() is not null);
create policy manage on availability_rules for all
  using (tenant_id = app.tenant_id() and (app.is_staff() or provider_id in (select id from providers where user_id = app.user_id())))
  with check (tenant_id = app.tenant_id() and (app.is_staff() or provider_id in (select id from providers where user_id = app.user_id())));
create policy member_read on availability_overrides for select using (tenant_id = app.tenant_id() and app.member_role() is not null);
create policy manage on availability_overrides for all
  using (tenant_id = app.tenant_id() and (app.is_staff() or provider_id in (select id from providers where user_id = app.user_id())))
  with check (tenant_id = app.tenant_id() and (app.is_staff() or provider_id in (select id from providers where user_id = app.user_id())));

grant select, insert, update, delete on offerings, services, availability_rules, availability_overrides to app_user;

-- A provider sees the users who booked with them (names on their schedule), and no one else.
create policy provider_clients on users for select using (exists (
  select 1 from bookings b where b.customer_id = users.id and b.tenant_id = app.tenant_id()
     and b.provider_id in (select id from providers where user_id = app.user_id())));

-- Busy time for slot generation. Members can't read other customers' bookings, so this returns times only.
create function app.busy(p_provider uuid, p_from timestamptz, p_to timestamptz)
returns table (starts_at timestamptz, blocked_until timestamptz)
language sql stable security definer set search_path = public as $f$
  select b.starts_at, b.blocked_until from bookings b
   where b.tenant_id = app.tenant_id() and app.member_role() is not null
     and b.provider_id = p_provider and b.status <> 'cancelled'
     and not (b.status = 'held' and b.hold_expires_at <= now())
     and b.starts_at < p_to and b.blocked_until > p_from
$f$;

-- Frees slots whose checkout hold lapsed. Called inside the booking tx (the constraint can't use now()).
create function app.release_expired_holds(p_provider uuid) returns void
language sql security definer set search_path = public as $f$
  update bookings set status = 'cancelled'
   where tenant_id = app.tenant_id() and app.member_role() is not null
     and provider_id = p_provider and status = 'held' and hold_expires_at <= now()
$f$;

-- Joins the current user to a space as a member (from its booking page). Returns null for an unknown slug.
create function app.join_tenant(p_slug text) returns uuid
language plpgsql security definer set search_path = public as $f$
declare tid uuid;
begin
  if app.user_id() is null then
    raise exception 'no user in scope' using errcode = '42501';
  end if;
  select id into tid from tenants where slug = p_slug;
  if tid is not null then
    insert into memberships (tenant_id, user_id, role) values (tid, app.user_id(), 'member') on conflict do nothing;
  end if;
  return tid;
end $f$;

grant execute on all functions in schema app to app_user;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
drop policy provider_clients on users;
drop function app.join_tenant(text);
drop function app.release_expired_holds(uuid);
drop function app.busy(uuid, timestamptz, timestamptz);
alter table bookings drop constraint bookings_no_overlap,
  add constraint bookings_provider_id_tstzrange_excl exclude using gist
    (provider_id with =, tstzrange(starts_at, ends_at) with &&) where (status <> 'cancelled');
alter table bookings drop column service_id, drop column blocked_until;
drop table availability_overrides, availability_rules, services, offerings;
-- +goose StatementEnd
