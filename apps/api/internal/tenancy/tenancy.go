// Package tenancy manages spaces (tenants) and the caller's memberships.
package tenancy

import (
	"cmp"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NavpreetSSidhu/atrium/apps/api/internal/db"
	"github.com/NavpreetSSidhu/atrium/apps/api/internal/httpx"
)

type Tenant struct {
	ID    string   `json:"id"`
	Slug  string   `json:"slug"`
	Name  string   `json:"name"`
	Role  string   `json:"role"`
	Focus []string `json:"focus"`
}

type Service struct {
	Pool *pgxpool.Pool
}

var (
	ErrBadInput  = errors.New("invalid input")
	ErrSlugTaken = errors.New("that address is taken")
	ErrNotFound  = errors.New("no such space")

	slugRe = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{1,38}[a-z0-9])$`)
	// Subdomains we serve ourselves; a tenant on one of these could phish our users.
	reserved = []string{"www", "app", "api", "admin", "auth", "mail", "help", "support", "status", "docs", "blog", "static", "cdn"}
	focuses  = []string{"courses", "appointments"}
)

// Create makes a space owned by the caller.
func (s *Service) Create(ctx context.Context, sc db.Scope, name, slug string, focus []string) (Tenant, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 80 || !slugRe.MatchString(slug) || slices.Contains(reserved, slug) {
		return Tenant{}, ErrBadInput
	}
	for _, f := range focus {
		if !slices.Contains(focuses, f) {
			return Tenant{}, ErrBadInput
		}
	}
	t := Tenant{Slug: slug, Name: name, Role: "owner", Focus: focus}
	if t.Focus == nil {
		t.Focus = []string{}
	}
	err := db.InTenant(ctx, s.Pool, sc, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "select app.create_tenant($1, $2, $3)::text", name, slug, t.Focus).Scan(&t.ID)
	})
	if pe := (*pgconn.PgError)(nil); errors.As(err, &pe) && pe.Code == "23505" {
		return Tenant{}, ErrSlugTaken
	}
	return t, err
}

// Mine lists the caller's spaces, oldest first.
func (s *Service) Mine(ctx context.Context, sc db.Scope) ([]Tenant, error) {
	out := []Tenant{}
	err := db.InTenant(ctx, s.Pool, sc, func(tx pgx.Tx) error {
		rows, _ := tx.Query(ctx, "select id::text, slug, name, role, focus from app.my_tenants()")
		var err error
		out, err = pgx.CollectRows(rows, pgx.RowToStructByPos[Tenant])
		return err
	})
	return out, err
}

// Join makes the caller a member of the space with this slug (from its booking page).
func (s *Service) Join(ctx context.Context, sc db.Scope, slug string) (Tenant, error) {
	var t Tenant
	err := db.InTenant(ctx, s.Pool, sc, func(tx pgx.Tx) error {
		var id *string
		if err := tx.QueryRow(ctx, "select app.join_tenant($1)::text", slug).Scan(&id); err != nil || id == nil {
			return cmp.Or(err, ErrNotFound)
		}
		rows, _ := tx.Query(ctx, "select id::text, slug, name, role, focus from app.my_tenants() where id = $1", *id)
		var err error
		t, err = pgx.CollectExactlyOneRow(rows, pgx.RowToStructByPos[Tenant])
		return err
	})
	return t, err
}

// Routes registers the tenancy endpoints behind authn (identity.Authn in production).
func (s *Service) Routes(mux *http.ServeMux, authn func(http.Handler) http.Handler) {
	mux.Handle("GET /tenants", authn(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sc, _ := db.ScopeFrom(r.Context())
		list, err := s.Mine(r.Context(), sc)
		write(w, http.StatusOK, list, err)
	})))
	mux.Handle("POST /tenants/join", authn(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Slug string }
		if !httpx.ReadJSON(w, r, &in) {
			return
		}
		sc, _ := db.ScopeFrom(r.Context())
		t, err := s.Join(r.Context(), sc, in.Slug)
		write(w, http.StatusOK, t, err)
	})))
	mux.Handle("POST /tenants", authn(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Name, Slug string
			Focus      []string
		}
		if !httpx.ReadJSON(w, r, &in) {
			return
		}
		sc, _ := db.ScopeFrom(r.Context())
		t, err := s.Create(r.Context(), sc, in.Name, in.Slug, in.Focus)
		write(w, http.StatusCreated, t, err)
	})))
}

func write(w http.ResponseWriter, status int, v any, err error) {
	switch {
	case err == nil:
		httpx.JSON(w, status, v)
	case errors.Is(err, ErrBadInput):
		httpx.Error(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, ErrNotFound):
		httpx.Error(w, http.StatusNotFound, err.Error())
	case errors.Is(err, ErrSlugTaken):
		httpx.Error(w, http.StatusConflict, err.Error())
	default:
		slog.Error("tenancy", "err", err)
		httpx.Error(w, http.StatusInternalServerError, "internal error")
	}
}
