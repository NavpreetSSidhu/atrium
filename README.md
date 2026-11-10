# Atrium

A multi-tenant SaaS for courses, live classes and meetings, service appointments, and storefronts with a marketplace. India first.

Built as a reference for **agentic coding**: specs in `docs/`, agent instructions in [`AGENTS.md`](AGENTS.md) (read by Claude Code via [`CLAUDE.md`](CLAUDE.md)), and invariants enforced by tests, not convention.

- Product spec and phases: [docs/PRD.md](docs/PRD.md)
- Architecture decisions: [docs/ADR.md](docs/ADR.md)

## Layout
| Path | What |
|---|---|
| `apps/api` | Go modular monolith (net/http, pgx, Postgres RLS) |
| `apps/web` | Next.js: tenant sites, marketing, marketplace |
| `apps/app` | Vite React PWA: dashboards, rooms, portals |
| `packages/*` | Shared TS: `ui`, `api-client` (generated), `blocks` (Puck) |
| `docs/` | PRD and ADRs |

pnpm workspaces + Turborepo for JS; the Go module is self-contained in `apps/api`.

## Dev
Requires Go 1.26+, Node 22+, pnpm 10, Docker.
```sh
docker compose up -d
cd apps/api
TEST_DATABASE_URL='postgres://postgres:dev@localhost:55432/atrium?sslmode=disable' go test ./...   # RLS gate
DATABASE_URL='postgres://postgres:dev@localhost:55432/atrium?sslmode=disable' go run ./cmd/api     # GET :8080/healthz
```
Migrations are in `apps/api/migrations` (goose format). Run them as a role with BYPASSRLS. The app connects as a login role granted `app_user`.

## Status: Phase 0 (foundations)
- [x] Specs: PRD, ADRs 1-5
- [x] Monorepo layout, agent instructions, CI (Go vet + RLS suite)
- [x] Go API skeleton (net/http, slog, graceful shutdown)
- [x] Core schema: users, tenants, memberships, providers, bookings, notes
- [x] RLS harness (`db.InTenant`) and cross-tenant/cross-role test suite
- [x] Double-booking exclusion constraint and tenant-composite FKs
- [ ] Specs still to write: RLS policy catalog, API outline (OpenAPI)
- [ ] Auth (OTP, Google, magic link) and session to `db.Scope` middleware
- [ ] River worker, OTel, sqlc, goose runner (`cmd/migrate`)
- [ ] Scaffold `apps/web`, `apps/app`, `packages/*`; generated API client
- [ ] Terraform/EKS, LiveKit cluster and `lk load-test`

## License
[MIT](LICENSE)
