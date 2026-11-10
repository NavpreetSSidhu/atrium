package db

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NavpreetSSidhu/atrium/apps/api/internal/db/dbtest"
)

// CI gate: cross-tenant and cross-role isolation. Needs TEST_DATABASE_URL (see docker-compose.yml).
func TestRLS(t *testing.T) {
	pool := dbtest.New(t, "db")
	ctx := context.Background()

	const (
		tA, tB                  = "00000000-0000-0000-0000-00000000000a", "00000000-0000-0000-0000-00000000000b"
		alice, bob, carol, dave = "00000000-0000-0000-0000-0000000000a1", "00000000-0000-0000-0000-0000000000b1", "00000000-0000-0000-0000-0000000000c1", "00000000-0000-0000-0000-0000000000d1"
		doc, pA, pB             = "00000000-0000-0000-0000-0000000000e1", "00000000-0000-0000-0000-0000000000f1", "00000000-0000-0000-0000-0000000000f2"
	)
	// Seed as the migration role (bypasses RLS).
	mustExec(t, pool, `
	  insert into users (id, name) values
	    ('`+alice+`','alice'),('`+bob+`','bob'),('`+carol+`','carol'),('`+dave+`','dave'),('`+doc+`','doc');
	  insert into tenants (id, slug, name) values ('`+tA+`','clinic-a','A'),('`+tB+`','academy-b','B');
	  insert into memberships values
	    ('`+tA+`','`+alice+`','owner'),('`+tA+`','`+carol+`','member'),('`+tA+`','`+dave+`','member'),
	    ('`+tA+`','`+doc+`','provider'),('`+tB+`','`+bob+`','owner'),('`+tB+`','`+doc+`','provider');
	  insert into providers (id, tenant_id, user_id) values ('`+pA+`','`+tA+`','`+doc+`'),('`+pB+`','`+tB+`','`+doc+`');
	  insert into bookings (tenant_id, provider_id, customer_id, starts_at, ends_at, blocked_until, status) values
	    ('`+tA+`','`+pA+`','`+carol+`','2026-12-01 10:00+05:30','2026-12-01 10:30+05:30','2026-12-01 10:40+05:30','confirmed'),
	    ('`+tB+`','`+pB+`','`+bob+`','2026-12-01 11:00+05:30','2026-12-01 11:30+05:30','2026-12-01 11:30+05:30','confirmed');
	  insert into offerings (tenant_id, type, title, status) values
	    ('`+tA+`','service','Consult','published'),('`+tA+`','service','Secret draft','draft');
	  insert into availability_rules (tenant_id, provider_id, weekday, start_min, end_min) values ('`+tA+`','`+pA+`',1,540,1020);
	  insert into notes (tenant_id, author_id, subject_user_id, visibility, body_enc) values
	    ('`+tA+`','`+doc+`','`+carol+`','private','x'),('`+tA+`','`+doc+`','`+carol+`','shared','y');
	  insert into sessions (user_id, refresh_hash, expires_at) values ('`+doc+`','h', now() + interval '1 day');`)

	count := func(s Scope, q string) (n int) {
		t.Helper()
		if err := InTenant(ctx, pool, s, func(tx pgx.Tx) error { return tx.QueryRow(ctx, q).Scan(&n) }); err != nil {
			t.Fatal(err)
		}
		return n
	}
	cases := []struct {
		name string
		s    Scope
		q    string
		want int
	}{
		{"owner sees all tenant bookings", Scope{tA, alice}, "select count(*) from bookings", 1},
		{"owner can't see other tenant", Scope{tB, alice}, "select count(*) from bookings", 0},
		{"other owner can't read A by spoofing tenant", Scope{tA, bob}, "select count(*) from bookings", 0},
		{"member sees own booking", Scope{tA, carol}, "select count(*) from bookings", 1},
		{"other member sees nothing", Scope{tA, dave}, "select count(*) from bookings", 0},
		{"provider sees only current tenant", Scope{tA, doc}, "select count(*) from bookings", 1},
		{"owner can't read private clinical notes", Scope{tA, alice}, "select count(*) from notes", 0},
		{"patient sees only shared note", Scope{tA, carol}, "select count(*) from notes", 1},
		{"author sees both notes", Scope{tA, doc}, "select count(*) from notes", 2},
		{"member can't list other members", Scope{tA, carol}, "select count(*) from memberships", 1},
		{"no scope, no rows", Scope{"", ""}, "select count(*) from tenants", 0},
		{"user sees own sessions", Scope{"", doc}, "select count(*) from sessions", 1},
		{"owner can't see another user's sessions", Scope{tA, alice}, "select count(*) from sessions", 0},
		{"member sees only themself in users", Scope{tA, carol}, "select count(*) from users", 1},
		{"staff see the space's users", Scope{tA, alice}, "select count(*) from users", 4},
		{"provider sees self and own clients", Scope{tA, doc}, "select count(*) from users", 2},
		{"provider's clients stay in their tenant", Scope{tB, doc}, "select count(*) from users where id = '" + carol + "'", 0},
		{"member sees published offerings only", Scope{tA, carol}, "select count(*) from offerings", 1},
		{"staff see drafts too", Scope{tA, alice}, "select count(*) from offerings", 2},
		{"other tenant sees no offerings", Scope{tB, bob}, "select count(*) from offerings", 0},
		{"member reads availability to book", Scope{tA, carol}, "select count(*) from availability_rules", 1},
		{"busy() shows times to members", Scope{tA, dave}, "select count(*) from app.busy('" + pA + "', '2026-12-01', '2026-12-02')", 1},
		{"busy() hides times from non-members", Scope{tA, bob}, "select count(*) from app.busy('" + pA + "', '2026-12-01', '2026-12-02')", 0},
	}
	for _, c := range cases {
		if got := count(c.s, c.q); got != c.want {
			t.Errorf("%s: got %d want %d", c.name, got, c.want)
		}
	}

	// Member can't create a booking for someone else.
	err := InTenant(ctx, pool, Scope{tA, dave}, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `insert into bookings (tenant_id, provider_id, customer_id, starts_at, ends_at, blocked_until)
		  values ($1,$2,$3,'2026-12-02 10:00+05:30','2026-12-02 10:30+05:30','2026-12-02 10:30+05:30')`, tA, pA, carol)
		return err
	})
	if !isCode(err, "42501") {
		t.Errorf("booking for another user: want RLS violation, got %v", err)
	}

	// Can't attach another tenant's provider.
	err = InTenant(ctx, pool, Scope{tA, dave}, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `insert into bookings (tenant_id, provider_id, customer_id, starts_at, ends_at, blocked_until)
		  values ($1,$2,$3,'2026-12-03 10:00+05:30','2026-12-03 10:30+05:30','2026-12-03 10:30+05:30')`, tA, pB, dave)
		return err
	})
	if !isCode(err, "23503") {
		t.Errorf("cross-tenant provider: want FK violation, got %v", err)
	}

	// Double booking is rejected by the exclusion constraint, including the 10-minute buffer after carol's 10:00-10:30.
	for _, start := range []string{"2026-12-01 10:15+05:30", "2026-12-01 10:30+05:30"} {
		err = InTenant(ctx, pool, Scope{tA, dave}, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `insert into bookings (tenant_id, provider_id, customer_id, starts_at, ends_at, blocked_until)
			  values ($1,$2,$3,$4::timestamptz,$4::timestamptz + interval '30 min',$4::timestamptz + interval '30 min')`, tA, pA, dave, start)
			return err
		})
		if !isCode(err, "23P01") {
			t.Errorf("booking at %s: want exclusion violation, got %v", start, err)
		}
	}
	// Right after the buffer is free.
	err = InTenant(ctx, pool, Scope{tA, dave}, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `insert into bookings (tenant_id, provider_id, customer_id, starts_at, ends_at, blocked_until)
		  values ($1,$2,$3,'2026-12-01 10:40+05:30','2026-12-01 11:10+05:30','2026-12-01 11:10+05:30')`, tA, pA, dave)
		return err
	})
	if err != nil {
		t.Errorf("booking after buffer: %v", err)
	}

	// A member can't change a provider's availability or publish offerings.
	for name, q := range map[string]string{
		"member edits availability": "update availability_rules set end_min = 1200",
		"member creates offering":   "insert into offerings (tenant_id, type, title) values ('" + tA + "','service','x')",
	} {
		err = InTenant(ctx, pool, Scope{tA, carol}, func(tx pgx.Tx) error {
			tag, err := tx.Exec(ctx, q)
			if err == nil && tag.RowsAffected() > 0 {
				return errors.New("write succeeded")
			}
			return err
		})
		if err != nil && !isCode(err, "42501") {
			t.Errorf("%s: want RLS denial, got %v", name, err)
		}
	}

	// login_codes is reachable only through SECURITY DEFINER functions.
	err = InTenant(ctx, pool, Scope{tA, alice}, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "select 1 from login_codes")
		return err
	})
	if !isCode(err, "42501") {
		t.Errorf("login_codes direct read: want permission denied, got %v", err)
	}

	// Every public table must have forced RLS; new tables without it fail CI.
	var missing []string
	rows, _ := pool.Query(ctx, `select relname from pg_class c join pg_namespace n on n.oid = c.relnamespace
	  where n.nspname = 'public' and c.relkind = 'r' and not (c.relrowsecurity and c.relforcerowsecurity)`)
	missing, err = pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil || len(missing) > 0 {
		t.Errorf("tables without forced RLS: %v (err %v)", missing, err)
	}
}

func mustExec(t *testing.T, pool *pgxpool.Pool, sql string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), sql); err != nil {
		t.Fatal(err)
	}
}

func isCode(err error, code string) bool {
	var pe *pgconn.PgError
	return errors.As(err, &pe) && pe.Code == code
}
