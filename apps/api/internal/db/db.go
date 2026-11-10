// Package db runs every tenant-scoped query inside a transaction that carries
// the request scope, so Postgres RLS (not app code) enforces isolation.
package db

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Scope is the request identity RLS evaluates. UserID must come from a verified session token,
// never from client input. TenantID may be client-selected: RLS checks membership via app.member_role().
type Scope struct {
	TenantID string
	UserID   string
}

type scopeKey struct{}

// WithScope stores the authenticated scope on the request context (set by identity.Authn).
func WithScope(ctx context.Context, s Scope) context.Context {
	return context.WithValue(ctx, scopeKey{}, s)
}

// ScopeFrom returns the scope set by WithScope.
func ScopeFrom(ctx context.Context) (Scope, bool) {
	s, ok := ctx.Value(scopeKey{}).(Scope)
	return s, ok
}

// InTenant runs fn in a tx scoped to s. SET LOCAL is safe under PgBouncer transaction pooling.
func InTenant(ctx context.Context, pool *pgxpool.Pool, s Scope, fn func(pgx.Tx) error) error {
	return pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		// Drop to app_user even if the DSN is privileged, so a misconfigured deploy can't bypass RLS.
		if _, err := tx.Exec(ctx, "set local role app_user"); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			"select set_config('app.tenant_id', $1, true), set_config('app.user_id', $2, true)",
			s.TenantID, s.UserID); err != nil {
			return err
		}
		return fn(tx)
	})
}
