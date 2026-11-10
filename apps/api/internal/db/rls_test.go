package db

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// CI gate: cross-tenant and cross-role isolation. Needs TEST_DATABASE_URL (see docker-compose.yml).
func TestRLS(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	mig, err := os.ReadFile("../../migrations/00001_core.sql")
	if err != nil {
		t.Fatal(err)
	}
	mustExec(t, pool, `drop schema if exists app cascade; drop schema public cascade; create schema public;`)
	mustExec(t, pool, strings.Split(string(mig), "-- +goose Down")[0])

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
	  insert into bookings (tenant_id, provider_id, customer_id, starts_at, ends_at, status) values
	    ('`+tA+`','`+pA+`','`+carol+`','2026-12-01 10:00+05:30','2026-12-01 10:30+05:30','confirmed'),
	    ('`+tB+`','`+pB+`','`+bob+`','2026-12-01 11:00+05:30','2026-12-01 11:30+05:30','confirmed');
	  insert into notes (tenant_id, author_id, subject_user_id, visibility, body_enc) values
	    ('`+tA+`','`+doc+`','`+carol+`','private','x'),('`+tA+`','`+doc+`','`+carol+`','shared','y');`)

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
	}
	for _, c := range cases {
		if got := count(c.s, c.q); got != c.want {
			t.Errorf("%s: got %d want %d", c.name, got, c.want)
		}
	}

	// Member can't create a booking for someone else.
	err = InTenant(ctx, pool, Scope{tA, dave}, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `insert into bookings (tenant_id, provider_id, customer_id, starts_at, ends_at)
		  values ($1,$2,$3,'2026-12-02 10:00+05:30','2026-12-02 10:30+05:30')`, tA, pA, carol)
		return err
	})
	if !isCode(err, "42501") {
		t.Errorf("booking for another user: want RLS violation, got %v", err)
	}

	// Can't attach another tenant's provider.
	err = InTenant(ctx, pool, Scope{tA, dave}, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `insert into bookings (tenant_id, provider_id, customer_id, starts_at, ends_at)
		  values ($1,$2,$3,'2026-12-03 10:00+05:30','2026-12-03 10:30+05:30')`, tA, pB, dave)
		return err
	})
	if !isCode(err, "23503") {
		t.Errorf("cross-tenant provider: want FK violation, got %v", err)
	}

	// Double booking is rejected by the exclusion constraint.
	err = InTenant(ctx, pool, Scope{tA, dave}, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `insert into bookings (tenant_id, provider_id, customer_id, starts_at, ends_at)
		  values ($1,$2,$3,'2026-12-01 10:15+05:30','2026-12-01 10:45+05:30')`, tA, pA, dave)
		return err
	})
	if !isCode(err, "23P01") {
		t.Errorf("overlapping booking: want exclusion violation, got %v", err)
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
