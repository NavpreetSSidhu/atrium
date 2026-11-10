package tenancy

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/NavpreetSSidhu/atrium/apps/api/internal/db"
	"github.com/NavpreetSSidhu/atrium/apps/api/internal/db/dbtest"
)

// fakeAuthn trusts X-Test-User. Real auth (identity.Authn) is tested in package identity.
func fakeAuthn(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uid := r.Header.Get("X-Test-User")
		if uid == "" {
			http.Error(w, "", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r.WithContext(db.WithScope(r.Context(), db.Scope{UserID: uid})))
	})
}

func TestTenants(t *testing.T) {
	pool := dbtest.New(t, "tenancy")
	const alice, bob = "00000000-0000-0000-0000-0000000000a1", "00000000-0000-0000-0000-0000000000b1"
	if _, err := pool.Exec(t.Context(), "insert into users (id, name) values ($1, 'alice'), ($2, 'bob')", alice, bob); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	(&Service{Pool: pool}).Routes(mux, fakeAuthn)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	call := func(user, method string, body any, out any) int {
		t.Helper()
		b, _ := json.Marshal(body)
		req, _ := http.NewRequest(method, srv.URL+"/tenants", bytes.NewReader(b))
		if user != "" {
			req.Header.Set("X-Test-User", user)
		}
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
	space := func(name, slug string, focus ...string) map[string]any {
		return map[string]any{"name": name, "slug": slug, "focus": focus}
	}

	var created Tenant
	if st := call(alice, "POST", space("Asha's Clinic", "asha-clinic", "appointments"), &created); st != 201 {
		t.Fatalf("create: %d", st)
	}
	for name, c := range map[string]struct {
		body map[string]any
		want int
	}{
		"duplicate slug":   {space("Other", "asha-clinic"), 409},
		"reserved slug":    {space("App", "app"), 400},
		"bad slug":         {space("X", "Bad Slug"), 400},
		"short slug":       {space("X", "ab"), 400},
		"blank name":       {space("  ", "fine-slug"), 400},
		"unknown focus":    {space("X", "fine-slug", "crypto"), 400},
		"courses and appt": {space("Both", "both-things", "courses", "appointments"), 201},
	} {
		if st := call(alice, "POST", c.body, nil); st != c.want {
			t.Errorf("%s: status %d, want %d", name, st, c.want)
		}
	}
	if st := call("", "POST", space("Anon", "anon-space"), nil); st != 401 {
		t.Errorf("unauthenticated create: %d", st)
	}

	var mine []Tenant
	call(alice, "GET", nil, &mine)
	if len(mine) != 2 || mine[0].ID != created.ID || mine[0].Role != "owner" || mine[0].Focus[0] != "appointments" {
		t.Fatalf("alice's spaces: %+v", mine)
	}
	call(bob, "GET", nil, &mine)
	if len(mine) != 0 {
		t.Fatalf("bob sees alice's spaces: %+v", mine)
	}

	// Joining from a booking page makes bob a member, once.
	join := func(slug string) (int, Tenant) {
		var tn Tenant
		b, _ := json.Marshal(map[string]string{"slug": slug})
		req, _ := http.NewRequest("POST", srv.URL+"/tenants/join", bytes.NewReader(b))
		req.Header.Set("X-Test-User", bob)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		json.NewDecoder(res.Body).Decode(&tn)
		return res.StatusCode, tn
	}
	for range 2 {
		if st, tn := join("asha-clinic"); st != 200 || tn.Role != "member" || tn.ID != created.ID {
			t.Fatalf("join: %d %+v", st, tn)
		}
	}
	if st, _ := join("no-such-space"); st != 404 {
		t.Fatalf("join unknown: %d", st)
	}
	call(bob, "GET", nil, &mine)
	if len(mine) != 1 || mine[0].Role != "member" {
		t.Fatalf("bob after joining: %+v", mine)
	}
}
