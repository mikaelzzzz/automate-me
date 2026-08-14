# Automate.me — Technical Design

**Date:** 2026-08-14 · **Status:** Approved (brainstorming complete) · **PRD:** `/PRD.md`

## 1. Context

Multi-agent Taskmaster for the Google Cloud "All Things Agentic" hackathon (Track 1). Captures a user's routine (voice/text/photo), prices it (Cost of Inaction), ranks automations by payback, and executes them autonomously: AP2 v0.2 purchases against a simulated merchant over A2A, Google Calendar writes, Maps-optimized daily briefings, Gmail drafts, and a Teams automation report.

Key verified facts driving this design (research 2026-08-14):

- **ADK Go v2.2.0 is GA** (module `google.golang.org/adk/v2`, requires **Go 1.26.5**): graph workflow engine, human-in-the-loop, A2A integration (`server/adka2a`, `agent/remoteagent/v2`), MCP toolsets, Cloud Run/Agent Engine deploy paths.
- **AP2 spec v0.2** (Apr 2026) defines **two mandates** — Checkout Mandate and Payment Mandate — not the v0.1 Intent→Cart→Payment triple that most articles (and the official Go samples) still describe. Mandate JWTs must use **non-deterministic signatures (ECDSA), not Ed25519**; versioning via exact-match `vct` claims (`mandate.checkout.open.1`, `mandate.payment.1`); merchant must return a signed Checkout JWT and issue Checkout/Payment Receipts. The **Trusted Surface must be non-agentic**.
- **adk-go pins the legacy a2a-go v0.3.x line** — we speak A2A only through adk-go's `adka2a`/`remoteagent` surface and never import a2a-go v2.x directly.
- **Models:** `gemini-3.5-flash` (stable; hackathon requires Gemini 3.5+). `gemini-3-pro` / `gemini-3.5-pro` do not exist.

## 2. System overview

Two Cloud Run services + Firestore. All Go.

```
┌─────────────────── Cloud Run: automate-me ────────────────────┐
│ React SPA (static, served by the Go binary) + REST API        │
│ (chat streams via SSE; briefing delivery is poll-based —      │
│  no cross-instance push fan-out needed)                       │
│                                                               │
│ Orchestrator (LLMAgent, gemini-3.5-flash)                     │
│  ├─ Routine Analyst      interview, photo→tasks (vision),     │
│  │                       duration estimates + confirmation    │
│  ├─ Value Engine         DETERMINISTIC Go (functiontool):     │
│  │                       Cost of Inaction, payback ranking    │
│  ├─ Automation Advisor   matches routines ↔ catalog,          │
│  │                       assembles proposals                  │
│  ├─ Briefing Agent       per-event route fan-out, departure   │
│  │                       blocks, weather→clothing, flood alert│
│  ├─ Executor tools       CalendarTool · MapsTool ·            │
│  │                       GmailDraftTool · ReportTool          │
│  └─ Shopping Agent       AP2 v0.2 client flow                 │
│                                │ A2A (via adka2a)             │
│ Calendar Watcher (Cloud Scheduler → scan → proposals)         │
│ Trusted Surface endpoints (non-agentic: consent + signing)    │
└────────────────────────────────┼──────────────────────────────┘
                                 ▼
┌────────────── Cloud Run: merchant-agent (A2A server) ─────────┐
│ Product catalog · signed Checkout JWT (ECDSA P-256) ·         │
│ Checkout Mandate verification · Checkout/Payment Receipts ·   │
│ SIMULATED settlement (clearly labeled)                        │
└───────────────────────────────────────────────────────────────┘
```

## 3. Units and boundaries

### 3.1 `automate-me` service (Go binary)

| Package | Purpose | Depends on |
|---|---|---|
| `cmd/server` | wiring, HTTP mux, static SPA serving | everything below |
| `internal/agents` | ADK agent graph definitions (orchestrator, analyst, advisor, briefing, shopping) | adk/v2, tools |
| `internal/engine` | **Value Engine** — pure functions: `CostOfInaction(task, rate) = minutes × freq/month × rate`; `PaybackMonths(a) = upfrontCost ÷ (monthlyTimeValueRecovered − monthlyRunningCost)`; zero-upfront automations rank by net monthly savings; negative-net never proposed. No I/O, no LLM | stdlib only |
| `internal/catalog` | recipe loading/matching (Firestore-backed data) | firestore |
| `internal/tools` | CalendarTool, MapsTool, WeatherTool, FloodTool, GmailDraftTool, ReportTool as ADK functiontools | Google APIs |
| `internal/ap2` | mandate types (`vct` exact match), JWT build/verify (ECDSA P-256), checkout hash binding, audit trail | jwx or stdlib crypto |
| `internal/trusted` | **non-agentic** consent + signing endpoints; loads user key from Secret Manager; signs only after explicit UI approval | ap2, secretmanager |
| `internal/store` | Firestore repositories + ADK `session.Service` implementation backed by Firestore — **built days 1–2 with a go/no-go at end of day 2**; fallback: pin Cloud Run to min=max=1 instance and document the tradeoff | firestore |
| `internal/briefing` | fan-out orchestration (`errgroup` + context), one route sub-task per calendar event | tools, engine |
| `web/` | React + TS + Vite + Tailwind (tokens generated from `docs/design/design-system.json`) | — |

Contract test for each unit: what it does, how it's used, what it depends on — table above is the checklist.

### 3.2 `merchant-agent` service

Separate Go module, **also built on adk-go's `adka2a` server surface** — both ends of the wire use the same pinned a2a-go line (v0.3.x via adk-go), making protocol compatibility structural; a2a-go v2.x is never imported by either module. Exposes an A2A agent card + skills (`search_catalog`, `create_checkout`, `submit_checkout_mandate`, `submit_payment_mandate`).

Implements the merchant half of AP2 v0.2 **and explicitly doubles as the simulated Credential Provider + Merchant Payment Processor** (one actor may hold several AP2 roles; the simulation labels all three): returns the signed Checkout JWT; verifies the **Checkout Mandate** against it (signature, exact `vct`, hash binding) → Checkout Receipt; verifies the **Payment Mandate** the same way (signature, `vct: mandate.payment.1`, checkout-hash binding) **before** settlement → Payment Receipt. Settlement is an in-process simulation, labeled as simulation in every artifact. Own ECDSA key in Secret Manager.

**Key exchange:** the user's public JWK is embedded in the `create_checkout` request and pinned by the merchant for that checkout; all mandate verification for the checkout uses the pinned key.

## 4. Data model (Firestore)

| Collection | Key fields |
|---|---|
| `users` | profile, hourly rate (declared or derived), mode (personal/teams), OAuth token refs |
| `routine_profiles` | tasks[] {name, freq, est_minutes, source: interview/photo/calendar, confirmed} |
| `catalog` | recipes: trigger pattern, capability ∈ **canonical enum {`vision`, `calendar_write`, `maps_routes`, `weather_flood`, `gmail_draft`, `ap2_purchase`, `report_gen`}** (single source of truth, referenced by PRD F4), class (executable/advised/roadmap), cost model {upfront, monthly_running} |
| `proposals` | routine ref, recipe ref, payback months, status (proposed/approved/executed/declined) |
| `mandates` | AP2 audit trail: checkout JWT, mandate JWTs, receipts, timestamps, status |
| `savings_ledger` | weekly entries {hours_recovered, brl_recovered, source recipe, `mandate_ref` → `mandates` doc when the entry stems from an AP2 purchase (F9 promises verifiable receipts attached)} |
| `sessions` | ADK session state (custom `session.Service`) — Cloud Run scales stateless |
| `briefings` | per-day cards {event, departure_time, route, weather, clothing, flood_risk} |

## 5. AP2 v0.2 flow (happy path)

1. User approves proposal in chat → **Shopping Agent** (via A2A) calls `create_checkout` (including the user's public JWK) → merchant returns the **signed Checkout JWT**.
2. **Trusted Surface** (React modal + `internal/trusted` endpoint; no LLM in path) verifies the Checkout JWT's merchant signature and renders **its contents**: cart, total, merchant identity, checkout hash.
3. On explicit click, Trusted Surface signs the **Checkout Mandate** (ECDSA P-256, hash-bound to the Checkout JWT, `vct: mandate.checkout.open.1`).
4. Merchant verifies the Checkout Mandate (signature against pinned JWK, exact `vct`, hash binding) → returns **Checkout Receipt**.
5. Trusted Surface signs the **Payment Mandate** (`vct: mandate.payment.1`, bound to the checkout hash).
6. Merchant — acting as simulated Credential Provider + Payment Processor — **verifies the Payment Mandate** (signature, `vct`, hash binding), then simulates settlement → **Payment Receipt** (signed) → stored in `mandates`, referenced from `savings_ledger`; CalendarTool books delivery.

Failure paths (all tested): invalid JWT signature → reject with reason; `vct` mismatch → reject; hash-binding mismatch → abort + audit entry; merchant timeout → retry idempotently by checkout ID.

Threat-model stance (honest scoping of the AP2 model): the Shopping Agent is treated as untrusted for **signing** — no LLM-adjacent code path can produce a mandate signature. In the hackathon build this is a **logical separation inside one trust domain** (same Cloud Run service and service account); the signing key lives in its own Secret Manager secret with least-privilege IAM as the cheapest real boundary, and full isolation of `internal/trusted` into its own service/service account is documented as the production-hardening step. The demo narrative states this scoping explicitly rather than overclaiming.

## 6. External APIs

| API | Use | Status |
|---|---|---|
| Google Calendar API | read (watcher, briefing), write (blocks, delivery, batching) | OAuth, verified |
| Google OAuth (early task — Watcher, Calendar/Gmail tools all depend on it) | scopes: `calendar.events` (read/write) + `gmail.compose` (drafts only); consent screen in testing mode (demo account allowlisted); refresh tokens encrypted in Firestore via Secret Manager KEK; `DEMO_MODE=seed` is the kill switch | design decision |
| Google Maps Routes API | `computeRoutes` with future `departureTime` (allowed for DRIVE), `TRAFFIC_AWARE_OPTIMAL`, `computeAlternativeRoutes` (⚠ not returned with intermediate waypoints — compute legs separately); **`duration − staticDuration` = traffic cost in seconds → × hourly rate = R$ shown to user**; cache by (origin, dest, 15-min window) — 33× pricier than Weather | key exists (DWS Pro project), verified live |
| Google Maps Weather API (**GA**, full Brazil coverage) | `forecast/hours:lookup` (`feelsLikeTemperature`, `precipitation`, `uvIndex`, `wind`) → clothing suggestion; **`publicAlerts:lookup`** with `FLOOD`/`FLASH_FLOOD` events, severity/urgency/certainty + GeoJSON polygon tested against route (pass `languageCode=pt-BR`); 10k free calls/mo | verified live (endpoints respond) |
| GeoSampa WFS layer `risco_ocorrencia_alagamento` | historic flood-point layer for São Paulo, downloaded **once** as static GeoJSON, intersected with route polyline: "your route crosses 3 points with flooding history" — zero runtime cost, always has data (August is dry season) | verified |
| Cloud Text-to-Speech | spoken replies | verified |
| Gmail API (drafts scope only) | confirmation/delegation drafts | verified |

Voice input: browser MediaRecorder → audio bytes → Gemini multimodal input directly (no separate STT).

## 7. Error handling

- Typed, wrapped errors (`%w`) throughout; sentinel errors per package; single-handling rule (log or return, never both).
- Tool failures surface to agents as structured tool errors → agent degrades gracefully and offers retry; never a dead end in chat.
- External API calls: context timeouts; Briefing renders partial cards (route without weather beats no card).
- Demo resilience: `DEMO_MODE=seed` boots with seeded Firestore data and a guaranteed-up merchant; kill switch if live OAuth fails.

## 8. Concurrency & quality gates

- Briefing fan-out: `errgroup.WithContext`, bounded parallelism, results collected via channel into an owned slice — no shared mutable state without sync.
- CI (GitHub Actions) on every push: `gofmt -l`, `go vet`, `go fix` modernizers, `golangci-lint`, **`go test -race ./...`**.
- Value Engine: table-driven tests over every payback formula. `internal/ap2`: sign/verify round-trips, tampered-hash, wrong-`vct`, Ed25519-rejection tests.
- Integration: `httptest`-hosted merchant; full A2A+AP2 happy path + 3 failure paths.

## 9. Deployment & observability

- Two Cloud Run services (`automate-me`, `merchant-agent`), deploy via gcloud in GitHub Actions.
- Firestore (native mode), Secret Manager (ECDSA keys, API keys), Cloud Scheduler (watcher + morning briefing).
- ADK telemetry → Cloud Logging/Trace; these console views are the demo's "running on Google Cloud" proof.
- README: spin-up instructions (required by rules), architecture diagram, `.env.example`.

## 10. Open questions

1. Three cheap API confirmations once the key is wired (all have safe defaults): `precipitation` subfield shape in Weather forecast; which Brazilian authority publishes `publicAlerts` (likely INMET); which pricing tier `TRAFFIC_AWARE_OPTIMAL` falls in.

(Resolved since v1: session.Service is scheduled for days 1–2 with an explicit go/no-go — §3.1; flood API selection — §6/§11.)

## 11. Explicitly rejected alternatives

- **Official AP2 Go samples as base** — v0.1 nomenclature, legacy Gemini SDK, no adk-go/a2a-go; would poison the architecture story.
- **Live API bidirectional voice** — works in Go (verified, `examples/bidi`) but undocumented and unneeded; push-to-talk chosen.
- **Google Flood Forecasting API** — real and free, but riverine-focused, waitlist-gated (kills the Aug 31 deadline) and its urban model has 20 km resolution; useless for street-level São Paulo alerts. Weather API `publicAlerts` + GeoSampa historic layer chosen instead. CGE-SP scraping and CEMADEN internal endpoints rejected as demo-fragile (no SLA, no CORS, "-" string fields).
- **Real payment sandbox** — no PSP integration exists in AP2 repo; simulation labeled as such.
- **Monolith without A2A** — cheaper but kills the agent-network narrative and the architecture prize.
