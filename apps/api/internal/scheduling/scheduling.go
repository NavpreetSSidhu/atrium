// Package scheduling holds services, provider availability, slot generation and appointments (PRD M4).
package scheduling

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NavpreetSSidhu/atrium/apps/api/internal/db"
)

const holdTTL = "10 minutes" // checkout hold for paid services (PRD M4)

var (
	ErrBadInput  = errors.New("invalid input")
	ErrNotFound  = errors.New("not found")
	ErrForbidden = errors.New("not allowed")
	ErrSlotTaken = errors.New("that time is no longer available")
	ErrTooLate   = errors.New("too close to the appointment to change it")
)

type Scheduler struct {
	Pool *pgxpool.Pool
	Now  func() time.Time // injectable for tests
}

type Service struct {
	ID                string `json:"id"`
	Title             string `json:"title"`
	Description       string `json:"description"`
	PricePaise        int64  `json:"price_paise"`
	Status            string `json:"status"`
	DurationMin       int    `json:"duration_min"`
	BufferMin         int    `json:"buffer_min"`
	Location          string `json:"location"`
	MinNoticeMin      int    `json:"min_notice_min"`
	HorizonDays       int    `json:"horizon_days"`
	CancelCutoffHours int    `json:"cancel_cutoff_hours"`
}

type Provider struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Timezone string `json:"timezone"`
	Me       bool   `json:"me"`
}

type Rule struct {
	Weekday int `json:"weekday"`
	Hours
}

type Override struct {
	Date     string `json:"date"`
	StartMin *int   `json:"start_min"`
	EndMin   *int   `json:"end_min"`
}

type AvailabilityDoc struct {
	Rules     []Rule     `json:"rules"`
	Overrides []Override `json:"overrides"`
}

type Booking struct {
	ID            string     `json:"id"`
	ServiceID     *string    `json:"service_id"`
	ServiceTitle  string     `json:"service_title"`
	ProviderID    string     `json:"provider_id"`
	ProviderTitle string     `json:"provider_title"`
	CustomerName  string     `json:"customer_name"`
	Mine          bool       `json:"mine"`
	StartsAt      time.Time  `json:"starts_at"`
	EndsAt        time.Time  `json:"ends_at"`
	Status        string     `json:"status"`
	HoldExpiresAt *time.Time `json:"hold_expires_at"`
}

// pgErr maps database errors to domain errors; RLS and constraints are the source of truth.
func pgErr(err error) error {
	var pe *pgconn.PgError
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return ErrNotFound
	case !errors.As(err, &pe):
		return err
	case pe.Code == "42501":
		return ErrForbidden
	case pe.Code == "23P01":
		return ErrSlotTaken
	case pe.Code == "23514", pe.Code == "23502", pe.Code == "23503", strings.HasPrefix(pe.Code, "22"):
		return ErrBadInput
	}
	return err
}

func (s *Scheduler) tx(ctx context.Context, sc db.Scope, fn func(pgx.Tx) error) error {
	return pgErr(db.InTenant(ctx, s.Pool, sc, fn))
}

const serviceCols = `o.id::text, o.title, o.description, o.price_paise, o.status, s.duration_min, s.buffer_min,
  s.location, s.min_notice_min, s.horizon_days, s.cancel_cutoff_hours
  from offerings o join services s on s.offering_id = o.id`

func scanService(row pgx.Row) (v Service, err error) {
	err = row.Scan(&v.ID, &v.Title, &v.Description, &v.PricePaise, &v.Status, &v.DurationMin, &v.BufferMin,
		&v.Location, &v.MinNoticeMin, &v.HorizonDays, &v.CancelCutoffHours)
	return v, err
}

// SaveService creates (empty ID) or updates a service. Staff only (RLS).
func (s *Scheduler) SaveService(ctx context.Context, sc db.Scope, v Service) (Service, error) {
	v.Title = strings.TrimSpace(v.Title)
	if v.Status == "" {
		v.Status = "published"
	}
	err := s.tx(ctx, sc, func(tx pgx.Tx) error {
		if v.ID == "" {
			if err := tx.QueryRow(ctx, `insert into offerings (tenant_id, type, title, description, price_paise, status)
			  values (app.tenant_id(), 'service', $1, $2, $3, $4) returning id::text`,
				v.Title, v.Description, v.PricePaise, v.Status).Scan(&v.ID); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `insert into services (offering_id, tenant_id, duration_min, buffer_min, location,
			  min_notice_min, horizon_days, cancel_cutoff_hours) values ($1, app.tenant_id(), $2, $3, $4, $5, $6, $7)`,
				v.ID, v.DurationMin, v.BufferMin, v.Location, v.MinNoticeMin, v.HorizonDays, v.CancelCutoffHours)
			return err
		}
		tag, err := tx.Exec(ctx, `update offerings set title = $2, description = $3, price_paise = $4, status = $5
		  where id = $1 and type = 'service'`, v.ID, v.Title, v.Description, v.PricePaise, v.Status)
		if err == nil && tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `update services set duration_min = $2, buffer_min = $3, location = $4, min_notice_min = $5,
		  horizon_days = $6, cancel_cutoff_hours = $7 where offering_id = $1`,
			v.ID, v.DurationMin, v.BufferMin, v.Location, v.MinNoticeMin, v.HorizonDays, v.CancelCutoffHours)
		return err
	})
	return v, err
}

// Services lists non-archived services. Members only see published ones (RLS).
func (s *Scheduler) Services(ctx context.Context, sc db.Scope) ([]Service, error) {
	out := []Service{}
	err := s.tx(ctx, sc, func(tx pgx.Tx) error {
		rows, _ := tx.Query(ctx, "select "+serviceCols+" where o.status <> 'archived' order by o.created_at")
		var err error
		out, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (Service, error) { return scanService(r) })
		return err
	})
	return out, err
}

// BecomeProvider makes the caller a provider in the current space (or updates their profile). Staff only.
func (s *Scheduler) BecomeProvider(ctx context.Context, sc db.Scope, title, timezone string) (Provider, error) {
	if _, err := time.LoadLocation(timezone); err != nil || timezone == "" || timezone == "Local" {
		return Provider{}, ErrBadInput
	}
	p := Provider{Title: strings.TrimSpace(title), Timezone: timezone, Me: true}
	err := s.tx(ctx, sc, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `insert into providers (tenant_id, user_id, title, timezone)
		  values (app.tenant_id(), app.user_id(), $1, $2)
		  on conflict (tenant_id, user_id) do update set title = excluded.title, timezone = excluded.timezone
		  returning id::text`, p.Title, p.Timezone).Scan(&p.ID)
	})
	return p, err
}

func (s *Scheduler) Providers(ctx context.Context, sc db.Scope) ([]Provider, error) {
	out := []Provider{}
	err := s.tx(ctx, sc, func(tx pgx.Tx) error {
		rows, _ := tx.Query(ctx, `select id::text, title, timezone, user_id = app.user_id() from providers order by title, id`)
		var err error
		out, err = pgx.CollectRows(rows, pgx.RowToStructByPos[Provider])
		return err
	})
	return out, err
}

func (s *Scheduler) GetAvailability(ctx context.Context, sc db.Scope, providerID string) (AvailabilityDoc, error) {
	doc := AvailabilityDoc{Rules: []Rule{}, Overrides: []Override{}}
	err := s.tx(ctx, sc, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, "select 1 from providers where id = $1", providerID).Scan(new(int)); err != nil {
			return err
		}
		rows, _ := tx.Query(ctx, `select weekday, start_min, end_min from availability_rules
		  where provider_id = $1 order by weekday, start_min`, providerID)
		var err error
		if doc.Rules, err = pgx.CollectRows(rows, pgx.RowToStructByPos[Rule]); err != nil {
			return err
		}
		rows, _ = tx.Query(ctx, `select to_char(date, 'YYYY-MM-DD'), start_min, end_min from availability_overrides
		  where provider_id = $1 order by date, start_min`, providerID)
		doc.Overrides, err = pgx.CollectRows(rows, pgx.RowToStructByPos[Override])
		return err
	})
	return doc, err
}

// SetAvailability replaces a provider's weekly rules and overrides. Staff or the provider themself (RLS).
func (s *Scheduler) SetAvailability(ctx context.Context, sc db.Scope, providerID string, doc AvailabilityDoc) error {
	return s.tx(ctx, sc, func(tx pgx.Tx) error {
		// RLS hides rows the caller can't manage, so check the delete reached the provider's data.
		var allowed bool
		if err := tx.QueryRow(ctx, `select app.is_staff() or exists(select 1 from providers
		  where id = $1 and user_id = app.user_id())`, providerID).Scan(&allowed); err != nil {
			return err
		}
		if !allowed {
			return ErrForbidden
		}
		for _, q := range []string{"delete from availability_rules where provider_id = $1", "delete from availability_overrides where provider_id = $1"} {
			if _, err := tx.Exec(ctx, q, providerID); err != nil {
				return err
			}
		}
		for _, r := range doc.Rules {
			if _, err := tx.Exec(ctx, `insert into availability_rules (tenant_id, provider_id, weekday, start_min, end_min)
			  values (app.tenant_id(), $1, $2, $3, $4)`, providerID, r.Weekday, r.Start, r.End); err != nil {
				return err
			}
		}
		for _, o := range doc.Overrides {
			if _, err := tx.Exec(ctx, `insert into availability_overrides (tenant_id, provider_id, date, start_min, end_min)
			  values (app.tenant_id(), $1, $2::date, $3, $4)`, providerID, o.Date, o.StartMin, o.EndMin); err != nil {
				return err
			}
		}
		return nil
	})
}

// loadAvailability reads a provider's timezone, rules and overrides into the slot engine's form.
func loadAvailability(ctx context.Context, tx pgx.Tx, providerID string) (*time.Location, Availability, error) {
	a := Availability{Overrides: map[string][]Hours{}}
	var tz string
	if err := tx.QueryRow(ctx, "select timezone from providers where id = $1", providerID).Scan(&tz); err != nil {
		return nil, a, err
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return nil, a, err
	}
	rows, _ := tx.Query(ctx, "select weekday, start_min, end_min from availability_rules where provider_id = $1", providerID)
	var r Rule
	if _, err := pgx.ForEachRow(rows, []any{&r.Weekday, &r.Start, &r.End}, func() error {
		a.Week[r.Weekday] = append(a.Week[r.Weekday], r.Hours)
		return nil
	}); err != nil {
		return nil, a, err
	}
	rows, _ = tx.Query(ctx, `select to_char(date, 'YYYY-MM-DD'), start_min, end_min from availability_overrides
	  where provider_id = $1`, providerID)
	var o Override
	_, err = pgx.ForEachRow(rows, []any{&o.Date, &o.StartMin, &o.EndMin}, func() error {
		day := a.Overrides[o.Date]
		if o.StartMin != nil {
			day = append(day, Hours{*o.StartMin, *o.EndMin})
		}
		a.Overrides[o.Date] = day // present even when empty: the day is closed
		return nil
	})
	return loc, a, err
}

// slotsIn computes slots for local dates [from, to] for a service with a provider, inside tx.
func (s *Scheduler) slotsIn(ctx context.Context, tx pgx.Tx, svc Service, providerID string, from, to string) ([]time.Time, error) {
	loc, a, err := loadAvailability(ctx, tx, providerID)
	if err != nil {
		return nil, err
	}
	f, err1 := time.ParseInLocation(time.DateOnly, from, loc)
	t, err2 := time.ParseInLocation(time.DateOnly, to, loc)
	if err1 != nil || err2 != nil || t.Before(f) || t.Sub(f) > 62*24*time.Hour {
		return nil, ErrBadInput
	}
	rows, _ := tx.Query(ctx, "select starts_at, blocked_until from app.busy($1, $2, $3)", providerID, f, t.AddDate(0, 0, 1))
	busy, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Busy])
	if err != nil {
		return nil, err
	}
	now := s.Now()
	return Slots(loc, a, f, t, SlotRule{
		Duration:  time.Duration(svc.DurationMin) * time.Minute,
		Buffer:    time.Duration(svc.BufferMin) * time.Minute,
		NotBefore: now.Add(time.Duration(svc.MinNoticeMin) * time.Minute),
		NotAfter:  now.AddDate(0, 0, svc.HorizonDays),
	}, busy), nil
}

func getService(ctx context.Context, tx pgx.Tx, id string) (Service, error) {
	return scanService(tx.QueryRow(ctx, "select "+serviceCols+" where o.id = $1 and o.status = 'published'", id))
}

// AvailableSlots lists bookable start times for local dates [from, to] (YYYY-MM-DD, at most ~2 months).
func (s *Scheduler) AvailableSlots(ctx context.Context, sc db.Scope, serviceID, providerID, from, to string) ([]time.Time, error) {
	out := []time.Time{}
	err := s.tx(ctx, sc, func(tx pgx.Tx) error {
		svc, err := getService(ctx, tx, serviceID)
		if err != nil {
			return err
		}
		slots, err := s.slotsIn(ctx, tx, svc, providerID, from, to)
		out = append(out, slots...)
		return err
	})
	return out, err
}

// offered reports whether start is a slot the server would offer right now (on its local date).
func (s *Scheduler) offered(ctx context.Context, tx pgx.Tx, svc Service, providerID string, start time.Time) (bool, error) {
	var tz string
	if err := tx.QueryRow(ctx, "select timezone from providers where id = $1", providerID).Scan(&tz); err != nil {
		return false, err
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return false, err
	}
	day := start.In(loc).Format(time.DateOnly)
	slots, err := s.slotsIn(ctx, tx, svc, providerID, day, day)
	return slices.ContainsFunc(slots, start.Equal), err
}

// Book reserves a slot for the caller. Free services confirm at once; paid ones are held for checkout.
func (s *Scheduler) Book(ctx context.Context, sc db.Scope, serviceID, providerID string, start time.Time) (Booking, error) {
	var id string
	err := s.tx(ctx, sc, func(tx pgx.Tx) error {
		svc, err := getService(ctx, tx, serviceID)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "select app.release_expired_holds($1)", providerID); err != nil {
			return err
		}
		if ok, err := s.offered(ctx, tx, svc, providerID, start); err != nil || !ok {
			return cmp.Or(err, ErrSlotTaken)
		}
		end := start.Add(time.Duration(svc.DurationMin) * time.Minute)
		// shortcut: paid bookings stay 'held' until the payments module confirms them (PRD M8).
		return tx.QueryRow(ctx, `insert into bookings (tenant_id, provider_id, service_id, customer_id, starts_at, ends_at,
		    blocked_until, status, hold_expires_at)
		  values (app.tenant_id(), $1, $2, app.user_id(), $3, $4, $5,
		    case when $6 = 0 then 'confirmed' else 'held' end,
		    case when $6 = 0 then null else now() + interval '`+holdTTL+`' end)
		  returning id::text`,
			providerID, serviceID, start, end, end.Add(time.Duration(svc.BufferMin)*time.Minute), svc.PricePaise).Scan(&id)
	})
	if err != nil {
		return Booking{}, err
	}
	return s.booking(ctx, sc, id)
}

const bookingCols = `b.id::text, b.service_id::text, coalesce(o.title, ''), b.provider_id::text, p.title, coalesce(u.name, ''),
  b.customer_id = app.user_id(), b.starts_at, b.ends_at, b.status, b.hold_expires_at
  from bookings b join providers p on p.id = b.provider_id
  left join offerings o on o.id = b.service_id left join users u on u.id = b.customer_id`

func (s *Scheduler) booking(ctx context.Context, sc db.Scope, id string) (Booking, error) {
	var b Booking
	err := s.tx(ctx, sc, func(tx pgx.Tx) error {
		rows, _ := tx.Query(ctx, "select "+bookingCols+" where b.id = $1", id)
		var err error
		b, err = pgx.CollectExactlyOneRow(rows, pgx.RowToStructByPos[Booking])
		return err
	})
	return b, err
}

// Bookings lists live bookings starting in [from, to). RLS decides whose: staff all, providers theirs,
// customers their own.
func (s *Scheduler) Bookings(ctx context.Context, sc db.Scope, from, to time.Time) ([]Booking, error) {
	out := []Booking{}
	err := s.tx(ctx, sc, func(tx pgx.Tx) error {
		rows, _ := tx.Query(ctx, "select "+bookingCols+` where b.starts_at >= $1 and b.starts_at < $2
		  and b.status <> 'cancelled' and not (b.status = 'held' and b.hold_expires_at <= now())
		  order by b.starts_at`, from, to)
		var err error
		out, err = pgx.CollectRows(rows, pgx.RowToStructByPos[Booking])
		return err
	})
	return out, err
}

// lockForChange loads a booking for cancel/reschedule and enforces the cutoff for customers.
// Staff and the booking's provider may change it at any time.
func (s *Scheduler) lockForChange(ctx context.Context, tx pgx.Tx, id string) (b struct {
	ServiceID, ProviderID, Status string
	StartsAt                      time.Time
}, err error) {
	var privileged bool
	var cutoffHours int
	err = tx.QueryRow(ctx, `select coalesce(b.service_id::text, ''), b.provider_id::text, b.status, b.starts_at,
	    app.is_staff() or p.user_id = app.user_id(), coalesce(s.cancel_cutoff_hours, 0)
	  from bookings b join providers p on p.id = b.provider_id left join services s on s.offering_id = b.service_id
	  where b.id = $1 for update of b`, id).Scan(&b.ServiceID, &b.ProviderID, &b.Status, &b.StartsAt, &privileged, &cutoffHours)
	if err != nil {
		return b, err
	}
	if b.Status == "cancelled" || b.Status == "completed" {
		return b, ErrNotFound
	}
	if !privileged && !s.Now().Before(b.StartsAt.Add(-time.Duration(cutoffHours)*time.Hour)) {
		return b, ErrTooLate
	}
	return b, nil
}

func (s *Scheduler) Cancel(ctx context.Context, sc db.Scope, id string) error {
	return s.tx(ctx, sc, func(tx pgx.Tx) error {
		if _, err := s.lockForChange(ctx, tx, id); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, "update bookings set status = 'cancelled' where id = $1", id)
		return err
	})
}

// Reschedule moves a booking to another offered slot with the same provider and service.
func (s *Scheduler) Reschedule(ctx context.Context, sc db.Scope, id string, start time.Time) (Booking, error) {
	err := s.tx(ctx, sc, func(tx pgx.Tx) error {
		b, err := s.lockForChange(ctx, tx, id)
		if err != nil {
			return err
		}
		svc, err := getService(ctx, tx, b.ServiceID)
		if err != nil {
			return err
		}
		// Step out of the way so the booking's own time doesn't count as busy, then move it.
		if _, err := tx.Exec(ctx, "update bookings set status = 'cancelled' where id = $1", id); err != nil {
			return err
		}
		if ok, err := s.offered(ctx, tx, svc, b.ProviderID, start); err != nil || !ok {
			return cmp.Or(err, ErrSlotTaken)
		}
		end := start.Add(time.Duration(svc.DurationMin) * time.Minute)
		_, err = tx.Exec(ctx, `update bookings set starts_at = $2, ends_at = $3, blocked_until = $4, status = $5 where id = $1`,
			id, start, end, end.Add(time.Duration(svc.BufferMin)*time.Minute), b.Status)
		return err
	})
	if err != nil {
		return Booking{}, err
	}
	return s.booking(ctx, sc, id)
}
