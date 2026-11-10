package identity

import (
	"cmp"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/NavpreetSSidhu/atrium/apps/api/internal/db"
	"github.com/NavpreetSSidhu/atrium/apps/api/internal/httpx"
)

// Routes registers the auth endpoints (see PRD M1).
func (s *Service) Routes(mux *http.ServeMux) {
	mux.HandleFunc("POST /auth/start", s.handleStart)
	mux.HandleFunc("POST /auth/verify", s.handleVerify)
	mux.HandleFunc("POST /auth/refresh", s.handleRefresh)
	mux.Handle("POST /auth/logout", s.Authn(http.HandlerFunc(s.handleLogout)))
	mux.Handle("GET /auth/sessions", s.Authn(http.HandlerFunc(s.handleSessions)))
	mux.Handle("DELETE /auth/sessions/{id}", s.Authn(http.HandlerFunc(s.handleRevoke)))
	mux.Handle("GET /me", s.Authn(http.HandlerFunc(s.handleMe)))
	mux.Handle("PATCH /me", s.Authn(http.HandlerFunc(s.handleSetName)))
}

const refreshCookie = "atrium_refresh"

// setRefresh sets (maxAge > 0) or clears (maxAge < 0) the refresh cookie.
func (s *Service) setRefresh(w http.ResponseWriter, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{Name: refreshCookie, Value: value, Path: "/", MaxAge: maxAge,
		HttpOnly: true, Secure: s.SecureCookie, SameSite: http.SameSiteStrictMode})
}

type sidKey struct{}

// Authn verifies the Bearer access token and puts db.Scope on the context. X-Tenant-ID is
// optional; RLS decides whether the user may see that tenant.
func (s *Service) Authn(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		c, err := s.parse(token)
		if !ok || err != nil {
			writeErr(w, ErrInvalidAuth)
			return
		}
		if live, err := s.live(r.Context(), c.Subject, c.SID); err != nil || !live {
			writeErr(w, cmp.Or(err, ErrInvalidAuth))
			return
		}
		sc := db.Scope{UserID: c.Subject, TenantID: r.Header.Get("X-Tenant-ID")}
		if sc.TenantID != "" && !isUUID(sc.TenantID) {
			writeErr(w, ErrBadInput)
			return
		}
		ctx := context.WithValue(db.WithScope(r.Context(), sc), sidKey{}, c.SID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Service) handleStart(w http.ResponseWriter, r *http.Request) {
	var in struct{ Channel, Dest string }
	if !httpx.ReadJSON(w, r, &in) {
		return
	}
	if err := s.StartLogin(r.Context(), in.Channel, in.Dest); err != nil {
		writeErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) handleVerify(w http.ResponseWriter, r *http.Request) {
	var in struct{ Channel, Dest, Code string }
	if !httpx.ReadJSON(w, r, &in) {
		return
	}
	t, err := s.FinishLogin(r.Context(), in.Channel, in.Dest, in.Code, r.UserAgent())
	if err == nil {
		s.setRefresh(w, t.refresh, int(refreshTTL.Seconds()))
	}
	writeResult(w, t, err)
}

func (s *Service) handleRefresh(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie(refreshCookie)
	if err != nil {
		writeErr(w, ErrInvalidAuth)
		return
	}
	t, err := s.Refresh(r.Context(), c.Value)
	switch {
	case err == nil:
		s.setRefresh(w, t.refresh, int(refreshTTL.Seconds()))
	case errors.Is(err, ErrInvalidAuth):
		s.setRefresh(w, "", -1)
	}
	writeResult(w, t, err)
}

func (s *Service) handleLogout(w http.ResponseWriter, r *http.Request) {
	sc, _ := db.ScopeFrom(r.Context())
	if err := s.Revoke(r.Context(), sc, r.Context().Value(sidKey{}).(string)); err != nil {
		writeErr(w, err)
		return
	}
	s.setRefresh(w, "", -1)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) handleSessions(w http.ResponseWriter, r *http.Request) {
	sc, _ := db.ScopeFrom(r.Context())
	list, err := s.Sessions(r.Context(), sc, r.Context().Value(sidKey{}).(string))
	writeResult(w, list, err)
}

func (s *Service) handleRevoke(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !isUUID(id) {
		writeErr(w, ErrBadInput)
		return
	}
	sc, _ := db.ScopeFrom(r.Context())
	if err := s.Revoke(r.Context(), sc, id); err != nil {
		writeErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) handleMe(w http.ResponseWriter, r *http.Request) {
	sc, _ := db.ScopeFrom(r.Context())
	var me struct {
		ID    string `json:"id"`
		Phone string `json:"phone,omitempty"`
		Email string `json:"email,omitempty"`
		Name  string `json:"name"`
	}
	err := db.InTenant(r.Context(), s.Pool, sc, func(tx pgx.Tx) error {
		return tx.QueryRow(r.Context(), `select id::text, coalesce(phone, ''), coalesce(email, ''), name
		  from users where id = app.user_id()`).Scan(&me.ID, &me.Phone, &me.Email, &me.Name)
	})
	writeResult(w, me, err)
}

func (s *Service) handleSetName(w http.ResponseWriter, r *http.Request) {
	var in struct{ Name string }
	if !httpx.ReadJSON(w, r, &in) {
		return
	}
	sc, _ := db.ScopeFrom(r.Context())
	if err := s.SetName(r.Context(), sc, in.Name); err != nil {
		writeErr(w, err)
		return
	}
	s.handleMe(w, r)
}

func writeResult(w http.ResponseWriter, v any, err error) {
	if err != nil {
		writeErr(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, v)
}

func writeErr(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, ErrBadInput):
		status = http.StatusBadRequest
	case errors.Is(err, ErrInvalidCode), errors.Is(err, ErrInvalidAuth):
		status = http.StatusUnauthorized
	case errors.Is(err, ErrNotFound):
		status = http.StatusNotFound
	case errors.Is(err, ErrRateLimited):
		status = http.StatusTooManyRequests
	default:
		slog.Error("identity", "err", err)
		err = errors.New("internal error")
	}
	httpx.Error(w, status, err.Error())
}

func isUUID(s string) bool {
	var u pgtype.UUID
	return u.Scan(s) == nil
}
