package scheduling

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/NavpreetSSidhu/atrium/apps/api/internal/db"
	"github.com/NavpreetSSidhu/atrium/apps/api/internal/db/dbtest"
)

// fakeAuthn trusts X-Test-User. Real auth is tested in package identity.
func fakeAuthn(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sc := db.Scope{UserID: r.Header.Get("X-Test-User"), TenantID: r.Header.Get("X-Tenant-ID")}
		next.ServeHTTP(w, r.WithContext(db.WithScope(r.Context(), sc)))
	})
}

func TestScheduling(t *testing.T) {
	pool := dbtest.New(t, "scheduling")
	const (
		tenant                       = "00000000-0000-0000-0000-00000000000a"
		owner, carol, dave, stranger = "00000000-0000-0000-0000-0000000000a1", "00000000-0000-0000-0000-0000000000c1", "00000000-0000-0000-0000-0000000000d1", "00000000-0000-0000-0000-0000000000e1"
	)
	for _, q := range []string{
		"insert into users (id, name) values ('" + owner + "','Dr. Rao'),('" + carol + "','Carol'),('" + dave + "','Dave'),('" + stranger + "','Stranger')",
		"insert into tenants (id, slug, name) values ('" + tenant + "','rao-clinic','Rao Clinic')",
		"insert into memberships values ('" + tenant + "','" + owner + "','owner'),('" + tenant + "','" + carol + "','member'),('" + tenant + "','" + dave + "','member')",
	} {
		if _, err := pool.Exec(t.Context(), q); err != nil {
			t.Fatal(err)
		}
	}
	ist, _ := time.LoadLocation("Asia/Kolkata")
	now := time.Date(2030, 1, 7, 8, 0, 0, 0, ist) // a Monday, 08:00
	mux := http.NewServeMux()
	(&Scheduler{Pool: pool, Now: func() time.Time { return now }}).Routes(mux, fakeAuthn)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	call := func(user, method, path string, body, out any) int {
		t.Helper()
		b, _ := json.Marshal(body)
		req, _ := http.NewRequest(method, srv.URL+path, bytes.NewReader(b))
		req.Header.Set("X-Test-User", user)
		req.Header.Set("X-Tenant-ID", tenant)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if out != nil {
			json.NewDecoder(res.Body).Decode(out)
		}
		return res.StatusCode
	}
	expect := func(name string, got, want int) {
		t.Helper()
		if got != want {
			t.Fatalf("%s: status %d, want %d", name, got, want)
		}
	}
	at := func(day, hhmm string) time.Time {
		v, _ := time.ParseInLocation("2006-01-02 15:04", day+" "+hhmm, ist)
		return v
	}
	slotsOf := func(user, service, provider, from, to string) (out []string) {
		t.Helper()
		var ts []time.Time
		expect("slots", call(user, "GET", "/slots?service_id="+service+"&provider_id="+provider+"&from="+from+"&to="+to, nil, &ts), 200)
		for _, v := range ts {
			out = append(out, v.In(ist).Format("01-02 15:04"))
		}
		return out
	}
	book := func(user, service, provider string, start time.Time) (int, Booking) {
		t.Helper()
		var b Booking
		st := call(user, "POST", "/bookings", map[string]any{"service_id": service, "provider_id": provider, "starts_at": start}, &b)
		return st, b
	}

	// Setup: the owner is the provider; members can't change setup.
	var prov Provider
	expect("owner becomes provider", call(owner, "PUT", "/providers/me", map[string]string{"title": "Dr. Rao", "timezone": "Asia/Kolkata"}, &prov), 200)
	expect("bad timezone", call(owner, "PUT", "/providers/me", map[string]string{"title": "x", "timezone": "Mars/Base"}, nil), 400)
	expect("member becomes provider", call(carol, "PUT", "/providers/me", map[string]string{"title": "x", "timezone": "Asia/Kolkata"}, nil), 403)

	svc := func(title string, price int64) map[string]any {
		return map[string]any{"title": title, "price_paise": price, "duration_min": 30, "buffer_min": 10, "location": "video",
			"min_notice_min": 60, "horizon_days": 30, "cancel_cutoff_hours": 24}
	}
	var free, paid Service
	expect("create free service", call(owner, "POST", "/services", svc("Consultation", 0), &free), 201)
	expect("create paid service", call(owner, "POST", "/services", svc("Procedure", 50000), &paid), 201)
	bad := svc("Too short", 0)
	bad["duration_min"] = 3
	expect("invalid duration", call(owner, "POST", "/services", bad, nil), 400)
	expect("member creates service", call(carol, "POST", "/services", svc("Nope", 0), nil), 403)
	draft := svc("Draft", 0)
	draft["status"] = "draft"
	var draftSvc Service
	expect("create draft", call(owner, "POST", "/services", draft, &draftSvc), 201)
	var seen []Service
	call(carol, "GET", "/services", nil, &seen)
	if len(seen) != 2 {
		t.Fatalf("member sees %d services, want 2 published", len(seen))
	}

	avail := map[string]any{
		"rules":     []map[string]int{{"weekday": 1, "start_min": 9 * 60, "end_min": 11 * 60}},
		"overrides": []map[string]any{{"date": "2030-01-14", "start_min": nil, "end_min": nil}},
	}
	expect("set availability", call(owner, "PUT", "/providers/"+prov.ID+"/availability", avail, nil), 200)
	expect("member sets availability", call(carol, "PUT", "/providers/"+prov.ID+"/availability", avail, nil), 403)
	expect("invalid hours", call(owner, "PUT", "/providers/"+prov.ID+"/availability",
		map[string]any{"rules": []map[string]int{{"weekday": 1, "start_min": 600, "end_min": 500}}}, nil), 400)

	// Slots: 1 h notice from 08:00, 30 min + 10 min buffer steps, 14th closed by override.
	want := []string{"01-07 09:00", "01-07 09:40", "01-07 10:20"}
	if got := slotsOf(carol, free.ID, prov.ID, "2030-01-07", "2030-01-14"); !equal(got, want) || len(got) != 3 {
		t.Fatalf("slots: %v want %v", got, want)
	}
	expect("draft service has no slots", call(carol, "GET", "/slots?service_id="+draftSvc.ID+"&provider_id="+prov.ID+"&from=2030-01-07&to=2030-01-07", nil, nil), 404)

	// Booking: free confirms; the same slot can't be taken twice; only offered slots are bookable.
	st, b := book(carol, free.ID, prov.ID, at("2030-01-07", "09:40"))
	expect("carol books", st, 201)
	if b.Status != "confirmed" || !b.Mine || b.ServiceTitle != "Consultation" {
		t.Fatalf("carol's booking: %+v", b)
	}
	st, _ = book(dave, free.ID, prov.ID, at("2030-01-07", "09:40"))
	expect("dave takes the same slot", st, 409)
	st, _ = book(dave, free.ID, prov.ID, at("2030-01-07", "10:25")) // free time, but not on the slot grid
	expect("off-grid time", st, 409)
	st, _ = book(dave, free.ID, prov.ID, at("2030-01-07", "08:30"))
	expect("inside minimum notice", st, 409)
	st, daves := book(dave, free.ID, prov.ID, at("2030-01-07", "09:00"))
	expect("dave books 09:00", st, 201)

	// Paid: held for checkout, and an expired hold frees the slot.
	st, held := book(dave, paid.ID, prov.ID, at("2030-01-07", "10:20"))
	expect("dave holds paid slot", st, 201)
	if held.Status != "held" || held.HoldExpiresAt == nil {
		t.Fatalf("paid booking: %+v", held)
	}
	if got := slotsOf(carol, free.ID, prov.ID, "2030-01-07", "2030-01-07"); len(got) != 0 {
		t.Fatalf("held slot still offered: %v", got)
	}
	if _, err := pool.Exec(t.Context(), "update bookings set hold_expires_at = now() - interval '1 minute' where id = $1", held.ID); err != nil {
		t.Fatal(err)
	}
	if got := slotsOf(carol, free.ID, prov.ID, "2030-01-07", "2030-01-07"); !equal(got, []string{"01-07 10:20"}) || len(got) != 1 {
		t.Fatalf("expired hold not freed: %v", got)
	}
	st, _ = book(carol, paid.ID, prov.ID, at("2030-01-07", "10:20"))
	expect("carol books the released slot", st, 201)

	// Who sees what.
	var list []Booking
	from, to := url.QueryEscape(at("2030-01-01", "00:00").Format(time.RFC3339)), url.QueryEscape(at("2030-02-01", "00:00").Format(time.RFC3339))
	expect("carol lists", call(carol, "GET", "/bookings?from="+from+"&to="+to, nil, &list), 200)
	if len(list) != 2 {
		t.Fatalf("carol sees %d bookings, want her 2", len(list))
	}
	call(owner, "GET", "/bookings?from="+from+"&to="+to, nil, &list)
	if len(list) != 3 || list[0].CustomerName != "Dave" {
		t.Fatalf("owner's schedule: %+v", list)
	}
	expect("stranger can't see services", call(stranger, "GET", "/slots?service_id="+free.ID+"&provider_id="+prov.ID+"&from=2030-01-07&to=2030-01-07", nil, nil), 404)
	st, _ = book(stranger, free.ID, prov.ID, at("2030-01-07", "10:20"))
	expect("stranger books", st, 404)

	// Cancel: customers respect the 24 h cutoff; staff can always cancel.
	expect("carol cancels inside cutoff", call(carol, "POST", "/bookings/"+b.ID+"/cancel", nil, nil), 409)
	expect("dave cancels carol's booking", call(dave, "POST", "/bookings/"+b.ID+"/cancel", nil, nil), 404)
	expect("owner cancels", call(owner, "POST", "/bookings/"+b.ID+"/cancel", nil, nil), 204)
	expect("cancel twice", call(owner, "POST", "/bookings/"+b.ID+"/cancel", nil, nil), 404)
	st, _ = book(dave, free.ID, prov.ID, at("2030-01-07", "09:40"))
	expect("cancelled slot is bookable again", st, 201)
	_ = daves

	// Reschedule a booking next week (outside the cutoff).
	st, nb := book(carol, free.ID, prov.ID, at("2030-01-21", "09:00"))
	expect("carol books next week", st, 201)
	var moved Booking
	expect("reschedule", call(carol, "POST", "/bookings/"+nb.ID+"/reschedule", map[string]any{"starts_at": at("2030-01-21", "09:40")}, &moved), 200)
	if !moved.StartsAt.Equal(at("2030-01-21", "09:40")) || moved.Status != "confirmed" {
		t.Fatalf("moved: %+v", moved)
	}
	expect("reschedule off-grid", call(carol, "POST", "/bookings/"+nb.ID+"/reschedule", map[string]any{"starts_at": at("2030-01-21", "09:20")}, nil), 409)
	expect("reschedule back to the freed slot", call(carol, "POST", "/bookings/"+nb.ID+"/reschedule", map[string]any{"starts_at": at("2030-01-21", "09:00")}, nil), 200)
	if got := slotsOf(dave, free.ID, prov.ID, "2030-01-21", "2030-01-21"); !equal(got, []string{"01-21 09:40", "01-21 10:20"}) || len(got) != 2 {
		t.Fatalf("slots after reschedule: %v", got)
	}

	expect("missing tenant header", func() int {
		req, _ := http.NewRequest("GET", srv.URL+"/services", nil)
		req.Header.Set("X-Test-User", owner)
		res, _ := http.DefaultClient.Do(req)
		res.Body.Close()
		return res.StatusCode
	}(), 400)
}
