# AGENTS.md

Instructions for AI coding agents (Claude Code, Codex, Cursor, etc.) and humans working in this repo.
`CLAUDE.md` imports this file; edit here only.

## What this is
Atrium: a multi-tenant SaaS (courses, live classes, appointments, storefronts, marketplace). India first.
- Product and phases: `docs/PRD.md`. Architecture decisions: `docs/ADR.md`. Read the relevant section before changing a module.
- Current phase and checklist: `README.md` → Status.

## Repo map
```
apps/api/            Go modular monolith (one module, binaries under cmd/)
  cmd/<binary>/      main packages only: wiring, no logic (api now; worker, realtime, migrate later)
  internal/db/       InTenant + Scope: the only way to run tenant-scoped SQL
  internal/identity/ login, sessions, access tokens, Authn middleware
  internal/tenancy/  spaces (tenants) and memberships
  internal/httpx/    shared JSON request/response helpers
  internal/<module>/ identity, tenancy, catalog, scheduling, rtc, commerce, ... (PRD §3)
  migrations/        goose SQL, numbered, append-only
apps/web/            Next.js: tenant sites, marketing, marketplace
apps/app/            Vite React PWA: dashboards, rooms, portals
packages/ui          shared React components
packages/api-client  generated TS client (never hand-edit)
packages/blocks      Puck block definitions (editor + renderer)
docs/                PRD, ADRs
```
Rule of thumb: `apps/*` are deployables, `packages/*` are shared TS code. Shared Go code lives in `apps/api/internal/` (Go has no need for a separate package dir until a second Go module exists).

## Commands
```sh
docker compose up -d                       # postgres :55432, redis :56379
cd apps/api && go vet ./... && go test ./... # needs TEST_DATABASE_URL for the RLS suite (see README)
pnpm install && pnpm lint && pnpm test && pnpm build   # JS workspaces via Turborepo (tests: node --test, no runner dep)
pnpm --filter @atrium/web dev              # Next.js :3000
pnpm --filter @atrium/app dev              # Vite :5173
```
`apps/web/AGENTS.md` is maintained by Next.js (`next dev` re-adds it): read it before touching `apps/web`, since Next 16 APIs differ from older training data.
Before you say "done": `go vet` and `go test` pass with `TEST_DATABASE_URL` set. A skipped RLS test is not a pass.

## Non-negotiable invariants
1. **Tenant isolation is enforced by Postgres RLS, not app code.** Every tenant-scoped query runs inside `db.InTenant`. Never query tenant tables on the raw pool from a request path.
2. `db.Scope.UserID` comes only from a verified access token (`identity.Authn`). `TenantID` may be client-selected (`X-Tenant-ID`) because RLS checks membership. Handlers read the scope with `db.ScopeFrom(ctx)`.
3. Every new table: `tenant_id uuid not null`, `enable` + `force row level security`, policies, and tenant-composite FKs `(tenant_id, x_id)` (FK checks bypass RLS). `rls_test.go` fails on any public table without forced RLS. Add cross-tenant cases for every new table. Integration tests get a fresh migrated database from `dbtest.New(t, "<pkg>")`.
4. Roles are read in the DB (`app.member_role()`), never trusted from the client.
5. Invariants that matter (no double booking, money) are DB constraints, not app checks.
6. Money: double-entry ledger is the source of truth; webhooks idempotent on provider event id; no stored balances.
7. Sensitive notes/intake/prescriptions: encrypted `bytea` columns (envelope encryption). Never log them.
8. Migrations are append-only. Never edit a migration that has been pushed; add a new one.

## Code structure (SOLID, the Go way)
- **Single responsibility:** one `internal/<module>` per bounded context from PRD §2. Handlers parse/validate and call a service; services hold logic; SQL lives in sqlc queries.
- **Open/closed + dependency inversion:** modules talk through small interfaces defined by the *consumer*, or through domain events on the outbox. Never import another module's internals.
- **Interface segregation:** interfaces of 1-3 methods, declared where used. No god interfaces, no interface for a single implementation unless a test needs it.
- **Liskov:** implementations (e.g. Razorpay now, Stripe later behind `commerce`) must honour the same contract, including errors and idempotency.
- Prefer stdlib (`net/http`, `log/slog`, `errgroup`). Add a dependency only when it removes real code.
- Errors: wrap with `fmt.Errorf("op: %w", err)`; map to HTTP status at the handler edge only.
- TS: strict mode (`tsconfig.base.json`), server data via the generated client, no hand-written fetch to the API.

## Workflow
- Spec first: a change in behaviour updates `docs/PRD.md`; a change in architecture adds an ADR to `docs/ADR.md`.
- Small, focused commits pushed straight to `main` (no PRs, solo project). Conventional prefixes: `feat:`, `fix:`, `chore:`, `docs:`, `test:`.
- Tick the `README.md` status checklist when a phase item lands.
- Never commit secrets or `.env` files.
