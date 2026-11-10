-- +goose Up
-- Self-serve space creation for onboarding. tenants has no insert policy, and a membership needs an
-- existing owner, so creating the first tenant + owner row goes through a SECURITY DEFINER function.
-- +goose StatementBegin
alter table tenants add column focus text[] not null default '{}'
  check (focus <@ array['courses','appointments']);

-- Creates a tenant owned by the current user (app.user_id()). Returns its id.
create function app.create_tenant(p_name text, p_slug text, p_focus text[]) returns uuid
language plpgsql security definer set search_path = public as $f$
declare tid uuid;
begin
  if app.user_id() is null then
    raise exception 'no user in scope' using errcode = '42501';
  end if;
  insert into tenants (name, slug, focus) values (p_name, p_slug, p_focus) returning id into tid;
  insert into memberships (tenant_id, user_id, role) values (tid, app.user_id(), 'owner');
  return tid;
end $f$;

-- The current user's spaces across tenants (RLS on tenants only shows the one in scope).
create function app.my_tenants() returns table (id uuid, slug text, name text, role text, focus text[])
language sql stable security definer set search_path = public as $f$
  select t.id, t.slug, t.name, m.role, t.focus
    from memberships m join tenants t on t.id = m.tenant_id
   where m.user_id = app.user_id()
   order by m.created_at
$f$;

grant execute on all functions in schema app to app_user;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
drop function app.my_tenants();
drop function app.create_tenant(text, text, text[]);
alter table tenants drop column focus;
-- +goose StatementEnd
