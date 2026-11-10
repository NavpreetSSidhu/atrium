-- +goose Up
-- Run as a migration role with BYPASSRLS (superuser locally). The app connects as a role granted app_user.
-- +goose StatementBegin
create extension if not exists btree_gist;
create extension if not exists pgcrypto;

do $$ begin
  if not exists (select 1 from pg_roles where rolname = 'app_user') then
    create role app_user nologin;
  end if;
end $$;

create schema app;

-- Request scope, set per transaction by db.InTenant. Unset => NULL => no rows.
create function app.tenant_id() returns uuid language sql stable
  as $f$ select nullif(current_setting('app.tenant_id', true), '')::uuid $f$;
create function app.user_id() returns uuid language sql stable
  as $f$ select nullif(current_setting('app.user_id', true), '')::uuid $f$;

create table users (
  id         uuid primary key default gen_random_uuid(),
  phone      text unique,
  email      text unique,
  name       text not null default '',
  created_at timestamptz not null default now()
);

create table tenants (
  id         uuid primary key default gen_random_uuid(),
  slug       text not null unique check (slug ~ '^[a-z0-9-]{3,40}$'),
  name       text not null,
  plan       text not null default 'free' check (plan in ('free','basic','pro')),
  created_at timestamptz not null default now()
);

create table memberships (
  tenant_id  uuid not null references tenants(id) on delete cascade,
  user_id    uuid not null references users(id) on delete cascade,
  role       text not null check (role in ('owner','admin','staff','provider','member')),
  created_at timestamptz not null default now(),
  primary key (tenant_id, user_id)
);
create index on memberships (user_id);

-- Role of the current user in the current tenant. SECURITY DEFINER so the
-- memberships policy can use it without recursing into itself.
create function app.member_role() returns text language sql stable security definer
  set search_path = public
  as $f$ select role from memberships where tenant_id = app.tenant_id() and user_id = app.user_id() $f$;
create function app.is_staff() returns boolean language sql stable
  as $f$ select coalesce(app.member_role() in ('owner','admin','staff'), false) $f$;

create table providers (
  id         uuid primary key default gen_random_uuid(),
  tenant_id  uuid not null references tenants(id) on delete cascade,
  user_id    uuid not null references users(id),
  title      text not null default '',
  timezone   text not null default 'Asia/Kolkata',
  unique (tenant_id, user_id),
  unique (tenant_id, id)
);

create table bookings (
  id          uuid primary key default gen_random_uuid(),
  tenant_id   uuid not null references tenants(id) on delete cascade,
  provider_id uuid not null,
  customer_id uuid not null references users(id),
  starts_at   timestamptz not null,
  ends_at     timestamptz not null check (ends_at > starts_at),
  status      text not null default 'held' check (status in ('held','confirmed','cancelled','completed')),
  hold_expires_at timestamptz,
  created_at  timestamptz not null default now(),
  -- Tenant-composite FKs: FK checks bypass RLS, so a plain FK would accept another tenant's provider.
  foreign key (tenant_id, provider_id) references providers (tenant_id, id),
  unique (tenant_id, id),
  -- No double booking, enforced by the database, not app code.
  exclude using gist (provider_id with =, tstzrange(starts_at, ends_at) with &&) where (status <> 'cancelled')
);
create index on bookings (tenant_id, starts_at);
create index on bookings (tenant_id, customer_id);

create table notes (
  id              uuid primary key default gen_random_uuid(),
  tenant_id       uuid not null references tenants(id) on delete cascade,
  author_id       uuid not null references users(id),
  subject_user_id uuid references users(id),
  booking_id      uuid,
  visibility      text not null default 'private' check (visibility in ('private','shared')),
  body_enc        bytea not null, -- envelope-encrypted (KMS data key, AES-GCM) by the app
  created_at      timestamptz not null default now(),
  foreign key (tenant_id, booking_id) references bookings (tenant_id, id)
);
create index on notes (tenant_id, booking_id);

-- RLS: every table, forced.
alter table users       enable row level security; alter table users       force row level security;
alter table tenants     enable row level security; alter table tenants     force row level security;
alter table memberships enable row level security; alter table memberships force row level security;
alter table providers   enable row level security; alter table providers   force row level security;
alter table bookings    enable row level security; alter table bookings    force row level security;
alter table notes       enable row level security; alter table notes       force row level security;

create policy self_or_cotenant on users using (
  id = app.user_id()
  or (app.member_role() is not null
      and exists (select 1 from memberships m where m.user_id = users.id and m.tenant_id = app.tenant_id())));
create policy self_update on users for update using (id = app.user_id());

create policy member_read on tenants for select using (id = app.tenant_id() and app.member_role() is not null);
create policy owner_update on tenants for update using (id = app.tenant_id() and app.member_role() = 'owner');

create policy tenant_read on memberships for select
  using (tenant_id = app.tenant_id() and (app.is_staff() or user_id = app.user_id()));
create policy staff_write on memberships for all
  using (tenant_id = app.tenant_id() and app.member_role() in ('owner','admin'))
  with check (tenant_id = app.tenant_id() and app.member_role() in ('owner','admin'));

create policy tenant_read on providers for select using (tenant_id = app.tenant_id() and app.member_role() is not null);
create policy staff_write on providers for all
  using (tenant_id = app.tenant_id() and app.is_staff())
  with check (tenant_id = app.tenant_id() and app.is_staff());

create policy party_or_staff on bookings for all
  using (tenant_id = app.tenant_id() and (
    app.is_staff()
    or customer_id = app.user_id()
    or provider_id in (select id from providers where user_id = app.user_id())))
  with check (tenant_id = app.tenant_id() and app.member_role() is not null and (
    app.is_staff()
    or customer_id = app.user_id()
    or provider_id in (select id from providers where user_id = app.user_id())));

-- Clinical/case notes: author only; shared notes also visible to their subject. Owners do NOT see private notes.
create policy author_all on notes for all
  using (tenant_id = app.tenant_id() and author_id = app.user_id())
  with check (tenant_id = app.tenant_id() and author_id = app.user_id() and app.member_role() is not null);
create policy subject_read on notes for select
  using (tenant_id = app.tenant_id() and visibility = 'shared' and subject_user_id = app.user_id());

grant usage on schema app, public to app_user;
grant execute on all functions in schema app to app_user;
grant select, insert, update, delete on all tables in schema public to app_user;
-- +goose StatementEnd

-- +goose Down
drop table notes, bookings, providers, memberships, tenants, users;
drop schema app cascade;
