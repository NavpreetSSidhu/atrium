-- +goose Up
-- Auth: login codes (SMS OTP, email magic link) and rotating refresh-token sessions. See ADR-6.
-- +goose StatementBegin
create table login_codes (
  id          uuid primary key default gen_random_uuid(),
  channel     text not null check (channel in ('sms','email')),
  dest        text not null,
  code_hash   bytea not null,
  attempts    int not null default 0,
  expires_at  timestamptz not null,
  consumed_at timestamptz,
  created_at  timestamptz not null default now()
);
create index on login_codes (channel, dest, created_at);

create table sessions (
  id           uuid primary key default gen_random_uuid(),
  user_id      uuid not null references users(id) on delete cascade,
  refresh_hash bytea not null unique,
  prev_hash    bytea unique,
  user_agent   text not null default '',
  created_at   timestamptz not null default now(),
  last_used_at timestamptz not null default now(),
  expires_at   timestamptz not null,
  revoked_at   timestamptz
);
create index on sessions (user_id);

alter table login_codes enable row level security; alter table login_codes force row level security;
alter table sessions    enable row level security; alter table sessions    force row level security;

-- login_codes: no policies, no grants. Only the SECURITY DEFINER functions below touch it.
create policy own_read on sessions for select using (user_id = app.user_id());
create policy own_revoke on sessions for update using (user_id = app.user_id()) with check (user_id = app.user_id());
grant select, update (revoked_at) on sessions to app_user;

-- Returns false when the destination is rate limited (5 codes per hour).
create function app.start_login(p_channel text, p_dest text, p_code_hash bytea) returns boolean
language plpgsql security definer set search_path = public as $f$
begin
  perform pg_advisory_xact_lock(hashtext(p_channel || ':' || p_dest));
  if (select count(*) from login_codes
       where channel = p_channel and dest = p_dest and created_at > now() - interval '1 hour') >= 5 then
    return false;
  end if;
  -- One live code per destination.
  update login_codes set consumed_at = now() where channel = p_channel and dest = p_dest and consumed_at is null;
  insert into login_codes (channel, dest, code_hash, expires_at)
    values (p_channel, p_dest, p_code_hash, now() + interval '10 minutes');
  return true;
end $f$;

-- On a correct code: consumes it, upserts the user, opens a session. Otherwise returns nulls.
-- The caller must commit even on failure, or the attempt counter is lost.
create function app.finish_login(p_channel text, p_dest text, p_code_hash bytea, p_refresh_hash bytea,
  p_user_agent text, p_ttl interval, out user_id uuid, out session_id uuid)
language plpgsql security definer set search_path = public as $f$
declare c login_codes;
begin
  select * into c from login_codes l
   where l.channel = p_channel and l.dest = p_dest and l.consumed_at is null and l.expires_at > now()
   order by l.created_at desc limit 1 for update;
  if not found then return; end if;
  if c.code_hash <> p_code_hash then
    update login_codes set attempts = attempts + 1, consumed_at = case when attempts + 1 >= 5 then now() end
     where id = c.id;
    return;
  end if;
  update login_codes set consumed_at = now() where id = c.id;
  if p_channel = 'sms' then
    insert into users (phone) values (p_dest)
      on conflict (phone) do update set phone = excluded.phone returning id into finish_login.user_id;
  else
    insert into users (email) values (p_dest)
      on conflict (email) do update set email = excluded.email returning id into finish_login.user_id;
  end if;
  insert into sessions (user_id, refresh_hash, user_agent, expires_at)
    values (finish_login.user_id, p_refresh_hash, left(p_user_agent, 256), now() + p_ttl)
    returning id into finish_login.session_id;
end $f$;

-- Rotates the refresh token. A rotated-out token coming back revokes the session (theft detection).
-- The caller must commit even on failure, or the revocation is lost.
create function app.refresh_session(p_old bytea, p_new bytea, out user_id uuid, out session_id uuid)
language plpgsql security definer set search_path = public as $f$
declare s sessions;
begin
  select * into s from sessions where refresh_hash = p_old for update;
  if not found then
    update sessions set revoked_at = now() where prev_hash = p_old and revoked_at is null;
    return;
  end if;
  if s.revoked_at is not null or s.expires_at <= now() then return; end if;
  update sessions set prev_hash = refresh_hash, refresh_hash = p_new, last_used_at = now() where id = s.id;
  refresh_session.user_id := s.user_id;
  refresh_session.session_id := s.id;
end $f$;

grant execute on all functions in schema app to app_user;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
drop function app.refresh_session(bytea, bytea);
drop function app.finish_login(text, text, bytea, bytea, text, interval);
drop function app.start_login(text, text, bytea);
drop table sessions, login_codes;
-- +goose StatementEnd
