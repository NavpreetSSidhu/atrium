package identity

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/NavpreetSSidhu/atrium/apps/api/internal/db/dbtest"
)

type capture struct {
	mu   sync.Mutex
	last map[string]string // dest -> message
}

func (c *capture) Send(_ context.Context, _, dest, msg string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.last[dest] = msg
	return nil
}

func (c *capture) code(dest string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	msg := c.last[dest]
	if u, err := url.Parse(msg); err == nil && u.Query().Get("code") != "" {
		return u.Query().Get("code")
	}
	return msg[len(msg)-6:]
}

func TestAuthFlow(t *testing.T) {
	pool := dbtest.New(t, "identity")
	sender := &capture{last: map[string]string{}}
	svc := &Service{Pool: pool, Sender: sender, Secret: []byte(strings.Repeat("k", 32)), AppURL: "http://app"}
	mux := http.NewServeMux()
	svc.Routes(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	call := func(method, path, bearer string, body any) (int, map[string]any) {
		t.Helper()
		b, _ := json.Marshal(body)
		req, _ := http.NewRequest(method, srv.URL+path, bytes.NewReader(b))
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var out map[string]any
		if strings.HasPrefix(res.Header.Get("Content-Type"), "application/json") && method+path != "GET/auth/sessions" {
			json.NewDecoder(res.Body).Decode(&out)
		}
		return res.StatusCode, out
	}
	expect := func(name string, got, want int) {
		t.Helper()
		if got != want {
			t.Fatalf("%s: status %d, want %d", name, got, want)
		}
	}
	login := func(channel, dest string) (access, refresh string) {
		t.Helper()
		st, _ := call("POST", "/auth/start", "", map[string]string{"channel": channel, "dest": dest})
		expect("start "+dest, st, 204)
		st, tok := call("POST", "/auth/verify", "", map[string]string{"channel": channel, "dest": dest, "code": sender.code(strings.ToLower(dest))})
		expect("verify "+dest, st, 200)
		return tok["access_token"].(string), tok["refresh_token"].(string)
	}
	const phone = "+919876543210"

	st, _ := call("POST", "/auth/start", "", map[string]string{"channel": "sms", "dest": "98765"})
	expect("non-E.164 phone", st, 400)
	st, _ = call("POST", "/auth/start", "", map[string]string{"channel": "fax", "dest": phone})
	expect("unknown channel", st, 400)

	// 5 wrong guesses kill the code, even if the right one comes next.
	st, _ = call("POST", "/auth/start", "", map[string]string{"channel": "sms", "dest": phone})
	expect("start", st, 204)
	good := sender.code(phone)
	for i := range 5 {
		st, _ = call("POST", "/auth/verify", "", map[string]string{"channel": "sms", "dest": phone, "code": fmt.Sprintf("x%05d", i)})
		expect("wrong code", st, 401)
	}
	st, _ = call("POST", "/auth/verify", "", map[string]string{"channel": "sms", "dest": phone, "code": good})
	expect("right code after lockout", st, 401)

	// Login, access, and a code can't be used twice.
	access, refresh := login("sms", phone)
	st, me := call("GET", "/me", access, nil)
	expect("me", st, 200)
	if me["phone"] != phone {
		t.Fatalf("me: got %v", me)
	}
	st, _ = call("POST", "/auth/verify", "", map[string]string{"channel": "sms", "dest": phone, "code": sender.code(phone)})
	expect("code reuse", st, 401)
	st, _ = call("GET", "/me", "", nil)
	expect("me without token", st, 401)
	st, _ = call("GET", "/me", access+"x", nil)
	expect("me with tampered token", st, 401)

	// Refresh rotates; replaying the old token revokes the session.
	st, tok := call("POST", "/auth/refresh", "", map[string]string{"refresh_token": refresh})
	expect("refresh", st, 200)
	st, _ = call("POST", "/auth/refresh", "", map[string]string{"refresh_token": refresh})
	expect("replay old refresh", st, 401)
	st, _ = call("POST", "/auth/refresh", "", map[string]string{"refresh_token": tok["refresh_token"].(string)})
	expect("new refresh after theft detected", st, 401)

	// Email magic link, logout ends the session.
	access, refresh = login("email", "Asha@Example.com")
	if !strings.HasPrefix(sender.last["asha@example.com"], "http://app/auth/verify?") {
		t.Fatalf("magic link: %q", sender.last["asha@example.com"])
	}
	st, _ = call("POST", "/auth/logout", access, nil)
	expect("logout", st, 204)
	st, _ = call("POST", "/auth/refresh", "", map[string]string{"refresh_token": refresh})
	expect("refresh after logout", st, 401)

	// Sessions are private to their user.
	aliceAccess, _ := login("email", "alice@example.com")
	bobAccess, _ := login("email", "bob@example.com")
	var sessions []Session
	req, _ := http.NewRequest("GET", srv.URL+"/auth/sessions", nil)
	req.Header.Set("Authorization", "Bearer "+aliceAccess)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	json.NewDecoder(res.Body).Decode(&sessions)
	res.Body.Close()
	if len(sessions) != 1 || !sessions[0].Current {
		t.Fatalf("alice sessions: %+v", sessions)
	}
	st, _ = call("DELETE", "/auth/sessions/"+sessions[0].ID, bobAccess, nil)
	expect("bob revokes alice's session", st, 404)
	st, _ = call("DELETE", "/auth/sessions/"+sessions[0].ID, aliceAccess, nil)
	expect("alice revokes own session", st, 204)

	// Rate limit: 5 codes per destination per hour.
	for range 5 {
		st, _ = call("POST", "/auth/start", "", map[string]string{"channel": "email", "dest": "spam@example.com"})
		expect("start under limit", st, 204)
	}
	st, _ = call("POST", "/auth/start", "", map[string]string{"channel": "email", "dest": "spam@example.com"})
	expect("start over limit", st, 429)
}
