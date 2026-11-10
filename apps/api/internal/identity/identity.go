// Package identity handles login (SMS OTP, email magic link), sessions and access tokens. See ADR-6.
package identity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"net/mail"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NavpreetSSidhu/atrium/apps/api/internal/db"
)

const (
	accessTTL  = 15 * time.Minute
	refreshTTL = 30 * 24 * time.Hour
)

var (
	ErrBadInput    = errors.New("invalid input")
	ErrInvalidCode = errors.New("invalid or expired code")
	ErrRateLimited = errors.New("too many codes requested, try later")
	ErrInvalidAuth = errors.New("invalid session")
	ErrNotFound    = errors.New("session not found")
)

// Sender delivers a login message. Implementations: LogSender (dev), MSG91 and SES later.
type Sender interface {
	Send(ctx context.Context, channel, dest, msg string) error
}

// LogSender logs messages instead of delivering them. Dev only: it writes login codes to the log.
type LogSender struct{}

func (LogSender) Send(_ context.Context, channel, dest, msg string) error {
	slog.Warn("DEV login message, not delivered", "channel", channel, "dest", dest, "msg", msg)
	return nil
}

type Service struct {
	Pool   *pgxpool.Pool
	Sender Sender
	Secret []byte // HS256 key for access tokens, at least 32 bytes
	AppURL string // base URL for email magic links
	// SecureCookie marks the refresh cookie Secure. Off only for plain-http local dev.
	SecureCookie bool
}

// Tokens is the login/refresh result. The refresh token never goes in the body: it is set as an
// httpOnly cookie so page scripts (and XSS) can't read it.
type Tokens struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
	refresh     string
}

var e164 = regexp.MustCompile(`^\+[1-9][0-9]{7,14}$`)

func normalize(channel, dest string) (string, error) {
	switch channel {
	case "sms":
		if e164.MatchString(dest) {
			return dest, nil
		}
	case "email":
		d := strings.ToLower(strings.TrimSpace(dest))
		if a, err := mail.ParseAddress(d); err == nil && a.Address == d {
			return d, nil
		}
	}
	return "", ErrBadInput
}

// StartLogin sends a 6-digit SMS code or an email magic link to dest.
func (s *Service) StartLogin(ctx context.Context, channel, dest string) error {
	dest, err := normalize(channel, dest)
	if err != nil {
		return err
	}
	code, msg := randToken(), ""
	if channel == "sms" {
		n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
		if err != nil {
			return err
		}
		code = fmt.Sprintf("%06d", n)
		msg = "Your Atrium code is " + code
	} else {
		msg = s.AppURL + "/auth/verify?" + url.Values{"email": {dest}, "code": {code}}.Encode()
	}
	var ok bool
	if err := db.InTenant(ctx, s.Pool, db.Scope{}, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "select app.start_login($1, $2, $3)", channel, dest, hash(code)).Scan(&ok)
	}); err != nil {
		return err
	}
	if !ok {
		return ErrRateLimited
	}
	return s.Sender.Send(ctx, channel, dest, msg)
}

// FinishLogin checks the code, creates the user on first login and opens a session.
func (s *Service) FinishLogin(ctx context.Context, channel, dest, code, userAgent string) (Tokens, error) {
	dest, err := normalize(channel, dest)
	if err != nil {
		return Tokens{}, err
	}
	refresh := randToken()
	var uid, sid *string
	// The function returns nulls (not an error) on a wrong code, so the tx commits the attempt count.
	if err := db.InTenant(ctx, s.Pool, db.Scope{}, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "select user_id, session_id from app.finish_login($1, $2, $3, $4, $5, $6)",
			channel, dest, hash(code), hash(refresh), userAgent, refreshTTL).Scan(&uid, &sid)
	}); err != nil {
		return Tokens{}, err
	}
	if uid == nil {
		return Tokens{}, ErrInvalidCode
	}
	return s.tokens(*uid, *sid, refresh)
}

// Refresh rotates the refresh token and issues a new access token.
func (s *Service) Refresh(ctx context.Context, refresh string) (Tokens, error) {
	next := randToken()
	var uid, sid *string
	if err := db.InTenant(ctx, s.Pool, db.Scope{}, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "select user_id, session_id from app.refresh_session($1, $2)",
			hash(refresh), hash(next)).Scan(&uid, &sid)
	}); err != nil {
		return Tokens{}, err
	}
	if uid == nil {
		return Tokens{}, ErrInvalidAuth
	}
	return s.tokens(*uid, *sid, next)
}

type Session struct {
	ID         string    `json:"id"`
	UserAgent  string    `json:"user_agent"`
	CreatedAt  time.Time `json:"created_at"`
	LastUsedAt time.Time `json:"last_used_at"`
	Current    bool      `json:"current"`
}

// Sessions lists the caller's live sessions. RLS limits rows to sc.UserID.
func (s *Service) Sessions(ctx context.Context, sc db.Scope, currentSID string) ([]Session, error) {
	var out []Session
	err := db.InTenant(ctx, s.Pool, sc, func(tx pgx.Tx) error {
		rows, _ := tx.Query(ctx, `select id::text, user_agent, created_at, last_used_at from sessions
		  where revoked_at is null and expires_at > now() order by last_used_at desc`)
		var err error
		out, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (Session, error) {
			var x Session
			err := r.Scan(&x.ID, &x.UserAgent, &x.CreatedAt, &x.LastUsedAt)
			x.Current = x.ID == currentSID
			return x, err
		})
		return err
	})
	return out, err
}

// Revoke ends one of the caller's sessions. Returns ErrNotFound if it isn't theirs or is already gone.
func (s *Service) Revoke(ctx context.Context, sc db.Scope, sessionID string) error {
	return db.InTenant(ctx, s.Pool, sc, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, "update sessions set revoked_at = now() where id = $1 and revoked_at is null", sessionID)
		if err == nil && tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return err
	})
}

// SetName updates the caller's display name.
func (s *Service) SetName(ctx context.Context, sc db.Scope, name string) error {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 80 {
		return ErrBadInput
	}
	return db.InTenant(ctx, s.Pool, sc, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "update users set name = $1 where id = app.user_id()", name)
		return err
	})
}

// live reports whether the session behind an access token is still active, so revocation is immediate.
func (s *Service) live(ctx context.Context, uid, sid string) (bool, error) {
	var ok bool
	err := db.InTenant(ctx, s.Pool, db.Scope{UserID: uid}, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `select exists(select 1 from sessions
		  where id = $1 and revoked_at is null and expires_at > now())`, sid).Scan(&ok)
	})
	return ok, err
}

type claims struct {
	jwt.RegisteredClaims
	SID string `json:"sid"`
}

func (s *Service) tokens(uid, sid, refresh string) (Tokens, error) {
	now := time.Now()
	access, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims{jwt.RegisteredClaims{
		Subject:   uid,
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(accessTTL)),
	}, sid}).SignedString(s.Secret)
	return Tokens{access, int(accessTTL.Seconds()), refresh}, err
}

func (s *Service) parse(token string) (*claims, error) {
	c := &claims{}
	_, err := jwt.ParseWithClaims(token, c, func(*jwt.Token) (any, error) { return s.Secret, nil },
		jwt.WithValidMethods([]string{"HS256"}), jwt.WithExpirationRequired())
	return c, err
}

func randToken() string {
	return rand.Text() // 26 base32 chars, 130 bits, URL-safe
}

func hash(s string) []byte {
	h := sha256.Sum256([]byte(s))
	return h[:]
}
