package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NavpreetSSidhu/atrium/apps/api/internal/identity"
	"github.com/NavpreetSSidhu/atrium/apps/api/internal/scheduling"
	"github.com/NavpreetSSidhu/atrium/apps/api/internal/tenancy"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		slog.Error("db", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	secret := []byte(os.Getenv("AUTH_JWT_SECRET"))
	if len(secret) < 32 {
		slog.Error("AUTH_JWT_SECRET must be at least 32 bytes")
		os.Exit(1)
	}
	// shortcut: only the dev log sender exists; production needs MSG91 (SMS) and SES (email) senders first.
	if os.Getenv("APP_ENV") == "production" {
		slog.Error("no SMS/email sender configured for production")
		os.Exit(1)
	}
	appURL := os.Getenv("APP_URL")
	if appURL == "" {
		appURL = "http://localhost:5173"
	}
	auth := &identity.Service{Pool: pool, Sender: identity.LogSender{}, Secret: secret, AppURL: appURL,
		SecureCookie: !strings.HasPrefix(appURL, "http://")}
	tenants := &tenancy.Service{Pool: pool}

	mux := http.NewServeMux()
	auth.Routes(mux)
	tenants.Routes(mux, auth.Authn)
	(&scheduling.Scheduler{Pool: pool, Now: time.Now}).Routes(mux, auth.Authn)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		if err := pool.Ping(r.Context()); err != nil {
			http.Error(w, "db down", http.StatusServiceUnavailable)
			return
		}
		w.Write([]byte("ok"))
	})

	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = ":8080"
	}
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		srv.Shutdown(shutdown)
	}()
	slog.Info("api listening", "addr", addr)
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		slog.Error("server", "err", err)
		os.Exit(1)
	}
}
