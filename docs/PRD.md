# Codename "Atrium": product and engineering spec plus phased plan

## Context
The goal is a multi-tenant SaaS in the style of lockers.social (community, courses, events, live, paid memberships, per-tenant plans and fees), extended into a **services and commerce platform**:
- Creators and educators sell courses, live classes, cohorts and conferences.
- Professionals sell appointments: doctors (like Practo), lawyers, consultants. A cross-tenant **marketplace** handles discovery.
- Sellers run single storefronts or join a product marketplace. Last-mile delivery comes later.
- Every tenant gets an SEO-friendly website or CMS with legal and redirect pages.
- Real-time meetings (like Zoom or Meet) and live streaming are **must-haves**, plus notes.

**Decisions locked with the user:** India first (Razorpay, GST, DPDP). Self-host the video SFU from day one. React web app shipped as a PWA. MVP covers **both** courses/live and appointments.

**Stack:** Go backend, Next.js for public sites, a React (Vite) SPA for the app, PostgreSQL with RLS.

Greenfield. Nothing in the current Desktop codebase (Lawcus is Laravel) gets reused.

---

## 1. Personas and roles
| Persona | Role key | Does |
|---|---|---|
| Platform admin | `platform_admin` | KYC review, disputes, payouts, tenant support |
| Tenant owner | `owner` | Billing, plan, domain, payouts, everything |
| Tenant admin or staff | `admin`, `staff` | Content, schedule, CRM, orders |
| Provider | `provider` | A doctor, lawyer, instructor or host inside a tenant: own calendar, notes, sessions |
| Member or customer | `member` | Buys, books, attends, learns |
| Guest | anonymous | Browses sites and the marketplace, checks out as a guest (tickets) |

One global user account works across many tenants, as on Lockers. Roles are stored per tenant in `memberships`.

## 2. Product modules (functional spec)

**M1 Identity and tenancy**
- Sign-in by phone OTP (MSG91), email magic link or Google OAuth. Spaces (tenants), invites and role matrix.
- Sessions use a short-lived access JWT plus a rotating refresh token. Sessions can be listed and revoked.
- 2FA is required for owners and providers who handle health data.

**M2 Site builder / CMS (Next.js)**
- Block-based page editor using **Puck**, an open-source React visual editor, so we don't build our own.
- Pages are stored as versioned JSON with draft and publish states. Themes: presets, layouts, accent color, dark/light.
- Auto pages: home, offerings, provider profiles, booking page, store, blog.
- **Legal pages from templates:** Terms, Privacy (DPDP), Refund/Cancellation, Shipping, Contact, Grievance Officer. Razorpay KYC and the IT Rules require these.
- **Redirects:** a 301/302 table, link-in-bio, and app deep-link and short-link pages.
- **SEO:** SSR/ISR with on-demand revalidation, per-page meta and OG, `sitemap.xml`, `robots.txt`, canonical URLs.
- JSON-LD by type: `Course`, `Event`, `Product`, `Physician`, `LegalService`, `Organization`, `Review`.
- **Domains:** `{slug}.atrium.in` subdomains plus custom domains through **Cloudflare for SaaS** (custom hostnames with automatic TLS).

**M3 Catalog**
- One polymorphic `offerings` table. Types: `course`, `live_class`, `event`, `service` (an appointment), `membership`, `product_physical`, `product_digital`.
- Each type has its own detail table. Pricing covers free, one-time, subscription, pay-what-you-want and packages (for example 5 sessions).

**M4 Scheduling and appointments**
- Provider availability uses weekly rules plus date overrides, each stored with its timezone.
- Booking rules: buffers, minimum notice, maximum horizon, slot length per service. Locations: video, in-person or phone.
- **No double booking, enforced in the database:** `EXCLUDE USING gist (provider_id WITH =, tstzrange(start,end) WITH &&)`.
- Checkout places a 10-minute slot hold.
- Reschedule and cancel policies, with refund rules attached to each policy. Reminders 24h and 1h before, by WhatsApp, SMS and email.
- Two-way Google and Outlook calendar sync: push to the external calendar, and import busy blocks through watch webhooks.
- Intake forms per service, for example patient history or case details.

**M5 Real-time: meetings and live (self-hosted LiveKit, which is written in Go)**
- **Meetings:** 1:1 and group sessions with up to about 50 interactive participants.
  - Waiting room and lobby, host controls (mute, remove, lock), screen share, chat, raise hand.
  - Captured device permissions, plus a pre-join device check.
- **Live classes and webinars:** up to about 100 interactive participants run directly on the SFU.
  - Larger audiences use **LiveKit Egress to HLS** on the CDN, which can serve 10k+ viewers with 5–10 s latency.
  - Promote-to-stage lets a viewer join as a speaker.
- **RTMP/WHIP ingest** through LiveKit Ingress, so hosts can stream from OBS.
- **Recording** is written to S3 by Egress and attached to the session or lesson.
- Meeting links work only with a token. The Go API mints LiveKit tokens per role after checking entitlement (paid ticket, booked appointment, or enrolled learner).
- Usage metering tracks participant-minutes, egress-minutes and recording-GB for plan limits.

**M6 Notes**
- Session notes are private to the provider. Doctor clinical notes and lawyer case notes are encrypted at field level.
- Shared notes go to the client. Lesson notes come from the instructor, and learners keep personal notes on each lesson.
- Notes use a rich-text JSON format (TipTap).
- **Phase 2:** live collaborative notes using Yjs, an automatic transcript (self-hosted Whisper worker), and AI summaries and action items.

**M7 Courses (LMS)**
- Modules and lessons: video, file, text, live session links. Drip schedule and progress tracking.
- Quizzes, assignments and certificates (Phase 2).
- **Video pipeline:** upload to S3 by presigned multipart, transcode to HLS ladders with an ffmpeg worker, serve through signed CDN URLs.

**M8 Commerce and payments (money correctness is a P0)**
- Cart, checkout, orders and coupons.
- **Razorpay** handles orders, payments, Subscriptions (for memberships) and **Route**, which splits each payment to the seller's linked account. The platform never holds seller funds, which keeps it clear of the RBI payment-aggregator rules.
- Platform fee per plan (for example Free 0 sales, Basic 5%, Pro 1%), with refunds pro-rated.
- **Double-entry ledger** (`ledger_entries`) as the source of truth. Webhooks are idempotent, keyed on the provider event id.
- Daily reconciliation job.
- GST-compliant invoices (PDF). The platform's own SaaS invoices carry GST.
- **Marketplace tax:** as an e-commerce operator the platform must collect **TCS under GST s.52** and deduct **TDS under s.194-O** on marketplace-sourced sales. A CA must confirm this before marketplace go-live.

**M9 Marketplace (cross-tenant discovery)**
- Categories: doctors, lawyers, tutors, courses, events, products. Search runs on Postgres FTS and pg_trgm. Geo search ("near me") runs on **PostGIS**.
- Filters cover fee, language, availability today and rating. Reviews are verified (booking required).
- **Provider verification queue:** NMC/State Medical Council registration for doctors, Bar Council enrolment for lawyers, plus PAN and bank KYC through Razorpay.
- **Legal constraint:** Bar Council of India Rule 36 bans lawyer advertising and solicitation. The lawyer vertical must run as a neutral directory, with no paid ranking and no promotional copy.
- **Telemedicine:** must follow the Telemedicine Practice Guidelines 2020. Consent capture, a registered RMP only, and e-prescription format, with medicine-list limits enforced per consult mode.

**M10 Storefront and product marketplace (Phase 3)**
- Variants, inventory, shipping zones and digital downloads.
- Shiprocket integration for courier delivery. **Last-mile** (hyperlocal, using Porter, Shadowfax or our own riders) comes in Phase 4.

**M11 Community and CRM:** feed, categories, comments, DMs, group chat. Audience CRM: tags, staff notes, follow-ups, saved filters. These are Lockers parity features in Phase 2.

**M12 Notifications:** a single outbox feeding email (SES), WhatsApp Cloud API or Gupshup, SMS (MSG91, with DLT-registered templates as Indian law requires), web push and in-app. Users set per-channel preferences.

**M13 SaaS billing:** plans per tenant (Free, Basic, Pro, and later Clinic or Enterprise), Razorpay Subscriptions, usage add-ons (stream minutes, storage), dunning and plan-limit enforcement.

**M14 Platform:** public API with API keys and signed webhooks (Pro), data export as a ZIP, an audit log, and an admin back-office.

## 3. Architecture

```
            Cloudflare (DNS, CDN, WAF, custom hostnames / TLS)
     ┌──────────────┬───────────────────┬─────────────────────┐
  Next.js (web)   React SPA (app, PWA)   HLS / VOD CDN (S3)
  tenant sites,   dashboards, rooms,         │
  marketplace,    learner/patient portal     │
  SEO, ISR             │                     │
     └──────┬──────────┘                     │
         Go API (REST+JSON, OpenAPI) ── WS gateway (Go) ── Redis (pub/sub, rate-limit, presence)
            │            │                                     │
       PgBouncer     River workers (Go) ── ffmpeg, PDFs,   LiveKit SFU cluster (Go)
            │        email/WA, webhooks, recon, search idx  + Egress + Ingress + TURN
     PostgreSQL 16 (primary + read replica, RLS, PostGIS, pg_trgm)
```

**Go: a modular monolith first, not microservices.** There is one repo and one `api` binary with strict internal packages: `identity`, `tenancy`, `site`, `catalog`, `scheduling`, `rtc`, `commerce`, `lms`, `notes`, `marketplace`, `notify`, `billing`. Packages talk only through Go interfaces and domain events, using an outbox table drained by River. Any module that develops hot spots gets extracted later.

Separate binaries ship from the same repo:
- `api`: HTTP
- `worker`: River jobs
- `realtime`: WebSocket fan-out for chat, notifications and presence, built on goroutine-per-connection with Redis pub/sub
- `migrate`

**Go building blocks (stdlib and battle-tested only):**
- HTTP: `net/http` with the 1.22+ ServeMux (or chi for middleware ergonomics)
- Database: `pgx/v5` with **sqlc** for type-safe queries, and **goose** for migrations
- Jobs: **River**, a Postgres-native queue that gives transactional enqueue with no Kafka early on
- Real-time: `livekit/server-sdk-go`
- Observability: `log/slog` and **OpenTelemetry** (traces and metrics to Grafana stack)
- Concurrency and profiling: `errgroup` and `singleflight` for fan-out and cache stampede, `pprof` in prod behind auth
- API contract: OpenAPI 3.1 generated from code, plus a typed TS client for both frontends

**Frontends (pnpm and Turborepo monorepo):**
- `apps/web`: Next.js App Router. Tenant sites resolve by host in middleware. Also hosts the marketing site and marketplace.
- `apps/app`: Vite, React, TanStack Router and Query, Tailwind with shadcn/ui, LiveKit React components, PWA (installable).
- `packages/ui`, `packages/api-client` (generated), `packages/blocks` (Puck block definitions, shared by the editor and the renderer).

## 4. Data and RLS design
- **Shared DB with shared schema.** Every tenant table has `tenant_id uuid not null`, and composite indexes lead with `tenant_id`.
- The Go app connects as role `app_user`, with **no BYPASSRLS and no table ownership**. Each request opens a tx and runs `SET LOCAL app.tenant_id = $1, app.user_id = $2, app.role = $3`. `SET LOCAL` is safe under PgBouncer transaction pooling.
- Policy pattern:
  - `USING (tenant_id = current_setting('app.tenant_id')::uuid)`
  - Narrower row policies on top, for example: a member sees only their own `bookings` and `orders`, and a provider sees only their own `session_notes`.
  - `FORCE ROW LEVEL SECURITY` on every table.
- **Cross-tenant reads** (marketplace) go through a `marketplace` schema of published-only listings. A worker denormalizes into it, and it never exposes private columns.
- Platform admin uses a separate `admin_user` role. Every action goes to `audit_log`.
- **Sensitive data:** clinical notes, intake answers and prescriptions use envelope encryption (AWS KMS data keys, AES-GCM in Go). Columns are `bytea`.
- Large append-only tables (`audit_log`, `messages`, `usage_events`, `ledger_entries`) are partitioned by month.
- **RLS tests are a CI gate.** A Go test suite seeds two tenants and asserts that every table is invisible across tenants and across roles.

**Core tables:**
- Identity and tenancy: `users`, `tenants`, `memberships`, `domains`, `redirects`
- Site: `pages` / `page_versions`, `themes`
- Catalog: `offerings` + `offering_course|service|event|product`
- Providers and scheduling: `providers`, `availability_rules`, `availability_overrides`, `bookings`, `slot_holds`
- Real-time: `rooms`, `room_sessions`, `recordings`, `usage_events`
- Notes and courses: `notes`, `courses`, `modules`, `lessons`, `enrollments`, `lesson_progress`
- Commerce: `carts`, `orders`, `order_items`, `payments`, `refunds`, `payouts`, `ledger_entries`, `coupons`, `invoices`
- Subscriptions: `subscriptions` (both member-facing and SaaS)
- Trust and safety: `reviews`, `verifications`, `consents`
- Platform: `notifications_outbox`, `webhook_endpoints`, `api_keys`, `audit_log`

## 5. Infrastructure (AWS ap-south-1 Mumbai, for health-data residency)
- **EKS:**
  - general node group for api, worker, realtime and Next.js (standalone)
  - **dedicated LiveKit node group** with host networking, UDP 50000–60000, TURN/TLS 443, compute-optimized instances
  - separate Egress nodes, which are CPU-heavy
- **Data:** RDS PostgreSQL 16 (Multi-AZ) with a read replica, ElastiCache Redis (needed for multi-node LiveKit), S3 with CloudFront or Cloudflare for media.
- **IaC and delivery:** Terraform. GitHub Actions to ArgoCD, with preview environments per PR for the web app.
- **SLOs:**
  - API p95 under 200 ms, 99.9% availability
  - meeting join under 3 s
  - booking double-book rate 0
  - payment webhook to order-paid under 5 s
  - RPO 5 min, RTO 1 h
- **Security:** OWASP ASVS L2, rate limiting (Redis token bucket), CSP, signed media URLs, secrets in AWS Secrets Manager, quarterly pentest before handling health data at scale.

## 6. Phased delivery
Durations assume roughly 4 backend, 3 frontend, 1 DevOps and 1 QA engineer, plus 1 PM and 1 designer.

**Phase 0: Foundations (3 wks)**
- Monorepo, CI, Terraform, EKS, RDS, Redis, S3.
- Go skeleton (api, worker, migrate), sqlc and goose, OTel.
- Auth (OTP, Google, magic link), tenants, memberships, RBAC, the **RLS harness and cross-tenant test suite**.
- Next.js host-based tenant resolution, SPA shell, generated API client.
- LiveKit cluster deployed and load-tested with `lk load-test`.

**Phase 1: MVP (14 wks), covering creators and appointments**
- **Site builder:** Puck blocks, themes, legal page templates, redirects, sitemap and robots, JSON-LD, subdomains plus custom domains.
- **Catalog:** course, live_class, event, service.
- **Scheduling:** availability, slot generation, holds, the exclusion constraint, reschedule and cancel, reminders.
- **Meetings:** 1:1 and group, waiting room, chat, screen share, recording.
- **Live classes:** up to 100 interactive.
- **LMS core:** lessons, the HLS video pipeline, progress.
- **Notes:** session notes (encrypted), lesson notes, shared notes.
- **Payments:** Razorpay checkout, Route split to linked accounts, platform fee, refunds, ledger, reconciliation, GST invoices.
- **Notifications:** email, WhatsApp, SMS.
- **Portals:** learner, patient and client.
- **SaaS:** plans and limits. **Admin:** KYC and verification queue.
- **Exit criteria:**
  - RLS suite green
  - 0 double-bookings under a k6 concurrency test
  - 50-person meeting stable at p95 join under 3 s
  - payment recon matches 100% over a 7-day pilot
  - Lighthouse SEO 95+ on tenant sites
  - pilot with 10 tenants (5 educators, 5 clinics or practices)

**Phase 2: Scale and marketplace (10 wks)**
- Marketplace discovery (FTS, PostGIS, filters, verified reviews), the lawyer-compliant directory mode.
- Large live streams (HLS egress at 10k viewers, RTMP ingest), memberships through Razorpay Subscriptions, community feed and DMs, CRM.
- Transcription with AI notes, collaborative notes using Yjs, Google and Outlook two-way sync.
- Quizzes and certificates, telemedicine consent and e-prescription.

**Phase 3: Commerce (8 wks)**
- Product storefronts (physical and digital), inventory and variants, product marketplace, Shiprocket shipping.
- Marketplace TCS/TDS handling, public API and signed webhooks, data export, the second-Locker equivalent (multiple spaces per owner).

**Phase 4: Expansion**
- Hyperlocal last-mile delivery (aggregator APIs first, then an own rider app).
- ABDM / ABHA health-ID integration, React Native apps, Stripe Connect plus a multi-currency global launch, read-side search on Meilisearch if Postgres FTS runs out of headroom.

## 7. Key risks
1. **Self-hosting video on day one** needs real SRE time for UDP networking, TURN, autoscaling Egress and cost. *Mitigation:* a LiveKit load test in Phase 0, with LiveKit Cloud as a drop-in fallback (same SDK).
2. **MVP breadth** (courses and appointments together). *Mitigation:* one shared catalog, checkout and room stack. The two verticals differ only in the scheduling and LMS modules.
3. **Regulation:** health data (DPDP, telemedicine guidelines), lawyer advertising (BCI Rule 36), and marketplace GST TCS. *Mitigation:* legal and CA review is a gate before Phase 2 marketplace launch.
4. **Money bugs.** *Mitigation:* ledger, idempotency keys, daily reconciliation, no balance stored outside the ledger.

## 8. Verification approach (applies to every phase)
- **Go:** unit tests, plus integration tests against a real Postgres (testcontainers) including the **RLS cross-tenant suite**.
- **Concurrency:** k6 load tests for booking concurrency and checkout. `lk load-test` for rooms and streams.
- **Payments:** Razorpay test mode with webhook replay and duplicate-delivery tests.
- **End to end:** Playwright flows (create space, publish site, sell course, book appointment, join meeting, write note, refund).
- **Web quality:** Lighthouse and SEO checks in CI against a seeded tenant site.

## Next step after approval
1. Write these specs into `~/Desktop/atrium/docs/`: PRD, ERD/SQL, RLS policy catalog, API outline, ADRs for the five key decisions.
2. Scaffold Phase 0.
