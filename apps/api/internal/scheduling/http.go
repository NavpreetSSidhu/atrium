package scheduling

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/NavpreetSSidhu/atrium/apps/api/internal/db"
	"github.com/NavpreetSSidhu/atrium/apps/api/internal/httpx"
)

// Routes registers scheduling endpoints. All need a signed-in user and an X-Tenant-ID.
func (s *Scheduler) Routes(mux *http.ServeMux, authn func(http.Handler) http.Handler) {
	h := func(pattern string, fn func(http.ResponseWriter, *http.Request, db.Scope)) {
		mux.Handle(pattern, authn(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sc, _ := db.ScopeFrom(r.Context())
			if sc.TenantID == "" {
				httpx.Error(w, http.StatusBadRequest, "X-Tenant-ID header required")
				return
			}
			fn(w, r, sc)
		})))
	}

	h("GET /services", func(w http.ResponseWriter, r *http.Request, sc db.Scope) {
		v, err := s.Services(r.Context(), sc)
		write(w, http.StatusOK, v, err)
	})
	saveService := func(status int) func(http.ResponseWriter, *http.Request, db.Scope) {
		return func(w http.ResponseWriter, r *http.Request, sc db.Scope) {
			var in Service
			if !httpx.ReadJSON(w, r, &in) {
				return
			}
			in.ID = r.PathValue("id")
			v, err := s.SaveService(r.Context(), sc, in)
			write(w, status, v, err)
		}
	}
	h("POST /services", saveService(http.StatusCreated))
	h("PUT /services/{id}", saveService(http.StatusOK))

	h("GET /providers", func(w http.ResponseWriter, r *http.Request, sc db.Scope) {
		v, err := s.Providers(r.Context(), sc)
		write(w, http.StatusOK, v, err)
	})
	h("PUT /providers/me", func(w http.ResponseWriter, r *http.Request, sc db.Scope) {
		var in struct{ Title, Timezone string }
		if !httpx.ReadJSON(w, r, &in) {
			return
		}
		v, err := s.BecomeProvider(r.Context(), sc, in.Title, in.Timezone)
		write(w, http.StatusOK, v, err)
	})
	h("GET /providers/{id}/availability", func(w http.ResponseWriter, r *http.Request, sc db.Scope) {
		v, err := s.GetAvailability(r.Context(), sc, r.PathValue("id"))
		write(w, http.StatusOK, v, err)
	})
	h("PUT /providers/{id}/availability", func(w http.ResponseWriter, r *http.Request, sc db.Scope) {
		var in AvailabilityDoc
		if !httpx.ReadJSON(w, r, &in) {
			return
		}
		if err := s.SetAvailability(r.Context(), sc, r.PathValue("id"), in); err != nil {
			write(w, 0, nil, err)
			return
		}
		v, err := s.GetAvailability(r.Context(), sc, r.PathValue("id"))
		write(w, http.StatusOK, v, err)
	})

	h("GET /slots", func(w http.ResponseWriter, r *http.Request, sc db.Scope) {
		q := r.URL.Query()
		v, err := s.AvailableSlots(r.Context(), sc, q.Get("service_id"), q.Get("provider_id"), q.Get("from"), q.Get("to"))
		write(w, http.StatusOK, v, err)
	})

	h("GET /bookings", func(w http.ResponseWriter, r *http.Request, sc db.Scope) {
		from, err1 := time.Parse(time.RFC3339, r.URL.Query().Get("from"))
		to, err2 := time.Parse(time.RFC3339, r.URL.Query().Get("to"))
		if err1 != nil || err2 != nil {
			write(w, 0, nil, ErrBadInput)
			return
		}
		v, err := s.Bookings(r.Context(), sc, from, to)
		write(w, http.StatusOK, v, err)
	})
	h("POST /bookings", func(w http.ResponseWriter, r *http.Request, sc db.Scope) {
		var in struct {
			ServiceID  string    `json:"service_id"`
			ProviderID string    `json:"provider_id"`
			StartsAt   time.Time `json:"starts_at"`
		}
		if !httpx.ReadJSON(w, r, &in) {
			return
		}
		v, err := s.Book(r.Context(), sc, in.ServiceID, in.ProviderID, in.StartsAt)
		write(w, http.StatusCreated, v, err)
	})
	h("POST /bookings/{id}/cancel", func(w http.ResponseWriter, r *http.Request, sc db.Scope) {
		if err := s.Cancel(r.Context(), sc, r.PathValue("id")); err != nil {
			write(w, 0, nil, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	h("POST /bookings/{id}/reschedule", func(w http.ResponseWriter, r *http.Request, sc db.Scope) {
		var in struct {
			StartsAt time.Time `json:"starts_at"`
		}
		if !httpx.ReadJSON(w, r, &in) {
			return
		}
		v, err := s.Reschedule(r.Context(), sc, r.PathValue("id"), in.StartsAt)
		write(w, http.StatusOK, v, err)
	})
}

func write(w http.ResponseWriter, status int, v any, err error) {
	switch {
	case err == nil:
		httpx.JSON(w, status, v)
	case errors.Is(err, ErrBadInput):
		httpx.Error(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, ErrForbidden):
		httpx.Error(w, http.StatusForbidden, err.Error())
	case errors.Is(err, ErrNotFound):
		httpx.Error(w, http.StatusNotFound, err.Error())
	case errors.Is(err, ErrSlotTaken), errors.Is(err, ErrTooLate):
		httpx.Error(w, http.StatusConflict, err.Error())
	default:
		slog.Error("scheduling", "err", err)
		httpx.Error(w, http.StatusInternalServerError, "internal error")
	}
}
