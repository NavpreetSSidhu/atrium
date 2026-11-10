package identity

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
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
	base, _ := url.Parse(srv.URL)

	// browser returns a client with its own cookie jar, like a browser profile.
	browser := func() *http.Client {
		jar, _ := cookiejar.New(nil)
		return &http.Client{Jar: jar}
	}
	cookieOf := func(c *http.Client) string {
		for _, ck := range c.Jar.Cookies(base) {
			if ck.Name == refreshCookie {
				return ck.Value
			}
		}
		return ""
	}
	call := func(c *http.Client, method, path, bearer string, body any) (int, map[string]any) {
		t.Helper()
		b, _ := json.Marshal(body)
		req, _ := http.NewRequest(method, srv.URL+path, bytes.NewReader(b))
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}
		res, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var out map[string]any
		if path != "/auth/sessions" {
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
	login := func(c *http.Client, channel, dest string) string {
		t.Helper()
		st, _ := call(c, "POST", "/auth/start", "", map[string]string{"channel": channel, "dest": dest})
		expect("start "+dest, st, 204)
		st, tok := call(c, "POST", "/auth/verify", "", map[string]string{"channel": channel, "dest": dest, "code": sender.code(strings.ToLower(dest))})
		expect("verify "+dest, st, 200)
		if _, leaked := tok["refresh_token"]; leaked || cookieOf(c) == "" {
			t.Fatalf("refresh token must be in the httpOnly cookie only: body %v", tok)
		}
		return tok["access_token"].(string)
	}
	const phone = "+919876543210"
	anon := browser()

	st, _ := call(anon, "POST", "/auth/start", "", map[string]string{"channel": "sms", "dest": "98765"})
	expect("non-E.164 phone", st, 400)
	st, _ = call(anon, "POST", "/auth/start", "", map[string]string{"channel": "fax", "dest": phone})
	expect("unknown channel", st, 400)

	// 5 wrong guesses kill the code, even if the right one comes next.
	st, _ = call(anon, "POST", "/auth/start", "", map[string]string{"channel": "sms", "dest": phone})
	expect("start", st, 204)
	good := sender.code(phone)
	for i := range 5 {
		st, _ = call(anon, "POST", "/auth/verify", "", map[string]string{"channel": "sms", "dest": phone, "code": fmt.Sprintf("x%05d", i)})
		expect("wrong code", st, 401)
	}
	st, _ = call(anon, "POST", "/auth/verify", "", map[string]string{"channel": "sms", "dest": phone, "code": good})
	expect("right code after lockout", st, 401)

	// Login, access, profile, and a code can't be used twice.
	b := browser()
	access := login(b, "sms", phone)
	st, me := call(b, "GET", "/me", access, nil)
	expect("me", st, 200)
	if me["phone"] != phone {
		t.Fatalf("me: got %v", me)
	}
	st, me = call(b, "PATCH", "/me", access, map[string]string{"name": "  Asha  "})
	expect("set name", st, 200)
	if me["name"] != "Asha" {
		t.Fatalf("set name: got %v", me)
	}
	st, _ = call(b, "PATCH", "/me", access, map[string]string{"name": " "})
	expect("blank name", st, 400)
	st, _ = call(anon, "POST", "/auth/verify", "", map[string]string{"channel": "sms", "dest": phone, "code": sender.code(phone)})
	expect("code reuse", st, 401)
	st, _ = call(anon, "GET", "/me", "", nil)
	expect("me without token", st, 401)
	st, _ = call(anon, "GET", "/me", access+"x", nil)
	expect("me with tampered token", st, 401)

	// Refresh rotates; replaying the old cookie revokes the session and its access tokens at once.
	stolen := cookieOf(b)
	st, tok := call(b, "POST", "/auth/refresh", "", nil)
	expect("refresh", st, 200)
	access = tok["access_token"].(string)
	if cookieOf(b) == stolen {
		t.Fatal("refresh did not rotate the cookie")
	}
	thief := browser()
	thief.Jar.SetCookies(base, []*http.Cookie{{Name: refreshCookie, Value: stolen}})
	st, _ = call(thief, "POST", "/auth/refresh", "", nil)
	expect("replay old refresh", st, 401)
	st, _ = call(b, "POST", "/auth/refresh", "", nil)
	expect("new refresh after theft detected", st, 401)
	st, _ = call(b, "GET", "/me", access, nil)
	expect("access token of revoked session", st, 401)
	st, _ = call(anon, "POST", "/auth/refresh", "", nil)
	expect("refresh without cookie", st, 401)

	// Email magic link; logout ends the session immediately.
	e := browser()
	access = login(e, "email", "Asha@Example.com")
	if !strings.HasPrefix(sender.last["asha@example.com"], "http://app/auth/verify?") {
		t.Fatalf("magic link: %q", sender.last["asha@example.com"])
	}
	st, _ = call(e, "POST", "/auth/logout", access, nil)
	expect("logout", st, 204)
	st, _ = call(e, "GET", "/me", access, nil)
	expect("access token after logout", st, 401)
	if cookieOf(e) != "" {
		t.Fatal("logout did not clear the refresh cookie")
	}

	// Sessions are private to their user.
	alice, bob := browser(), browser()
	aliceAccess, bobAccess := login(alice, "email", "alice@example.com"), login(bob, "email", "bob@example.com")
	var sessions []Session
	req, _ := http.NewRequest("GET", srv.URL+"/auth/sessions", nil)
	req.Header.Set("Authorization", "Bearer "+aliceAccess)
	res, err := alice.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	json.NewDecoder(res.Body).Decode(&sessions)
	res.Body.Close()
	if len(sessions) != 1 || !sessions[0].Current {
		t.Fatalf("alice sessions: %+v", sessions)
	}
	st, _ = call(bob, "DELETE", "/auth/sessions/"+sessions[0].ID, bobAccess, nil)
	expect("bob revokes alice's session", st, 404)
	st, _ = call(alice, "DELETE", "/auth/sessions/"+sessions[0].ID, aliceAccess, nil)
	expect("alice revokes own session", st, 204)

	// Rate limit: 5 codes per destination per hour.
	for range 5 {
		st, _ = call(anon, "POST", "/auth/start", "", map[string]string{"channel": "email", "dest": "spam@example.com"})
		expect("start under limit", st, 204)
	}
	st, _ = call(anon, "POST", "/auth/start", "", map[string]string{"channel": "email", "dest": "spam@example.com"})
	expect("start over limit", st, 429)
}
