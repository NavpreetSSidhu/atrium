# Architecture decisions

## ADR-1 Go modular monolith, not microservices
One repo with binaries `api`, `worker`, `realtime` and `migrate`, and strict `internal/<module>` packages that talk through interfaces and an outbox. A module gets extracted only after it shows a measured hot spot. *Why:* a team of about 9 can't run 12 services, and the module boundaries keep extraction cheap later.

## ADR-2 Shared-schema multi-tenancy, isolation enforced by Postgres RLS
- Every tenant table has `tenant_id` and **FORCE RLS**. The app runs as `app_user` (no BYPASSRLS).
- `db.InTenant` does `SET LOCAL ROLE app_user` plus `app.tenant_id`/`app.user_id` per transaction. Roles are read from `memberships` inside the DB (`app.member_role()`) and never trusted from the client.
- Foreign keys are **tenant-composite** (`(tenant_id, x_id)`) because FK checks bypass RLS.
- `internal/db/rls_test.go` is a CI gate. It also fails if any public table lacks forced RLS.

## ADR-3 Self-hosted LiveKit for meetings and live streaming
LiveKit is an open-source Go SFU with Egress (recording, HLS), Ingress (RTMP/WHIP) and built-in TURN. It runs on a dedicated EKS node group with host networking in ap-south-1. LiveKit Cloud is the drop-in fallback because it uses the same SDK. Rooms with 100 or fewer people run on the SFU directly; anything larger uses HLS through the CDN.

## ADR-4 Razorpay Route as the payment rail (India first)
Payments split straight to each seller's linked account, so the platform never holds seller funds (RBI PA rules). An internal double-entry ledger is the source of truth. Webhooks are idempotent on the provider event id, and a daily reconciliation job checks the ledger. Stripe Connect comes later behind the same `commerce` interface.

## ADR-5 Next.js for public sites, Vite React SPA for the app
- Tenant sites and the marketplace need SSR/ISR, custom domains and SEO, so they run on Next.js (host-based tenant resolution, Cloudflare for SaaS hostnames).
- Dashboards and rooms are interactive and authenticated, so they run as a Vite SPA shipped as a PWA.
- Both use one generated OpenAPI TypeScript client.

## ADR-6 Sessions: stateless access JWT, rotating refresh token in Postgres
- Access token: HS256 JWT (`sub` = user, `sid` = session), 15 minutes, verified without a DB hit. A revoked session keeps working until its access token expires; that 15-minute window is accepted.
- Refresh token: 32 random bytes, stored only as a SHA-256 hash in `sessions`. Every refresh rotates it. A rotated-out token coming back means it leaked, so the session is revoked.
- Login codes and session creation run only through `SECURITY DEFINER` functions (`app.start_login`, `app.finish_login`, `app.refresh_session`). `login_codes` has no grants for `app_user` at all, and a session can only be created by proving a code. The app stays on `app_user` and keeps forced RLS on every table.
- The scope's `UserID` comes only from a verified JWT. `TenantID` is requested by the client (`X-Tenant-ID`) and enforced by RLS through `app.member_role()`.
- *Why:* no extra session store, stolen refresh tokens are detectable, and auth can't become a way around RLS.
