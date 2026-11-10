package identity

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/NavpreetSSidhu/atrium/apps/api/internal/db"
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
	if !readJSON(w, r, &in) {
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
	if !readJSON(w, r, &in) {
		return
	}
	t, err := s.FinishLogin(r.Context(), in.Channel, in.Dest, in.Code, r.UserAgent())
	writeResult(w, t, err)
}

func (s *Service) handleRefresh(w http.ResponseWriter, r *http.Request) {
	var in struct {
		RefreshToken string `json:"refresh_token"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	t, err := s.Refresh(r.Context(), in.RefreshToken)
	writeResult(w, t, err)
}

func (s *Service) handleLogout(w http.ResponseWriter, r *http.Request) {
	sc, _ := db.ScopeFrom(r.Context())
	if err := s.Revoke(r.Context(), sc, r.Context().Value(sidKey{}).(string)); err != nil {
		writeErr(w, err)
		return
	}
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

func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(v); err != nil {
		http.Error(w, `{"error":"invalid JSON body"}`, http.StatusBadRequest)
		return false
	}
	return true
}

func writeResult(w http.ResponseWriter, v any, err error) {
	if err != nil {
		writeErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
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
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

func isUUID(s string) bool {
	var u pgtype.UUID
	return u.Scan(s) == nil
}
